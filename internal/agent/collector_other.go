//go:build !linux

package agent

import (
	"context"
	"time"
)

// NewLinuxCollector remains available to the cross-platform CLI build. The
// production agent is Linux-only; a non-Linux binary can still serve health
// and enrollment diagnostics without pretending to collect host metrics.
func NewLinuxCollector() Collector { return unsupportedCollector{} }

type unsupportedCollector struct{}

func unknownTailscale() Tailscale { return Tailscale{State: "unknown", Addresses: []string{}} }

func (unsupportedCollector) Collect(_ context.Context, now time.Time) Snapshot {
	return Snapshot{
		ProtocolVersion: ProtocolVersion,
		AgentVersion:    "dev",
		Host:            Host{ObservedAt: now.UTC().Format(time.RFC3339Nano)},
		Tailscale:       unknownTailscale(),
		Filesystems:     []Filesystem{},
		CollectionWarnings: []string{
			"cpu_unavailable", "memory_unavailable", "uptime_unavailable",
			"boot_id_unavailable", "filesystems_unavailable", "tailscale_unavailable",
		},
	}
}
