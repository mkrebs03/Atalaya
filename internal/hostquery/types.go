// Package hostquery defines the read-only current-host projection.
package hostquery

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("host not found")

type HostCursor struct{ Name, ID string }
type FilesystemCursor struct{ MountPoint, ID string }

type Repository interface {
	ListHosts(context.Context, int, *HostCursor) ([]HostSummary, error)
	GetHost(context.Context, string) (HostDetail, error)
	ListFilesystems(context.Context, string, int, *FilesystemCursor) ([]Filesystem, error)
}

type Tailscale struct {
	State     string   `json:"state"`
	Hostname  *string  `json:"hostname"`
	Addresses []string `json:"addresses"`
}

type ConstrainedFilesystem struct {
	ID          string  `json:"id"`
	MountPoint  string  `json:"mount_point"`
	UsedPercent float64 `json:"used_percent"`
	UsedBytes   int64   `json:"used_bytes"`
	TotalBytes  int64   `json:"total_bytes"`
}

type HostSummary struct {
	ID                        string                 `json:"id"`
	DisplayName               string                 `json:"display_name"`
	AdministrativeState       string                 `json:"administrative_state"`
	ObservedState             string                 `json:"observed_state"`
	Stale                     bool                   `json:"stale"`
	LastAttemptAt             *string                `json:"last_attempt_at"`
	LastSuccessReceivedAt     *string                `json:"last_success_received_at"`
	FreshnessDeadlineAt       *string                `json:"freshness_deadline_at"`
	ConsecutiveFailures       int64                  `json:"consecutive_failures"`
	CPUUsedPercent            *float64               `json:"cpu_used_percent"`
	MemoryUsedBytes           *int64                 `json:"memory_used_bytes"`
	MemoryTotalBytes          *int64                 `json:"memory_total_bytes"`
	UptimeSeconds             *int64                 `json:"uptime_seconds"`
	Tailscale                 *Tailscale             `json:"tailscale"`
	Warnings                  []string               `json:"warnings"`
	AgentVersion              *string                `json:"agent_version"`
	ProtocolVersion           *int64                 `json:"protocol_version"`
	MostConstrainedFilesystem *ConstrainedFilesystem `json:"most_constrained_filesystem"`
	Version                   int64                  `json:"version"`
}

type Snapshot struct {
	ReceivedAt         string    `json:"received_at"`
	ObservedAt         string    `json:"observed_at"`
	Hostname           string    `json:"hostname"`
	BootID             *string   `json:"boot_id"`
	UptimeSeconds      *int64    `json:"uptime_seconds"`
	CPUUsedPercent     *float64  `json:"cpu_used_percent"`
	MemoryUsedBytes    *int64    `json:"memory_used_bytes"`
	MemoryTotalBytes   *int64    `json:"memory_total_bytes"`
	Tailscale          Tailscale `json:"tailscale"`
	CollectionWarnings []string  `json:"collection_warnings"`
}

type HostDetail struct {
	HostSummary
	AgentURL string    `json:"agent_url"`
	Snapshot *Snapshot `json:"snapshot"`
}

type Filesystem struct {
	ID             string  `json:"id"`
	IdentityKind   string  `json:"identity_kind"`
	MountPoint     string  `json:"mount_point"`
	Device         string  `json:"device"`
	MountRoot      string  `json:"mount_root"`
	MajorMinor     string  `json:"major_minor"`
	FSType         string  `json:"fs_type"`
	Selected       bool    `json:"selected"`
	Present        bool    `json:"present"`
	UsedBytes      *int64  `json:"used_bytes"`
	TotalBytes     *int64  `json:"total_bytes"`
	AvailableBytes *int64  `json:"available_bytes"`
	LastObservedAt *string `json:"last_observed_at"`
	Version        int64   `json:"version"`
}
