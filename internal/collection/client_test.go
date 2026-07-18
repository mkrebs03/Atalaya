package collection

import (
	"context"
	"errors"
	"math"
	"net"
	"net/netip"
	"testing"

	"github.com/mkrebs03/atalaya/internal/agent"
)

type resolverSequence struct {
	values [][]netip.Addr
	calls  int
}

func (r *resolverSequence) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	value := r.values[r.calls]
	r.calls++
	return value, nil
}

type recordingDialer struct{ addresses []string }

func (d *recordingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	d.addresses = append(d.addresses, address)
	client, server := net.Pipe()
	go server.Close()
	return client, nil
}

func TestValidateAgentURL(t *testing.T) {
	for _, valid := range []string{"https://oracle.tailnet.ts.net:9474", "https://a.ts.net:9474"} {
		if _, err := ValidateAgentURL(valid); err != nil {
			t.Fatalf("valid URL %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"http://a.ts.net:9474", "https://100.64.0.1:9474", "https://a.ts.net", "https://a.ts.net:9474/", "https://a.ts.net:9474?x=1", "https://user@a.ts.net:9474", "https://evil.example:9474"} {
		_, err := ValidateAgentURL(invalid)
		var pull *PullError
		if !errors.As(err, &pull) || pull.Kind != FailureProtocol {
			t.Fatalf("invalid URL %q: %v", invalid, err)
		}
	}
}

func TestTailscaleAddressPolicy(t *testing.T) {
	for _, item := range []struct {
		address string
		allowed bool
	}{{"100.64.0.1", true}, {"100.127.255.254", true}, {"fd7a:115c:a1e0::1", true}, {"100.128.0.1", false}, {"127.0.0.1", false}, {"fd7a:115c:a1e1::1", false}} {
		address, _ := netip.ParseAddr(item.address)
		if got := tailscaleAddress(address); got != item.allowed {
			t.Errorf("%s allowed=%v", item.address, got)
		}
	}
}

func TestSecureDialRejectsMixedDNSAndResolvesEveryDial(t *testing.T) {
	resolver := &resolverSequence{values: [][]netip.Addr{{netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("100.64.0.2")}, {netip.MustParseAddr("100.64.0.3")}}}
	dialer := &recordingDialer{}
	client, err := NewClient(resolver, dialer, nil)
	if err != nil {
		t.Fatal(err)
	}
	dial := client.secureDial("oracle.example.ts.net")
	if _, err = dial(context.Background(), "tcp", ""); err == nil {
		t.Fatal("mixed DNS answer accepted")
	}
	if len(dialer.addresses) != 0 {
		t.Fatal("dial occurred before all addresses were validated")
	}
	first, err := dial(context.Background(), "tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := dial(context.Background(), "tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
	if resolver.calls != 3 || len(dialer.addresses) != 2 || dialer.addresses[0] == dialer.addresses[1] {
		t.Fatalf("calls=%d addresses=%v", resolver.calls, dialer.addresses)
	}
}

func TestWireRejectsServerOnlyClockSkewWarning(t *testing.T) {
	snapshot := minimalWireSnapshot()
	snapshot.CollectionWarnings = []string{"clock_skew"}
	if err := validateSnapshot(snapshot); failure(err) != FailureProtocol {
		t.Fatalf("clock_skew accepted: %v", err)
	}
}

func TestWireRequiresNonNullArraysButAcceptsEmptyArrays(t *testing.T) {
	valid := minimalWireSnapshot()
	if err := validateSnapshot(valid); err != nil {
		t.Fatalf("empty arrays rejected: %v", err)
	}
	for _, mutate := range []func(*agent.Snapshot){
		func(s *agent.Snapshot) { s.Tailscale.Addresses = nil },
		func(s *agent.Snapshot) { s.Filesystems = nil },
		func(s *agent.Snapshot) { s.CollectionWarnings = nil },
	} {
		snapshot := minimalWireSnapshot()
		mutate(&snapshot)
		if err := validateSnapshot(snapshot); failure(err) != FailureProtocol {
			t.Fatalf("nil array accepted: %v", err)
		}
	}
}

func TestWireRejectsStorageOverflowAndNonCanonicalFilesystemFields(t *testing.T) {
	overflow := uint64(math.MaxInt64) + 1
	uuidUpper := "DEADBEEF"
	uuidUnicode := "déadbeef"
	tests := []struct {
		name   string
		mutate func(*agent.Snapshot)
	}{
		{"uptime overflow", func(s *agent.Snapshot) { s.Host.UptimeSeconds = &overflow }},
		{"memory overflow", func(s *agent.Snapshot) { s.Host.MemoryUsedBytes, s.Host.MemoryTotalBytes = &overflow, &overflow }},
		{"filesystem overflow", func(s *agent.Snapshot) {
			s.Filesystems = []agent.Filesystem{validFilesystem()}
			s.Filesystems[0].TotalBytes, s.Filesystems[0].UsedBytes = overflow, overflow
		}},
		{"relative mount", func(s *agent.Snapshot) {
			s.Filesystems = []agent.Filesystem{validFilesystem()}
			s.Filesystems[0].MountPoint = "var"
		}},
		{"dirty root", func(s *agent.Snapshot) {
			s.Filesystems = []agent.Filesystem{validFilesystem()}
			s.Filesystems[0].MountRoot = "/a/../b"
		}},
		{"leading zero major", func(s *agent.Snapshot) {
			s.Filesystems = []agent.Filesystem{validFilesystem()}
			s.Filesystems[0].MajorMinor = "08:1"
		}},
		{"overflow minor", func(s *agent.Snapshot) {
			s.Filesystems = []agent.Filesystem{validFilesystem()}
			s.Filesystems[0].MajorMinor = "8:4294967296"
		}},
		{"uppercase UUID", func(s *agent.Snapshot) {
			fs := validFilesystem()
			fs.FSUUID = &uuidUpper
			s.Filesystems = []agent.Filesystem{fs}
		}},
		{"non ASCII UUID", func(s *agent.Snapshot) {
			fs := validFilesystem()
			fs.FSUUID = &uuidUnicode
			s.Filesystems = []agent.Filesystem{fs}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := minimalWireSnapshot()
			test.mutate(&snapshot)
			if err := validateSnapshot(snapshot); failure(err) != FailureProtocol {
				t.Fatalf("invalid snapshot accepted: %v", err)
			}
		})
	}
}

func validFilesystem() agent.Filesystem {
	return agent.Filesystem{MountPoint: "/", Device: "/dev/sda1", MountRoot: "/", MajorMinor: "8:1", FSType: "ext4", TotalBytes: 1}
}

func TestAgentAndClientValidationParity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*agent.Snapshot)
	}{
		{"duplicate address", func(s *agent.Snapshot) { s.Tailscale.Addresses = []string{"100.64.0.1", "100.64.0.1"} }},
		{"duplicate warning", func(s *agent.Snapshot) { s.CollectionWarnings = []string{"cpu_unavailable", "cpu_unavailable"} }},
		{"conflicting warnings", func(s *agent.Snapshot) {
			s.CollectionWarnings = []string{"filesystems_partial", "filesystems_unavailable"}
		}},
		{"duplicate filesystem identity", func(s *agent.Snapshot) { fs := validFilesystem(); s.Filesystems = []agent.Filesystem{fs, fs} }},
		{"dirty absolute device", func(s *agent.Snapshot) {
			fs := validFilesystem()
			fs.Device = "/dev//sda1"
			s.Filesystems = []agent.Filesystem{fs}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := minimalWireSnapshot()
			test.mutate(&snapshot)
			agentAccepted := agent.ValidateSnapshot(snapshot) == nil
			clientAccepted := validateSnapshot(snapshot) == nil
			if agentAccepted != clientAccepted || agentAccepted {
				t.Fatalf("agent=%v client=%v", agentAccepted, clientAccepted)
			}
		})
	}
}
