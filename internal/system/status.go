// Package system contains the small, public-facing operational status model.
package system

import "context"

// WorkerStatus describes a background subsystem without exposing its internal
// error text. ErrorCode is an intentionally stable, machine-readable value.
type WorkerStatus struct {
	State           string  `json:"state"`
	ErrorCode       string  `json:"error_code"`
	LastHeartbeatAt *string `json:"last_heartbeat_at"`
}

// Status is the authenticated operational status contract. It contains no
// credentials, URLs with secrets, or raw process errors.
type Status struct {
	Version           string       `json:"version"`
	SchemaVersion     int          `json:"schema_version"`
	DatabaseSizeBytes int64        `json:"database_size_bytes"`
	LastHeartbeatAt   *string      `json:"last_heartbeat_at"`
	Collection        WorkerStatus `json:"collection"`
	Retention         WorkerStatus `json:"retention"`
	Checks            WorkerStatus `json:"checks"`
	Notifications     WorkerStatus `json:"notifications"`
}

// Provider supplies the status used by the authenticated system endpoint.
type Provider interface {
	SystemStatus(context.Context, string) (Status, error)
}
