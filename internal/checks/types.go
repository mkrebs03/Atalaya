// Package checks defines service-check configuration, results, and worker contracts.
package checks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	MinIntervalSeconds = 15
	MaxIntervalSeconds = 3600
	MinTimeoutSeconds  = 1
	MaxTimeoutSeconds  = 60
	DefaultInterval    = 60 * time.Second
	DefaultTimeout     = 10 * time.Second
	MaxRedirects       = 3
	MaxNameBytes       = 128
	MaxRequestIDBytes  = 128
)

var (
	ErrNotFound        = errors.New("check not found")
	ErrVersionConflict = errors.New("check version conflict")
	ErrArchived        = errors.New("check is archived")
	ErrInvalid         = errors.New("invalid check")
	ErrNoDueSlot       = errors.New("no due check slot")
	ErrRunNotFound     = errors.New("check run not found")
	ErrRunConflict     = errors.New("check run request conflict")
	ErrInvalidSettings = errors.New("invalid check settings")
)

type Type string

const (
	TypeHTTP Type = "http"
	TypeTCP  Type = "tcp"
)

type State string

const (
	StateUnknown State = "unknown"
	StateUp      State = "up"
	StateDown    State = "down"
)

type ErrorCode string

const (
	ErrorDNS           ErrorCode = "dns"
	ErrorTimeout       ErrorCode = "timeout"
	ErrorConnect       ErrorCode = "connect"
	ErrorTLS           ErrorCode = "tls"
	ErrorHTTPStatus    ErrorCode = "http_status"
	ErrorInvalidTarget ErrorCode = "invalid_target"
	ErrorInternal      ErrorCode = "internal"
)

type HTTPConfig struct {
	URL               string
	IntervalSeconds   int
	TimeoutSeconds    int
	ExpectedStatusMin int
	ExpectedStatusMax int
	FollowRedirects   bool
	MaxRedirects      int
}

type TCPConfig struct {
	Host            string
	Port            int
	IntervalSeconds int
	TimeoutSeconds  int
}

type Config struct {
	Type Type
	HTTP *HTTPConfig
	TCP  *TCPConfig
}

func (config Config) Interval() time.Duration {
	if config.HTTP != nil {
		return time.Duration(config.HTTP.IntervalSeconds) * time.Second
	}
	if config.TCP != nil {
		return time.Duration(config.TCP.IntervalSeconds) * time.Second
	}
	return 0
}

func (config Config) Timeout() time.Duration {
	if config.HTTP != nil {
		return time.Duration(config.HTTP.TimeoutSeconds) * time.Second
	}
	if config.TCP != nil {
		return time.Duration(config.TCP.TimeoutSeconds) * time.Second
	}
	return 0
}

type Check struct {
	ID                      string
	Name                    string
	RelatedHostID           *string
	Config                  Config
	Enabled                 bool
	ArchivedAt              *time.Time
	ObservedState           State
	LastAttemptAt           *time.Time
	LastResultAt            *time.Time
	FreshnessDeadlineAt     *time.Time
	ConsecutiveFailures     int64
	LastLatencyMS           *int64
	LastErrorCode           *ErrorCode
	LastHTTPStatus          *int
	CreatedAt               time.Time
	UpdatedAt               time.Time
	Version                 int64
	ConfigurationGeneration int64
}

type Result struct {
	Status     State
	LatencyMS  *int64
	ErrorCode  *ErrorCode
	HTTPStatus *int
	ReceivedAt time.Time
}

type ScheduledClaim struct {
	SlotID     string
	CheckID    string
	Config     Config
	Attempt    int
	LeaseUntil time.Time
	Generation int64
	NominalAt  time.Time
	DeadlineAt time.Time
	Interval   time.Duration
	Timeout    time.Duration
}

type ManualRun struct {
	ID              string
	CheckID         string
	RequestID       string
	State           string
	RequestedAt     time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	LeaseUntil      *time.Time
	ProcessAttempts int
	Config          Config
	Result          *Result
	FailureCode     string
}

type ManualClaim struct {
	RunID      string
	CheckID    string
	Config     Config
	Attempt    int
	LeaseUntil time.Time
}

type Repository interface {
	List(context.Context, int, *Cursor, bool) ([]Check, error)
	Get(context.Context, string) (Check, error)
	Create(context.Context, Check, time.Time, IDSource) (Check, error)
	Update(context.Context, string, int64, Check, time.Time, IDSource) error
	Archive(context.Context, string, int64, time.Time) error
	SetEnabled(context.Context, string, int64, bool, time.Time, IDSource) error
	Restore(context.Context, string, int64, time.Time) error
	ListRuns(context.Context, string, int, string) ([]ManualRun, error)
	GetRun(context.Context, string, string) (ManualRun, error)
	EnqueueRun(context.Context, string, string, time.Time, IDSource) (ManualRun, bool, error)
}

type SchedulerRepository interface {
	MaintainScheduled(context.Context, time.Time, IDSource) error
	ClaimScheduled(context.Context, time.Time) (ScheduledClaim, error)
	FinishScheduled(context.Context, ScheduledClaim, Result, IDSource) error
	ReleaseScheduled(context.Context, ScheduledClaim, time.Time) error
}

type ManualRepository interface {
	ClaimManualRun(context.Context, time.Time) (ManualClaim, error)
	FinishManualRun(context.Context, ManualClaim, Result, string, time.Time) error
	ReleaseManualRun(context.Context, ManualClaim, time.Time) error
}

type IDSource interface {
	ID() (string, error)
	Intn(int) int
}

type Cursor struct {
	Name string
	ID   string
}

type Settings struct {
	IntervalSeconds     int
	TimeoutSeconds      int
	AllowedPrivateCIDRs []string
	UpdatedAt           time.Time
	Version             int64
}

type SettingsPatch struct {
	IntervalSeconds     *int
	TimeoutSeconds      *int
	AllowedPrivateCIDRs *[]string
}

type ReaderIDSource struct {
	Reader io.Reader
}

func NewReaderIDSource(reader io.Reader) IDSource { return ReaderIDSource{Reader: reader} }

func (source ReaderIDSource) ID() (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(source.Reader, value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func (source ReaderIDSource) Intn(max int) int {
	if max <= 1 {
		return 0
	}
	var value [1]byte
	limit := 256 - (256 % max)
	for {
		if _, err := io.ReadFull(source.Reader, value[:]); err != nil {
			return 0
		}
		if int(value[0]) < limit {
			return int(value[0]) % max
		}
	}
}

type Executor interface {
	Probe(context.Context, Config) Result
}

func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > MaxNameBytes {
		return fmt.Errorf("%w: name", ErrInvalid)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: name", ErrInvalid)
		}
	}
	return nil
}

func ValidateRequestID(requestID string) error {
	if strings.TrimSpace(requestID) == "" || len(requestID) > MaxRequestIDBytes {
		return fmt.Errorf("%w: request_id", ErrInvalid)
	}
	for _, r := range requestID {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: request_id", ErrInvalid)
		}
	}
	return nil
}

func ValidateConfig(config Config) error {
	switch config.Type {
	case TypeHTTP:
		if config.HTTP == nil || config.TCP != nil {
			return fmt.Errorf("%w: http configuration", ErrInvalid)
		}
		if err := validateSchedule(config.HTTP.IntervalSeconds, config.HTTP.TimeoutSeconds); err != nil {
			return err
		}
		parsed, err := url.Parse(config.HTTP.URL)
		if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Hostname() == "" {
			return fmt.Errorf("%w: http url", ErrInvalid)
		}
		if parsed.Fragment != "" {
			return fmt.Errorf("%w: http fragment", ErrInvalid)
		}
		if port := parsed.Port(); port != "" {
			value, err := strconv.Atoi(port)
			if err != nil || value < 1 || value > 65535 {
				return fmt.Errorf("%w: http port", ErrInvalid)
			}
		}
		if config.HTTP.ExpectedStatusMin < 100 || config.HTTP.ExpectedStatusMin > 599 || config.HTTP.ExpectedStatusMax < config.HTTP.ExpectedStatusMin || config.HTTP.ExpectedStatusMax > 599 {
			return fmt.Errorf("%w: expected status", ErrInvalid)
		}
		if config.HTTP.MaxRedirects < 1 || config.HTTP.MaxRedirects > MaxRedirects {
			return fmt.Errorf("%w: max redirects", ErrInvalid)
		}
		return nil
	case TypeTCP:
		if config.TCP == nil || config.HTTP != nil || strings.TrimSpace(config.TCP.Host) == "" || strings.ContainsAny(config.TCP.Host, "\t\r\n /\\") {
			return fmt.Errorf("%w: tcp host", ErrInvalid)
		}
		if config.TCP.Port < 1 || config.TCP.Port > 65535 {
			return fmt.Errorf("%w: tcp port", ErrInvalid)
		}
		return validateSchedule(config.TCP.IntervalSeconds, config.TCP.TimeoutSeconds)
	default:
		return fmt.Errorf("%w: type", ErrInvalid)
	}
}

func validateSchedule(interval, timeout int) error {
	if interval < MinIntervalSeconds || interval > MaxIntervalSeconds || timeout < MinTimeoutSeconds || timeout > MaxTimeoutSeconds || timeout >= interval {
		return fmt.Errorf("%w: schedule", ErrInvalid)
	}
	return nil
}
