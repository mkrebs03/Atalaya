package alerts

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestEvaluateSustainedConditionAndUnknownEvidence(t *testing.T) {
	hostID := "host-1"
	rule := Rule{ID: "rule-1", Name: "Host down", Type: RuleHostUnreachable, Severity: SeverityCritical, HostID: &hostID, ForSeconds: 60, ChannelIDs: []string{"channel-1"}, Enabled: true}
	start := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	current := Evaluation{RuleID: rule.ID, State: EvaluationInactive, LastConditionStatus: ConditionUnknown}

	transition, err := Evaluate(rule, current, Observation{At: start, Since: &start, Status: ConditionTrue})
	if err != nil || transition.Evaluation.State != EvaluationPending || transition.Opened {
		t.Fatalf("first transition=%+v err=%v", transition, err)
	}

	pending := transition.Evaluation
	transition, err = Evaluate(rule, pending, Observation{At: start.Add(59 * time.Second), Since: &start, Status: ConditionTrue})
	if err != nil || transition.Evaluation.State != EvaluationPending || transition.Opened {
		t.Fatalf("pending transition=%+v err=%v", transition, err)
	}

	pending = transition.Evaluation
	transition, err = Evaluate(rule, pending, Observation{At: start.Add(time.Minute), Since: &start, Status: ConditionTrue})
	if err != nil || transition.Evaluation.State != EvaluationFiring || !transition.Opened {
		t.Fatalf("firing transition=%+v err=%v", transition, err)
	}

	firing := transition.Evaluation
	transition, err = Evaluate(rule, firing, Observation{At: start.Add(2 * time.Minute), Status: ConditionUnknown})
	if err != nil || transition.Evaluation.State != EvaluationFiring || transition.Resolved || transition.Evaluation.LastConditionStatus != ConditionUnknown {
		t.Fatalf("unknown transition=%+v err=%v", transition, err)
	}

	firing = transition.Evaluation
	transition, err = Evaluate(rule, firing, Observation{At: start.Add(3 * time.Minute), Status: ConditionFalse})
	if err != nil || transition.Evaluation.State != EvaluationInactive || !transition.Resolved || transition.Evaluation.IncidentID != nil {
		t.Fatalf("resolved transition=%+v err=%v", transition, err)
	}
}

func TestEvaluateKeepsFirstEvidenceSeparateFromThresholdAnchor(t *testing.T) {
	hostID := "host-1"
	rule := Rule{ID: "rule-1", Name: "Host down", Type: RuleHostUnreachable, Severity: SeverityCritical, HostID: &hostID, ForSeconds: 60, ChannelIDs: []string{"channel-1"}, Enabled: true}
	start := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	firstFailure := start.Add(10 * time.Second)
	threshold := start.Add(-time.Minute)
	transition, err := Evaluate(rule, Evaluation{RuleID: rule.ID, State: EvaluationInactive, LastConditionStatus: ConditionUnknown}, Observation{
		At: firstFailure, ThresholdSince: &threshold, Status: ConditionTrue,
	})
	if err != nil || transition.Evaluation.State != EvaluationFiring || !transition.Opened {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	if transition.Evaluation.FirstTrueAt == nil || !transition.Evaluation.FirstTrueAt.Equal(firstFailure) {
		t.Fatalf("first_true_at=%v, want %v", transition.Evaluation.FirstTrueAt, firstFailure)
	}
}

func TestValidateRuleRejectsMixedDiscriminatorFields(t *testing.T) {
	hostID := "host-1"
	checkID := "check-1"
	rule := Rule{ID: "rule-1", Name: "invalid", Type: RuleHostUnreachable, Severity: SeverityWarning, HostID: &hostID, CheckID: &checkID, ChannelIDs: []string{"channel-1"}, Enabled: true}
	if err := ValidateRule(rule); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ValidateRule() error=%v, want ErrInvalid", err)
	}
}

func TestValidateRuleRejectsConditionFieldsFromAnotherVariant(t *testing.T) {
	hostID := "host-1"
	checkID := "check-1"
	filesystemID := "filesystem-1"
	tests := []Rule{
		{Name: "check metric", Type: RuleCheckFailed, Severity: SeverityWarning, CheckID: &checkID, CheckMode: CheckDuration, Metric: MetricCPUUsed, ChannelIDs: []string{"channel-1"}},
		{Name: "host mode", Type: RuleHostMetric, Severity: SeverityWarning, HostID: &hostID, CheckMode: CheckAttempts, Metric: MetricCPUUsed, Operator: OperatorGTE, Threshold: 90, ChannelIDs: []string{"channel-1"}},
		{Name: "filesystem mode", Type: RuleFilesystem, Severity: SeverityWarning, FilesystemID: &filesystemID, CheckMode: CheckDuration, Operator: OperatorGTE, Threshold: 90, ChannelIDs: []string{"channel-1"}},
		{Name: "nan threshold", Type: RuleHostMetric, Severity: SeverityWarning, HostID: &hostID, Metric: MetricCPUUsed, Operator: OperatorGTE, Threshold: math.NaN(), ChannelIDs: []string{"channel-1"}},
	}
	for _, rule := range tests {
		if err := ValidateRule(rule); !errors.Is(err, ErrInvalid) {
			t.Errorf("ValidateRule(%q) error=%v, want ErrInvalid", rule.Name, err)
		}
	}
}

func TestValidateRuleRejectsDuplicateChannels(t *testing.T) {
	hostID := "host-1"
	rule := Rule{Name: "duplicate channels", Type: RuleHostUnreachable, Severity: SeverityWarning, HostID: &hostID, ChannelIDs: []string{"channel-1", "channel-1"}}
	if err := ValidateRule(rule); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ValidateRule() error=%v, want ErrInvalid", err)
	}
}
