package collection

import "github.com/mkrebs03/atalaya/internal/agent"

func validateSnapshot(s agent.Snapshot) error {
	if agent.ValidateSnapshot(s) != nil {
		return &PullError{Kind: FailureProtocol}
	}
	return nil
}
