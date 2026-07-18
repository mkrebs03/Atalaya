package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const ProtocolVersion = 1

type Snapshot struct {
	ProtocolVersion    int          `json:"protocol_version"`
	AgentVersion       string       `json:"agent_version"`
	Host               Host         `json:"host"`
	Tailscale          Tailscale    `json:"tailscale"`
	Filesystems        []Filesystem `json:"filesystems"`
	CollectionWarnings []string     `json:"collection_warnings"`
}
type Host struct {
	Hostname         string   `json:"hostname"`
	ObservedAt       string   `json:"observed_at"`
	BootID           *string  `json:"boot_id"`
	UptimeSeconds    *uint64  `json:"uptime_seconds"`
	CPUUsedPercent   *float64 `json:"cpu_used_percent"`
	MemoryUsedBytes  *uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes *uint64  `json:"memory_total_bytes"`
}
type Tailscale struct {
	State     string   `json:"state"`
	Hostname  *string  `json:"hostname"`
	Addresses []string `json:"addresses"`
}
type Filesystem struct {
	MountPoint     string  `json:"mount_point"`
	Device         string  `json:"device"`
	MountRoot      string  `json:"mount_root"`
	MajorMinor     string  `json:"major_minor"`
	FSType         string  `json:"fs_type"`
	FSUUID         *string `json:"fs_uuid"`
	UsedBytes      uint64  `json:"used_bytes"`
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
}

func (snapshot *Snapshot) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateJSONMembers(data); err != nil {
		return err
	}
	type snapshotAlias Snapshot
	var decoded snapshotAlias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return err
	}
	top, err := exactJSONObject(data, []string{"protocol_version", "agent_version", "host", "tailscale", "filesystems", "collection_warnings"}, nil)
	if err != nil {
		return err
	}
	if _, err = exactJSONObject(top["host"], []string{"hostname", "observed_at", "boot_id", "uptime_seconds", "cpu_used_percent", "memory_used_bytes", "memory_total_bytes"}, map[string]bool{"boot_id": true, "uptime_seconds": true, "cpu_used_percent": true, "memory_used_bytes": true, "memory_total_bytes": true}); err != nil {
		return fmt.Errorf("host: %w", err)
	}
	if _, err = exactJSONObject(top["tailscale"], []string{"state", "hostname", "addresses"}, map[string]bool{"hostname": true}); err != nil {
		return fmt.Errorf("tailscale: %w", err)
	}
	var filesystems []json.RawMessage
	if err = json.Unmarshal(top["filesystems"], &filesystems); err != nil || filesystems == nil {
		return fmt.Errorf("filesystems must be an array")
	}
	for index, filesystem := range filesystems {
		if _, err = exactJSONObject(filesystem, []string{"mount_point", "device", "fs_uuid", "mount_root", "major_minor", "fs_type", "used_bytes", "total_bytes", "available_bytes"}, map[string]bool{"fs_uuid": true}); err != nil {
			return fmt.Errorf("filesystem %d: %w", index, err)
		}
	}
	*snapshot = Snapshot(decoded)
	return nil
}

func rejectDuplicateJSONMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil {
		return err
	}
	if err = scanJSONValue(decoder, first); err != nil {
		return err
	}
	return ensureJSONEnd(decoder)
}

func scanJSONValue(decoder *json.Decoder, token json.Token) error {
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate or invalid JSON member %q", key)
			}
			seen[key] = true
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err = scanJSONValue(decoder, value); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err = scanJSONValue(decoder, value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	closing, err := decoder.Token()
	if err != nil || closing != matchingDelimiter(delimiter) {
		return fmt.Errorf("invalid JSON compound")
	}
	return nil
}

func matchingDelimiter(open json.Delim) json.Delim {
	if open == '{' {
		return '}'
	}
	return ']'
}

func exactJSONObject(data []byte, required []string, nullable map[string]bool) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) || json.Unmarshal(data, &object) != nil || object == nil {
		return nil, fmt.Errorf("required object is missing or null")
	}
	allowed := make(map[string]bool, len(required))
	for _, field := range required {
		allowed[field] = true
		value, exists := object[field]
		if !exists {
			return nil, fmt.Errorf("required field %q is missing", field)
		}
		if !nullable[field] && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("required field %q is null", field)
		}
	}
	for field := range object {
		if !allowed[field] {
			return nil, fmt.Errorf("unknown field %q", field)
		}
	}
	return object, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid trailing JSON")
	}
	return nil
}

type Collector interface {
	Collect(context.Context, time.Time) Snapshot
}
