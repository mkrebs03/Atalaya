//go:build linux

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	tailscaleOutputLimit = 1 << 20
	mountinfoLimit       = 4 << 20
)

type LinuxCollector struct {
	ProcRoot     string
	UUIDRoot     string
	Hostname     func() (string, error)
	RunTailscale func(context.Context) ([]byte, error)
	Statfs       func(string, *unix.Statfs_t) error
	statfsSlots  chan struct{}
}

func NewLinuxCollector() *LinuxCollector {
	return &LinuxCollector{ProcRoot: "/proc", UUIDRoot: "/dev/disk/by-uuid", Hostname: os.Hostname, RunTailscale: runTailscale, Statfs: unix.Statfs, statfsSlots: make(chan struct{}, 4)}
}

func (c *LinuxCollector) Collect(ctx context.Context, now time.Time) Snapshot {
	hostname, err := c.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = ""
	}
	s := Snapshot{ProtocolVersion: ProtocolVersion, AgentVersion: "dev", Host: Host{Hostname: hostname, ObservedAt: now.UTC().Format(time.RFC3339Nano)}, Filesystems: []Filesystem{}, CollectionWarnings: []string{}}
	if v, err := c.cpu(ctx); err == nil {
		s.Host.CPUUsedPercent = &v
	} else {
		s.CollectionWarnings = append(s.CollectionWarnings, "cpu_unavailable")
	}
	if used, total, err := parseMemory(c.read("meminfo")); err == nil {
		s.Host.MemoryUsedBytes, s.Host.MemoryTotalBytes = &used, &total
	} else {
		s.CollectionWarnings = append(s.CollectionWarnings, "memory_unavailable")
	}
	if v, err := parseUptime(c.read("uptime")); err == nil {
		s.Host.UptimeSeconds = &v
	} else {
		s.CollectionWarnings = append(s.CollectionWarnings, "uptime_unavailable")
	}
	if b, err := c.read("sys/kernel/random/boot_id"); err == nil && strings.TrimSpace(string(b)) != "" {
		v := strings.TrimSpace(string(b))
		s.Host.BootID = &v
	} else {
		s.CollectionWarnings = append(s.CollectionWarnings, "boot_id_unavailable")
	}
	fs, partial, err := c.filesystems(ctx)
	if err != nil {
		s.CollectionWarnings = append(s.CollectionWarnings, "filesystems_unavailable")
	} else {
		s.Filesystems = fs
		if partial {
			s.CollectionWarnings = append(s.CollectionWarnings, "filesystems_partial")
		}
	}
	tailscaleCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if value, err := c.RunTailscale(tailscaleCtx); err == nil {
		if ts, err := parseTailscale(value); err == nil {
			s.Tailscale = ts
		} else {
			s.Tailscale = unknownTailscale()
			s.CollectionWarnings = append(s.CollectionWarnings, "tailscale_unavailable")
		}
	} else {
		s.Tailscale = unknownTailscale()
		s.CollectionWarnings = append(s.CollectionWarnings, "tailscale_unavailable")
	}
	return s
}

func (c *LinuxCollector) read(name string) ([]byte, error) {
	limits := map[string]int64{"stat": 64 << 10, "meminfo": 64 << 10, "uptime": 4 << 10, "sys/kernel/random/boot_id": 4 << 10}
	limit, ok := limits[name]
	if !ok {
		return nil, fmt.Errorf("unsupported proc file")
	}
	f, err := os.Open(filepath.Join(c.ProcRoot, name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("proc file exceeds limit")
	}
	return b, nil
}
func (c *LinuxCollector) cpu(ctx context.Context) (float64, error) {
	a, err := parseCPU(c.read("stat"))
	if err != nil {
		return 0, err
	}
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
	}
	b, err := parseCPU(c.read("stat"))
	if err != nil {
		return 0, err
	}
	return cpuPercent(a, b)
}
func cpuPercent(a, b cpuCounters) (float64, error) {
	if b.total <= a.total || b.idle < a.idle {
		return 0, fmt.Errorf("invalid CPU counters")
	}
	total, idle := b.total-a.total, b.idle-a.idle
	if idle > total {
		return 0, fmt.Errorf("invalid CPU idle delta")
	}
	return float64(total-idle) * 100 / float64(total), nil
}

type cpuCounters struct{ total, idle uint64 }

func parseCPU(data []byte, err error) (cpuCounters, error) {
	if err != nil {
		return cpuCounters{}, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, fmt.Errorf("invalid proc stat")
	}
	var c cpuCounters
	for i, f := range fields[1:] {
		// guest and guest_nice are already included in user and nice.
		if i >= 8 {
			break
		}
		v, e := strconv.ParseUint(f, 10, 64)
		if e != nil || ^uint64(0)-c.total < v {
			return cpuCounters{}, fmt.Errorf("invalid CPU counter")
		}
		c.total += v
		if i == 3 || i == 4 {
			if ^uint64(0)-c.idle < v {
				return cpuCounters{}, fmt.Errorf("invalid CPU idle")
			}
			c.idle += v
		}
	}
	return c, nil
}
func parseMemory(data []byte, readErr error) (uint64, uint64, error) {
	if readErr != nil {
		return 0, 0, readErr
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		key := strings.TrimSuffix(f[0], ":")
		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}
		v, e := strconv.ParseUint(f[1], 10, 64)
		if e != nil || v > ^uint64(0)/1024 {
			return 0, 0, fmt.Errorf("invalid memory")
		}
		values[key] = v * 1024
	}
	total, ok1 := values["MemTotal"]
	avail, ok2 := values["MemAvailable"]
	if !ok1 || !ok2 || avail > total {
		return 0, 0, fmt.Errorf("missing memory")
	}
	return total - avail, total, nil
}
func parseUptime(data []byte, readErr error) (uint64, error) {
	if readErr != nil {
		return 0, readErr
	}
	f := strings.Fields(string(data))
	if len(f) == 0 {
		return 0, fmt.Errorf("missing uptime")
	}
	v, e := strconv.ParseFloat(f[0], 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= math.Exp2(64) {
		return 0, fmt.Errorf("invalid uptime")
	}
	return uint64(v), nil
}

var excludedFS = map[string]bool{"autofs": true, "binfmt_misc": true, "bpf": true, "cgroup": true, "cgroup2": true, "configfs": true, "debugfs": true, "devpts": true, "devtmpfs": true, "efivarfs": true, "fusectl": true, "hugetlbfs": true, "mqueue": true, "nsfs": true, "overlay": true, "proc": true, "pstore": true, "ramfs": true, "rootfs": true, "rpc_pipefs": true, "securityfs": true, "selinuxfs": true, "squashfs": true, "sysfs": true, "tmpfs": true, "tracefs": true}

type mount struct{ root, point, majorMinor, fsType, source string }

func parseMountinfo(r io.Reader) ([]mount, bool) {
	scanner := bufio.NewScanner(r)
	var out []mount
	partial := false
	for scanner.Scan() {
		m, e := parseMountLine(scanner.Text())
		if e != nil {
			partial = true
			continue
		}
		if !excludedFS[m.fsType] {
			out = append(out, m)
		}
	}
	return out, partial || scanner.Err() != nil
}
func parseMountLine(line string) (mount, error) {
	f := strings.Fields(line)
	sep := -1
	for i, v := range f {
		if v == "-" {
			sep = i
			break
		}
	}
	if sep < 6 || len(f) < sep+3 {
		return mount{}, fmt.Errorf("invalid mountinfo")
	}
	root, e := decodeMount(f[3])
	if e != nil {
		return mount{}, e
	}
	point, e := decodeMount(f[4])
	if e != nil {
		return mount{}, e
	}
	source, e := decodeMount(f[sep+2])
	if e != nil {
		return mount{}, e
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(point) {
		return mount{}, fmt.Errorf("relative mount")
	}
	if strings.HasPrefix(source, "/") {
		source = filepath.Clean(source)
	}
	return mount{filepath.Clean(root), filepath.Clean(point), f[2], f[sep+1], source}, nil
}
func decodeMount(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+3 >= len(s) {
			return "", fmt.Errorf("bad escape")
		}
		code := s[i+1 : i+4]
		switch code {
		case "040":
			b.WriteByte(' ')
		case "011":
			b.WriteByte('\t')
		case "012":
			b.WriteByte('\n')
		case "134":
			b.WriteByte('\\')
		default:
			return "", fmt.Errorf("bad escape")
		}
		i += 3
	}
	return b.String(), nil
}
func (c *LinuxCollector) filesystems(ctx context.Context) ([]Filesystem, bool, error) {
	f, e := os.Open(filepath.Join(c.ProcRoot, "self/mountinfo"))
	if e != nil {
		return nil, false, e
	}
	defer f.Close()
	limited := &countingReader{reader: io.LimitReader(f, mountinfoLimit+1)}
	mounts, partial := parseMountinfo(limited)
	if limited.count > mountinfoLimit {
		return nil, false, fmt.Errorf("mountinfo exceeds limit")
	}
	uuids := c.uuidDevices()
	out := make([]Filesystem, 0, len(mounts))
	for _, m := range mounts {
		st, e := c.boundedStatfs(ctx, m.point)
		if e != nil {
			partial = true
			continue
		}
		if st.Bsize <= 0 {
			partial = true
			continue
		}
		total, okTotal := multiplyBytes(st.Blocks, uint64(st.Bsize))
		avail, okAvail := multiplyBytes(st.Bavail, uint64(st.Bsize))
		free, okFree := multiplyBytes(st.Bfree, uint64(st.Bsize))
		if !okTotal || !okAvail || !okFree || free > total || avail > total {
			partial = true
			continue
		}
		used := total - free
		var uuid *string
		if v, ok := uuids[m.majorMinor]; ok {
			uuid = &v
		}
		out = append(out, Filesystem{m.point, m.source, m.root, m.majorMinor, m.fsType, uuid, used, total, avail})
	}
	return out, partial, nil
}

type statfsResult struct {
	stat unix.Statfs_t
	err  error
}

func (c *LinuxCollector) boundedStatfs(ctx context.Context, path string) (unix.Statfs_t, error) {
	if c.Statfs == nil {
		c.Statfs = unix.Statfs
	}
	if c.statfsSlots == nil {
		c.statfsSlots = make(chan struct{}, 4)
	}
	select {
	case c.statfsSlots <- struct{}{}:
	case <-ctx.Done():
		return unix.Statfs_t{}, ctx.Err()
	default:
		return unix.Statfs_t{}, fmt.Errorf("statfs capacity exhausted")
	}
	result := make(chan statfsResult, 1)
	go func() {
		var st unix.Statfs_t
		err := c.Statfs(path, &st)
		result <- statfsResult{st, err}
		<-c.statfsSlots
	}()
	callCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	select {
	case value := <-result:
		return value.stat, value.err
	case <-callCtx.Done():
		return unix.Statfs_t{}, callCtx.Err()
	}
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

func multiplyBytes(blocks, blockSize uint64) (uint64, bool) {
	if blockSize != 0 && blocks > ^uint64(0)/blockSize {
		return 0, false
	}
	return blocks * blockSize, true
}
func (c *LinuxCollector) uuidDevices() map[string]string {
	out := map[string]string{}
	entries, e := os.ReadDir(c.UUIDRoot)
	if e != nil {
		return out
	}
	amb := map[string]bool{}
	for _, entry := range entries {
		var st unix.Stat_t
		if unix.Stat(filepath.Join(c.UUIDRoot, entry.Name()), &st) != nil {
			continue
		}
		key := fmt.Sprintf("%d:%d", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev)))
		v := strings.ToLower(entry.Name())
		if _, ok := out[key]; ok {
			amb[key] = true
		}
		out[key] = v
	}
	for k := range amb {
		delete(out, k)
	}
	return out
}

func runTailscale(ctx context.Context) ([]byte, error) {
	return runTailscaleCandidates(ctx, []string{"/usr/bin/tailscale", "/usr/local/bin/tailscale"}, trustedTailscaleExecutable, nil)
}

type executableValidator func(unix.Stat_t) bool

func trustedTailscaleExecutable(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Uid == 0 && stat.Mode&0o111 != 0 && stat.Mode&0o022 == 0
}

func runTailscaleCandidates(ctx context.Context, candidates []string, validate executableValidator, afterOpen func(*os.File) error) ([]byte, error) {
	executable, err := openTailscaleExecutable(candidates, validate)
	if err != nil {
		return nil, err
	}
	defer executable.Close()
	if afterOpen != nil {
		if err := afterOpen(executable); err != nil {
			return nil, err
		}
	}
	cmd := exec.CommandContext(ctx, "/proc/self/fd/3", "status", "--json")
	cmd.Args[0] = "tailscale"
	cmd.ExtraFiles = []*os.File{executable}
	var b limitedBuffer
	b.limit = tailscaleOutputLimit
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	if e := cmd.Run(); e != nil {
		return nil, e
	}
	if b.exceeded {
		return nil, fmt.Errorf("tailscale output too large")
	}
	return b.data, nil
}

func openTailscaleExecutable(candidates []string, validate executableValidator) (*os.File, error) {
	for _, path := range candidates {
		var before unix.Stat_t
		if err := unix.Lstat(path, &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG {
			continue
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			continue
		}
		var stat unix.Stat_t
		inspectErr := unix.Fstat(fd, &stat)
		if inspectErr != nil || before.Dev != stat.Dev || before.Ino != stat.Ino || !validate(stat) {
			_ = unix.Close(fd)
			continue
		}
		return os.NewFile(uintptr(fd), "tailscale"), nil
	}
	return nil, fmt.Errorf("tailscale executable unavailable")
}

type limitedBuffer struct {
	data     []byte
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if remaining > n {
			remaining = n
		}
		b.data = append(b.data, p[:remaining]...)
	}
	if n > remaining {
		b.exceeded = true
	}
	return n, nil
}
func unknownTailscale() Tailscale { return Tailscale{State: "unknown", Addresses: []string{}} }
func parseTailscale(b []byte) (Tailscale, error) {
	var raw struct {
		BackendState string `json:"BackendState"`
		Self         *struct {
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return Tailscale{}, fmt.Errorf("invalid tailscale JSON")
	}
	state := map[string]string{"Running": "connected", "Stopped": "stopped", "NeedsLogin": "needs_login"}[raw.BackendState]
	if state == "" {
		state = "unknown"
	}
	result := Tailscale{State: state, Addresses: []string{}}
	if raw.Self != nil {
		if raw.Self.DNSName != "" && raw.Self.DNSName != "." {
			v := strings.TrimSuffix(raw.Self.DNSName, ".")
			result.Hostname = &v
		}
		result.Addresses = append(result.Addresses, raw.Self.TailscaleIPs...)
	}
	return result, nil
}
