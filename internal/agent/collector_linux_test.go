//go:build linux

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestParseMountinfoEscapesAndRejectsUnknown(t *testing.T) {
	mounts, partial := parseMountinfo(strings.NewReader("36 25 8:1 / /data\\040disk rw - ext4 /dev/sda1 rw\n37 25 8:2 / /bad\\999 rw - ext4 /dev/sda2 rw\n"))
	if len(mounts) != 1 || mounts[0].point != "/data disk" || !partial {
		t.Fatalf("mounts=%#v partial=%v", mounts, partial)
	}
}

func TestTailscaleDiscoveryIgnoresPATH(t *testing.T) {
	d := t.TempDir()
	fake := filepath.Join(d, "tailscale")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", d)
	if path, err := exec.LookPath("tailscale"); err != nil || path != fake {
		t.Fatalf("test PATH not active: %q %v", path, err)
	}
	executable, _ := openTailscaleExecutable([]string{"/usr/bin/tailscale", "/usr/local/bin/tailscale"}, trustedTailscaleExecutable)
	if executable != nil {
		defer executable.Close()
		if executable.Name() == fake {
			t.Fatal("PATH executable selected")
		}
	}
}

func TestTailscaleExecutesValidatedInodeAndClosesFD(t *testing.T) {
	d := t.TempDir()
	path := filepath.Join(d, "tailscale")
	original := []byte("#!/bin/sh\nprintf original\n")
	replacement := []byte("#!/bin/sh\nprintf replacement\n")
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatal(err)
	}
	var fd int
	out, err := runTailscaleCandidates(context.Background(), []string{path}, func(stat unix.Stat_t) bool { return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0o111 != 0 }, func(file *os.File) error {
		fd = int(file.Fd())
		newPath := filepath.Join(d, "new")
		if err := os.WriteFile(newPath, replacement, 0o755); err != nil {
			return err
		}
		return os.Rename(newPath, path)
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "original" {
		t.Fatalf("executed %q", out)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
		t.Fatal("validated descriptor remained open")
	}
}

func TestBoundedStatfsCapsHungCalls(t *testing.T) {
	c := NewLinuxCollector()
	release := make(chan struct{})
	var active, max atomic.Int32
	c.Statfs = func(string, *unix.Statfs_t) error {
		n := active.Add(1)
		for {
			old := max.Load()
			if n <= old || max.CompareAndSwap(old, n) {
				break
			}
		}
		<-release
		active.Add(-1)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.boundedStatfs(ctx, "/") }()
	}
	wg.Wait()
	if max.Load() > 4 {
		t.Fatalf("max active=%d", max.Load())
	}
	start := time.Now()
	if _, err := c.boundedStatfs(context.Background(), "/"); err == nil || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("exhausted executor err=%v elapsed=%v", err, time.Since(start))
	}
	close(release)
}
func TestParseMemoryAndUptime(t *testing.T) {
	used, total, err := parseMemory([]byte("MemTotal: 100 kB\nMemAvailable: 40 kB\n"), nil)
	if err != nil || used != 60*1024 || total != 100*1024 {
		t.Fatalf("used=%d total=%d err=%v", used, total, err)
	}
	up, err := parseUptime([]byte("123.9 0"), nil)
	if err != nil || up != 123 {
		t.Fatalf("up=%d err=%v", up, err)
	}
}
func TestParseTailscaleSubset(t *testing.T) {
	v, err := parseTailscale([]byte(`{"BackendState":"Running","Self":{"DNSName":"host.example.ts.net.","TailscaleIPs":["100.64.0.1"],"Future":true},"Unknown":1}`))
	if err != nil || v.State != "connected" || v.Hostname == nil || *v.Hostname != "host.example.ts.net" || len(v.Addresses) != 1 {
		t.Fatalf("value=%#v err=%v", v, err)
	}
}

func TestParseCPUIgnoresPerCoreAndGuestCounters(t *testing.T) {
	counters, err := parseCPU([]byte("cpu  10 2 3 20 5 1 1 2 999 999\ncpu0 1000 0 0 0\n"), nil)
	if err != nil || counters.total != 44 || counters.idle != 25 {
		t.Fatalf("counters=%#v err=%v", counters, err)
	}
}

func TestMultiplyBytesRejectsOverflow(t *testing.T) {
	if _, ok := multiplyBytes(^uint64(0), 2); ok {
		t.Fatal("accepted overflow")
	}
}

func TestCPURejectsIdleDeltaAboveTotal(t *testing.T) {
	if _, err := cpuPercent(cpuCounters{100, 20}, cpuCounters{110, 40}); err == nil {
		t.Fatal("accepted impossible idle delta")
	}
}
func TestProcReadIsBounded(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "uptime"), make([]byte, 4097), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewLinuxCollector()
	c.ProcRoot = d
	if _, err := c.read("uptime"); err == nil {
		t.Fatal("accepted oversized proc file")
	}
}
func TestUUIDAmbiguityFallsBack(t *testing.T) {
	d := t.TempDir()
	for _, name := range []string{"A", "B"} {
		if err := os.WriteFile(filepath.Join(d, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := NewLinuxCollector()
	c.UUIDRoot = d
	if got := c.uuidDevices(); len(got) != 0 {
		t.Fatalf("ambiguous map=%v", got)
	}
}
func TestTailscaleOutputLimit(t *testing.T) {
	var b limitedBuffer
	b.limit = 2
	n, err := b.Write([]byte("abc"))
	if err != nil || n != 3 || !b.exceeded || string(b.data) != "ab" {
		t.Fatalf("buffer=%#v n=%d err=%v", b, n, err)
	}
	c := NewLinuxCollector()
	c.RunTailscale = func(context.Context) ([]byte, error) { return nil, context.DeadlineExceeded }
	s := c.Collect(context.Background(), time.Unix(1, 0))
	found := false
	for _, w := range s.CollectionWarnings {
		found = found || w == "tailscale_unavailable"
	}
	if !found {
		t.Fatal("missing tailscale warning")
	}
}
