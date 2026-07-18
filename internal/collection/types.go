// Package collection schedules authenticated pulls from Atalaya agents.
package collection

import (
	"context"
	"errors"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
)

const (
	DefaultConcurrency = 4
	MaxPayloadBytes    = 256 << 10
)

type Clock interface{ Now() time.Time }
type Random interface {
	Intn(int) int
	ID() (string, error)
}

type Claim struct {
	SlotID, HostID, AgentURL string
	Credential               []byte
	NominalAt, DeadlineAt    time.Time
	Interval, Timeout        time.Duration
	CollectionGeneration     int64
}

func (c *Claim) Clear() { clear(c.Credential) }

type Result struct {
	Snapshot   *agent.Snapshot
	ReceivedAt time.Time
	Failure    FailureKind
}

type FailureKind string

const (
	FailureDNS      FailureKind = "dns"
	FailureTimeout  FailureKind = "timeout"
	FailureConnect  FailureKind = "connect"
	FailureTLS      FailureKind = "tls"
	FailureAuth     FailureKind = "auth"
	FailureProtocol FailureKind = "protocol"
	FailurePayload  FailureKind = "payload"
	FailureInternal FailureKind = "internal"
)

type PullError struct{ Kind FailureKind }

func (e *PullError) Error() string { return "agent pull failed: " + string(e.Kind) }

var ErrNoDueSlot = errors.New("no due schedule slot")

type Repository interface {
	Maintain(context.Context, time.Time, Random) error
	ClaimDue(context.Context, time.Time) (Claim, error)
	Finish(context.Context, Claim, Result, Random) error
	Release(context.Context, Claim, time.Time) error
}

type Puller interface {
	Pull(context.Context, Claim) (agent.Snapshot, error)
}
