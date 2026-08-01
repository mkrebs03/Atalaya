// Package alerts defines alert rules and their deterministic state machine.
package alerts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

var (
	ErrNotFound        = errors.New("alert resource not found")
	ErrVersionConflict = errors.New("alert resource version conflict")
	ErrInvalid         = errors.New("invalid alert resource")
	ErrArchived        = errors.New("alert rule is archived")
	ErrTargetNotFound  = errors.New("alert target not found")
	ErrChannelInvalid  = errors.New("notification channel is invalid")
)

type RuleType string

const (
	RuleHostUnreachable RuleType = "host_unreachable"
	RuleCheckFailed     RuleType = "check_failed"
	RuleHostMetric      RuleType = "host_metric_threshold"
	RuleFilesystem      RuleType = "filesystem_threshold"
)

type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type EvaluationState string

const (
	EvaluationInactive EvaluationState = "inactive"
	EvaluationPending  EvaluationState = "pending"
	EvaluationFiring   EvaluationState = "firing"
)

type ConditionStatus string

const (
	ConditionTrue    ConditionStatus = "true"
	ConditionFalse   ConditionStatus = "false"
	ConditionUnknown ConditionStatus = "unknown"
)

type CheckMode string

const (
	CheckDuration CheckMode = "duration"
	CheckAttempts CheckMode = "attempts"
)

type Metric string

const (
	MetricCPUUsed    Metric = "cpu_used_percent"
	MetricMemoryUsed Metric = "memory_used_percent"
)

type Operator string

const (
	OperatorGT  Operator = "gt"
	OperatorGTE Operator = "gte"
)

type Rule struct {
	ID                  string
	Name                string
	Type                RuleType
	Severity            Severity
	HostID              *string
	CheckID             *string
	FilesystemID        *string
	CheckMode           CheckMode
	ForSeconds          int
	ConsecutiveFailures int
	Metric              Metric
	Operator            Operator
	Threshold           float64
	ChannelIDs          []string
	Enabled             bool
	ArchivedAt          *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Version             int64
	Evaluation          Evaluation
}

type Evaluation struct {
	RuleID              string
	State               EvaluationState
	FirstTrueAt         *time.Time
	LastEvaluatedAt     *time.Time
	UpdatedAt           *time.Time
	LastValue           *float64
	LastConditionStatus ConditionStatus
	IncidentID          *string
}

type Incident struct {
	ID               string
	RuleID           string
	RuleName         string
	RuleType         RuleType
	TargetID         string
	TargetName       string
	Severity         Severity
	State            string
	ConditionStatus  ConditionStatus
	StartedAt        time.Time
	FiredAt          time.Time
	ResolvedAt       *time.Time
	InitialValue     *float64
	LastValue        *float64
	ResolutionReason string
}

type Observation struct {
	// Since marks when the current true evidence began. ThresholdSince is an
	// optional independent timestamp used for duration rules whose age is
	// measured from an external anchor, such as the last successful pull.
	At             time.Time
	Since          *time.Time
	ThresholdSince *time.Time
	Status         ConditionStatus
	Value          *float64
}

type RuleCursor struct {
	Name string
	ID   string
}

type IncidentCursor struct {
	FiredAt time.Time
	ID      string
}

type Repository interface {
	ListAlertRules(context.Context, int, *RuleCursor, bool) ([]Rule, error)
	GetAlertRule(context.Context, string) (Rule, error)
	CreateAlertRule(context.Context, Rule, time.Time, IDSource) (Rule, error)
	UpdateAlertRule(context.Context, string, int64, Rule, time.Time) error
	SetAlertRuleEnabled(context.Context, string, int64, bool, time.Time) error
	ArchiveAlertRule(context.Context, string, int64, time.Time) error
	RestoreAlertRule(context.Context, string, int64, time.Time) error
	ListIncidents(context.Context, string, string, int, *IncidentCursor) ([]Incident, error)
	GetIncident(context.Context, string) (Incident, error)
}

type IDSource interface {
	ID() (string, error)
	Intn(int) int
}

func NewReaderIDSource(reader io.Reader) IDSource {
	return readerIDSource{reader: reader}
}

type readerIDSource struct{ reader io.Reader }

func (source readerIDSource) ID() (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(source.reader, value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func (source readerIDSource) Intn(max int) int {
	if max <= 1 {
		return 0
	}
	var value [1]byte
	limit := 256 - (256 % max)
	for {
		if _, err := io.ReadFull(source.reader, value[:]); err != nil {
			return 0
		}
		if int(value[0]) < limit {
			return int(value[0]) % max
		}
	}
}

type Transition struct {
	Evaluation Evaluation
	Opened     bool
	Resolved   bool
}

func ValidateRule(rule Rule) error {
	if strings.TrimSpace(rule.Name) == "" || len(rule.Name) > 128 {
		return fmt.Errorf("%w: name", ErrInvalid)
	}
	for _, r := range rule.Name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: name", ErrInvalid)
		}
	}
	if rule.Severity != SeverityWarning && rule.Severity != SeverityCritical {
		return fmt.Errorf("%w: severity", ErrInvalid)
	}
	if rule.ForSeconds < 0 || rule.ForSeconds > 86400 {
		return fmt.Errorf("%w: duration", ErrInvalid)
	}
	if len(rule.ChannelIDs) == 0 {
		return fmt.Errorf("%w: at least one notification channel is required", ErrInvalid)
	}
	seenChannels := make(map[string]struct{}, len(rule.ChannelIDs))
	for _, channelID := range rule.ChannelIDs {
		if strings.TrimSpace(channelID) == "" {
			return fmt.Errorf("%w: channel", ErrInvalid)
		}
		if _, exists := seenChannels[channelID]; exists {
			return fmt.Errorf("%w: duplicate channel", ErrInvalid)
		}
		seenChannels[channelID] = struct{}{}
	}

	countTargets := 0
	if rule.HostID != nil {
		countTargets++
	}
	if rule.CheckID != nil {
		countTargets++
	}
	if rule.FilesystemID != nil {
		countTargets++
	}
	if countTargets != 1 {
		return fmt.Errorf("%w: exactly one target is required", ErrInvalid)
	}

	switch rule.Type {
	case RuleHostUnreachable:
		if rule.HostID == nil || rule.CheckID != nil || rule.FilesystemID != nil || rule.CheckMode != "" || rule.ConsecutiveFailures != 0 || rule.Metric != "" || rule.Operator != "" || rule.Threshold != 0 {
			return fmt.Errorf("%w: host unreachable condition", ErrInvalid)
		}
	case RuleCheckFailed:
		if rule.CheckID == nil || rule.HostID != nil || rule.FilesystemID != nil || rule.Metric != "" || rule.Operator != "" || rule.Threshold != 0 || (rule.CheckMode != CheckDuration && rule.CheckMode != CheckAttempts) {
			return fmt.Errorf("%w: check failed condition", ErrInvalid)
		}
		if rule.CheckMode == CheckDuration {
			if rule.ConsecutiveFailures != 0 {
				return fmt.Errorf("%w: check duration condition", ErrInvalid)
			}
		} else {
			if rule.ConsecutiveFailures < 1 || rule.ConsecutiveFailures > 1000 || rule.ForSeconds != 0 {
				return fmt.Errorf("%w: check attempts condition", ErrInvalid)
			}
		}
	case RuleHostMetric:
		if rule.HostID == nil || rule.CheckID != nil || rule.FilesystemID != nil || rule.CheckMode != "" || rule.ConsecutiveFailures != 0 || (rule.Metric != MetricCPUUsed && rule.Metric != MetricMemoryUsed) || (rule.Operator != OperatorGT && rule.Operator != OperatorGTE) || !validThreshold(rule.Threshold) {
			return fmt.Errorf("%w: host metric condition", ErrInvalid)
		}
	case RuleFilesystem:
		if rule.FilesystemID == nil || rule.HostID != nil || rule.CheckID != nil || rule.CheckMode != "" || rule.ConsecutiveFailures != 0 || rule.Metric != "" || (rule.Operator != OperatorGT && rule.Operator != OperatorGTE) || !validThreshold(rule.Threshold) {
			return fmt.Errorf("%w: filesystem condition", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: type", ErrInvalid)
	}
	return nil
}

func validThreshold(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

// Evaluate advances one evaluation using only the evidence supplied by the
// caller. It never resolves a firing incident on unknown evidence.
func Evaluate(rule Rule, current Evaluation, observation Observation) (Transition, error) {
	if err := ValidateRule(rule); err != nil {
		return Transition{}, err
	}
	if observation.At.IsZero() {
		return Transition{}, fmt.Errorf("%w: observation time", ErrInvalid)
	}
	if current.RuleID == "" {
		current.RuleID = rule.ID
	}
	if current.State == "" {
		current.State = EvaluationInactive
	}
	if current.LastConditionStatus == "" {
		current.LastConditionStatus = ConditionUnknown
	}
	current.LastEvaluatedAt = timePtr(observation.At)
	current.LastValue = cloneFloat(observation.Value)
	current.LastConditionStatus = observation.Status

	transition := Transition{Evaluation: current}
	switch observation.Status {
	case ConditionUnknown:
		if current.State != EvaluationFiring {
			transition.Evaluation.State = EvaluationInactive
			transition.Evaluation.FirstTrueAt = nil
		}
	case ConditionFalse:
		if current.State == EvaluationFiring {
			transition.Resolved = true
		}
		transition.Evaluation.State = EvaluationInactive
		transition.Evaluation.FirstTrueAt = nil
		transition.Evaluation.IncidentID = nil
	case ConditionTrue:
		firstTrueAt := observation.At
		if observation.Since != nil && !observation.Since.IsZero() {
			firstTrueAt = observation.Since.UTC()
		}
		thresholdSince := firstTrueAt
		if observation.ThresholdSince != nil && !observation.ThresholdSince.IsZero() {
			thresholdSince = observation.ThresholdSince.UTC()
		}
		switch current.State {
		case EvaluationFiring:
			// Remain firing.
		case EvaluationPending:
			if current.FirstTrueAt == nil {
				transition.Evaluation.FirstTrueAt = timePtr(firstTrueAt)
			}
			if observation.At.Sub(thresholdSince) >= time.Duration(rule.ForSeconds)*time.Second {
				transition.Evaluation.State = EvaluationFiring
				transition.Opened = true
			}
		case EvaluationInactive:
			transition.Evaluation.State = EvaluationPending
			transition.Evaluation.FirstTrueAt = timePtr(firstTrueAt)
			if observation.At.Sub(thresholdSince) >= time.Duration(rule.ForSeconds)*time.Second {
				transition.Evaluation.State = EvaluationFiring
				transition.Opened = true
			}
		default:
			return Transition{}, fmt.Errorf("%w: evaluation state", ErrInvalid)
		}
	default:
		return Transition{}, fmt.Errorf("%w: condition status", ErrInvalid)
	}
	return transition, nil
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
