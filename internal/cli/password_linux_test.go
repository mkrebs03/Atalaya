//go:build linux

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadTerminalPasswordUsesPseudoTerminalWithoutEcho(t *testing.T) {
	master, slave := openPseudoTerminal(t)
	defer master.Close()
	defer slave.Close()

	output := &bytes.Buffer{}
	type result struct {
		password []byte
		err      error
	}
	resultChannel := make(chan result, 1)
	go func() {
		password, err := readTerminalPassword(slave, output, "Password: ")
		resultChannel <- result{password: password, err: err}
	}()
	waitForTerminalEcho(t, slave, false)

	if _, err := master.Write([]byte("correct horse battery staple\n")); err != nil {
		t.Fatalf("write pseudo-terminal password: %v", err)
	}
	select {
	case received := <-resultChannel:
		defer clear(received.password)
		if received.err != nil {
			t.Fatalf("readTerminalPassword() error = %v", received.err)
		}
		if string(received.password) != "correct horse battery staple" {
			t.Fatalf("password = %q", received.password)
		}
	case <-time.After(time.Second):
		t.Fatal("readTerminalPassword() did not finish")
	}
	if output.String() != "Password: \n" {
		t.Fatalf("prompt output = %q", output.String())
	}
	waitForTerminalEcho(t, slave, true)
}

func TestReadPasswordWithoutEchoRestoresPseudoTerminalWhenInterrupted(t *testing.T) {
	master, slave := openPseudoTerminal(t)
	defer master.Close()
	defer slave.Close()

	originalState, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("read original terminal state: %v", err)
	}
	originalFlags, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("read original terminal flags: %v", err)
	}
	interrupts := make(chan os.Signal, 1)
	resultChannel := make(chan error, 1)
	go func() {
		password, err := readPasswordWithoutEchoSignals(int(slave.Fd()), interrupts)
		clear(password)
		resultChannel <- err
	}()
	waitForTerminalEcho(t, slave, false)
	interrupts <- syscall.SIGINT

	select {
	case err := <-resultChannel:
		if err == nil {
			t.Fatal("readPasswordWithoutEchoSignals() error = nil")
		}
	case <-time.After(time.Second):
		t.Fatal("interrupted password read did not finish")
	}
	afterState, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("read restored terminal state: %v", err)
	}
	if !reflect.DeepEqual(afterState, originalState) {
		t.Fatal("terminal state was not restored after interrupt")
	}
	afterFlags, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("read restored terminal flags: %v", err)
	}
	if afterFlags != originalFlags {
		t.Fatalf("terminal flags = %#x, want %#x", afterFlags, originalFlags)
	}
}

func TestReadPasswordWithoutEchoClearsPartialBufferWhenInterrupted(t *testing.T) {
	master, slave := openPseudoTerminal(t)
	defer master.Close()
	defer slave.Close()

	interrupts := make(chan os.Signal, 1)
	var partialBuffer []byte
	resultChannel := make(chan error, 1)
	go func() {
		password, err := readPasswordWithoutEchoSignalsObserved(
			int(slave.Fd()),
			interrupts,
			func(current []byte) {
				if partialBuffer == nil {
					partialBuffer = current
					interrupts <- syscall.SIGINT
				}
			},
		)
		clear(password)
		resultChannel <- err
	}()
	waitForTerminalEcho(t, slave, false)
	if _, err := master.Write([]byte("partial password\n")); err != nil {
		t.Fatalf("write partial pseudo-terminal password: %v", err)
	}

	select {
	case err := <-resultChannel:
		if err == nil {
			t.Fatal("readPasswordWithoutEchoSignalsObserved() error = nil")
		}
	case <-time.After(time.Second):
		t.Fatal("interrupted partial password read did not finish")
	}
	if len(partialBuffer) == 0 {
		t.Fatal("password reader did not expose a partial buffer to the test observer")
	}
	if !allZero(partialBuffer) {
		t.Fatal("partial password buffer was not cleared")
	}
}

func TestReadTerminalPasswordRestoresPseudoTerminalAfterProcessSignal(t *testing.T) {
	master, slave := openPseudoTerminal(t)
	defer master.Close()
	defer slave.Close()

	originalState, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("read original terminal state: %v", err)
	}
	originalFlags, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("read original terminal flags: %v", err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestPasswordPromptHelperProcess$")
	command.Env = append(os.Environ(), "ATALAYA_PASSWORD_PROMPT_HELPER=1")
	command.Stdin = slave
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start password prompt helper: %v", err)
	}
	waitForTerminalEcho(t, slave, false)
	if err := command.Process.Signal(syscall.SIGINT); err != nil {
		command.Process.Kill()
		t.Fatalf("interrupt password prompt helper: %v", err)
	}

	waitChannel := make(chan error, 1)
	go func() {
		waitChannel <- command.Wait()
	}()
	select {
	case err := <-waitChannel:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != passwordPromptInterruptedExitCode {
			t.Fatalf("password prompt helper wait error = %v", err)
		}
	case <-time.After(time.Second):
		command.Process.Kill()
		t.Fatal("interrupted password prompt helper did not finish")
	}
	afterState, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("read restored terminal state: %v", err)
	}
	if !reflect.DeepEqual(afterState, originalState) {
		t.Fatal("terminal state was not restored after process signal")
	}
	afterFlags, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("read restored terminal flags: %v", err)
	}
	if afterFlags != originalFlags {
		t.Fatalf("terminal flags = %#x, want %#x", afterFlags, originalFlags)
	}
}

const passwordPromptInterruptedExitCode = 23

func TestPasswordPromptHelperProcess(t *testing.T) {
	if os.Getenv("ATALAYA_PASSWORD_PROMPT_HELPER") != "1" {
		return
	}
	password, err := readTerminalPassword(os.Stdin, os.Stderr, "Password: ")
	clear(password)
	if err == nil {
		os.Exit(passwordPromptInterruptedExitCode + 1)
	}
	os.Exit(passwordPromptInterruptedExitCode)
}

func openPseudoTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	masterDescriptor, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open pseudo-terminal master: %v", err)
	}
	master := os.NewFile(uintptr(masterDescriptor), "/dev/ptmx")
	if err := unix.IoctlSetPointerInt(masterDescriptor, unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Fatalf("unlock pseudo-terminal: %v", err)
	}
	number, err := unix.IoctlGetInt(masterDescriptor, unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Fatalf("locate pseudo-terminal slave: %v", err)
	}
	slave, err := os.OpenFile(
		fmt.Sprintf("/dev/pts/%d", number),
		os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		master.Close()
		t.Fatalf("open pseudo-terminal slave: %v", err)
	}
	return master, slave
}

func waitForTerminalEcho(t *testing.T, terminal *os.File, expected bool) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err := unix.IoctlGetTermios(int(terminal.Fd()), unix.TCGETS)
		if err != nil {
			t.Fatalf("read pseudo-terminal state: %v", err)
		}
		if echo := state.Lflag&unix.ECHO != 0; echo == expected {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pseudo-terminal echo did not become %t", expected)
}
