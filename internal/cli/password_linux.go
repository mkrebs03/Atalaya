//go:build linux

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

const passwordTerminalPollMilliseconds = 50

func readPasswordWithoutEcho(fileDescriptor int) ([]byte, error) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(
		interrupts,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGQUIT,
		syscall.SIGTERM,
		syscall.SIGTSTP,
	)
	defer signal.Stop(interrupts)

	return readPasswordWithoutEchoSignals(fileDescriptor, interrupts)
}

func readPasswordWithoutEchoSignals(
	fileDescriptor int,
	interrupts <-chan os.Signal,
) ([]byte, error) {
	return readPasswordWithoutEchoSignalsObserved(fileDescriptor, interrupts, nil)
}

func readPasswordWithoutEchoSignalsObserved(
	fileDescriptor int,
	interrupts <-chan os.Signal,
	afterByte func([]byte),
) (result []byte, resultErr error) {
	fileFlags, err := unix.FcntlInt(uintptr(fileDescriptor), unix.F_GETFL, 0)
	if err != nil {
		return nil, fmt.Errorf("read terminal flags: %w", err)
	}
	state, err := unix.IoctlGetTermios(fileDescriptor, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("read terminal state: %w", err)
	}
	hiddenState := *state
	hiddenState.Lflag &^= unix.ECHO
	hiddenState.Lflag |= unix.ICANON | unix.ISIG
	hiddenState.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fileDescriptor, unix.TCSETS, &hiddenState); err != nil {
		return nil, fmt.Errorf("disable terminal echo: %w", err)
	}
	if _, err := unix.FcntlInt(
		uintptr(fileDescriptor),
		unix.F_SETFL,
		fileFlags|unix.O_NONBLOCK,
	); err != nil {
		_ = unix.IoctlSetTermios(fileDescriptor, unix.TCSETS, state)
		return nil, fmt.Errorf("make password terminal nonblocking: %w", err)
	}

	var input [1]byte
	var password []byte
	defer func() {
		clear(input[:])
		if err := unix.IoctlSetTermios(fileDescriptor, unix.TCSETS, state); err != nil {
			clear(password)
			result = nil
			resultErr = joinPasswordReadError(
				resultErr,
				fmt.Errorf("restore terminal state: %w", err),
			)
		}
		if _, err := unix.FcntlInt(uintptr(fileDescriptor), unix.F_SETFL, fileFlags); err != nil {
			clear(password)
			result = nil
			resultErr = joinPasswordReadError(
				resultErr,
				fmt.Errorf("restore terminal flags: %w", err),
			)
		}
		if resultErr != nil {
			clear(password)
			result = nil
		}
	}()

	pollDescriptors := []unix.PollFd{{
		Fd:     int32(fileDescriptor),
		Events: unix.POLLIN,
	}}
	for {
		select {
		case received := <-interrupts:
			return nil, fmt.Errorf("password prompt interrupted by %s", received)
		default:
		}

		pollDescriptors[0].Revents = 0
		_, err := unix.Poll(pollDescriptors, passwordTerminalPollMilliseconds)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("wait for password input: %w", err)
		}
		revents := pollDescriptors[0].Revents
		if revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return nil, fmt.Errorf("password terminal became unavailable")
		}
		if revents&(unix.POLLIN|unix.POLLHUP) == 0 {
			continue
		}

		count, err := unix.Read(fileDescriptor, input[:])
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read password input: %w", err)
		}
		if count == 0 {
			if len(password) == 0 {
				return nil, io.EOF
			}
			return password, nil
		}

		switch input[0] {
		case '\b':
			if len(password) > 0 {
				password[len(password)-1] = 0
				password = password[:len(password)-1]
			}
		case '\n':
			return password, nil
		case '\r':
			// Unix terminals use newline as the delimiter after ICRNL conversion.
		default:
			password = append(password, input[0])
			if afterByte != nil {
				afterByte(password)
			}
		}
		input[0] = 0
	}
}

func joinPasswordReadError(current, additional error) error {
	if current == nil {
		return additional
	}
	return errors.Join(current, additional)
}
