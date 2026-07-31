package hosts

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/mkrebs03/atalaya/internal/hostquery"
)

const (
	EnrollmentTokenBytes = 32
	EnrollmentTTL        = 15 * time.Minute
	EnrollmentReplayTTL  = 15 * time.Minute
	MinScheduleInterval  = 15
	MaxScheduleInterval  = 3600
	MinScheduleTimeout   = 1
	MaxScheduleTimeout   = 60
)

var (
	ErrEnrollmentInvalid   = errors.New("enrollment token is invalid")
	ErrEnrollmentConsumed  = errors.New("enrollment token has already been consumed")
	ErrEnrollmentConflict  = errors.New("enrollment request conflicts with an existing host")
	ErrHostVersionRequired = errors.New("host version is required")
	ErrHostVersionConflict = errors.New("host version conflict")
	ErrHostArchived        = errors.New("host is archived")
	ErrHostDisabled        = errors.New("host is disabled")
	ErrHostConflict        = errors.New("host conflicts with an existing resource")
	ErrFilesystemNotFound  = errors.New("filesystem not found")
	ErrInvalidManagement   = errors.New("invalid host management request")
)

type EnrollmentToken struct {
	ID          string
	Label       *string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
	ConsumedAt  *time.Time
	ReplayUntil *time.Time
	HostID      *string
	Version     int64
}

type EnrollmentRequest struct {
	Token           string
	RequestID       string
	DisplayName     string
	AgentURL        string
	AgentVersion    string
	ProtocolVersion int
}

type EnrollmentResponse struct {
	HostID              string
	AgentCredential     []byte
	PollIntervalSeconds int
}

type HostPatch struct {
	DisplayName     *string `json:"display_name"`
	IntervalSeconds *int    `json:"interval_seconds"`
	TimeoutSeconds  *int    `json:"timeout_seconds"`
}

type ManagementRepository interface {
	CreateEnrollmentToken(context.Context, string, string, time.Time, io.Reader) (EnrollmentToken, string, error)
	ListEnrollmentTokens(context.Context, int, string) ([]EnrollmentToken, error)
	RevokeEnrollmentToken(context.Context, string, int64, time.Time) error
	EnrollAgent(context.Context, EnrollmentRequest, time.Time, io.Reader) (EnrollmentResponse, error)
	PatchHost(context.Context, string, int64, HostPatch, time.Time, io.Reader) error
	SetHostAdministrativeState(context.Context, string, int64, string, time.Time, io.Reader) error
	RestoreHost(context.Context, string, int64, time.Time) error
	SelectFilesystem(context.Context, string, string, int64, bool, time.Time) error
	ListHostsIncludingArchived(context.Context, int, *hostquery.HostCursor) ([]hostquery.HostSummary, error)
}

func ValidateEnrollmentRequest(request EnrollmentRequest) error {
	if len(request.Token) < 32 || len(request.Token) > 128 || request.RequestID == "" || len(request.RequestID) > 128 {
		return ErrInvalidManagement
	}
	if request.DisplayName == "" || len(request.DisplayName) > 128 {
		return ErrInvalidManagement
	}
	if request.AgentVersion == "" || len(request.AgentVersion) > 64 || request.ProtocolVersion != 1 {
		return ErrInvalidManagement
	}
	if err := validateText(request.DisplayName, 128); err != nil {
		return err
	}
	if err := validateText(request.RequestID, 128); err != nil {
		return err
	}
	if err := validateText(request.AgentVersion, 64); err != nil {
		return err
	}
	return nil
}

func validateText(value string, maxBytes int) error {
	if value == "" || len(value) > maxBytes {
		return ErrInvalidManagement
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ErrInvalidManagement
		}
	}
	return nil
}

func ValidateSchedule(interval, timeout int) error {
	if interval < MinScheduleInterval || interval > MaxScheduleInterval || timeout < MinScheduleTimeout || timeout > MaxScheduleTimeout || timeout >= interval {
		return ErrInvalidManagement
	}
	return nil
}
