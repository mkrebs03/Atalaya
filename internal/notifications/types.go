// Package notifications defines persistent delivery contracts and providers.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrNotFound        = errors.New("notification resource not found")
	ErrVersionConflict = errors.New("notification resource version conflict")
	ErrInvalid         = errors.New("invalid notification resource")
	ErrArchived        = errors.New("notification channel is archived")
	ErrNoDueDelivery   = errors.New("no due notification delivery")
)

type Provider string

const ProviderDiscord Provider = "discord"

type DeliveryKind string

const (
	KindIncident DeliveryKind = "incident"
	KindTest     DeliveryKind = "test"
)

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliverySending   DeliveryState = "sending"
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryFailed    DeliveryState = "failed"
	DeliverySkipped   DeliveryState = "skipped"
)

type Transition string

const (
	TransitionFiring   Transition = "firing"
	TransitionResolved Transition = "resolved"
)

type Channel struct {
	ID               string
	Name             string
	Provider         Provider
	SecretConfigured bool
	Enabled          bool
	ArchivedAt       *time.Time
	LastTestAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int64
}

type Delivery struct {
	ID            string
	Kind          DeliveryKind
	ChannelID     string
	IncidentID    *string
	Transition    *Transition
	TestRequestID *string
	State         DeliveryState
	Attempts      int
	NextAttemptAt time.Time
	LeaseUntil    *time.Time
	LastErrorCode string
	SkippedReason string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeliveredAt   *time.Time
}

type DeliveryClaim struct {
	DeliveryID string
	ChannelID  string
	Kind       DeliveryKind
	Provider   Provider
	WebhookURL string
	Payload    []byte
	Attempt    int
	LeaseUntil time.Time
}

func (claim *DeliveryClaim) Clear() {
	claim.WebhookURL = ""
	clear(claim.Payload)
	claim.Payload = nil
}

type SendResult struct {
	StatusCode int
	RetryAfter time.Duration
	ErrorCode  string
	Err        error
}

type DeliveryOutcome struct {
	Delivered  bool
	Retryable  bool
	ErrorCode  string
	RetryAfter time.Duration
}

type IDSource interface {
	ID() (string, error)
	Intn(int) int
}

type Clock interface {
	Now() time.Time
}

type ChannelCursor struct {
	Name string
	ID   string
}

type DeliveryCursor struct {
	CreatedAt string
	ID        string
}

type Repository interface {
	ListChannels(context.Context, int, *ChannelCursor, bool) ([]Channel, error)
	GetChannel(context.Context, string) (Channel, error)
	CreateChannel(context.Context, Channel, string, time.Time, IDSource) (Channel, error)
	UpdateChannel(context.Context, string, int64, Channel, *string, time.Time) error
	ArchiveChannel(context.Context, string, int64, time.Time) error
	RestoreChannel(context.Context, string, int64, time.Time) error
	EnqueueTest(context.Context, string, string, time.Time, IDSource) (Delivery, bool, error)
	ListDeliveries(context.Context, string, string, int, *DeliveryCursor) ([]Delivery, error)
	GetDelivery(context.Context, string) (Delivery, error)
	ClaimDelivery(context.Context, time.Time) (DeliveryClaim, error)
	FinishDelivery(context.Context, DeliveryClaim, DeliveryOutcome, time.Time, IDSource) error
}

type Sender interface {
	Send(context.Context, DeliveryClaim) SendResult
}

func ValidateChannelName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 128 {
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
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 {
		return fmt.Errorf("%w: request_id", ErrInvalid)
	}
	for _, r := range requestID {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: request_id", ErrInvalid)
		}
	}
	return nil
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
