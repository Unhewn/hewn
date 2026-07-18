package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Decision is a human's answer to an ApprovalRequest.
type Decision int

// Decision values.
const (
	DecisionDeny Decision = iota
	DecisionAllowOnce
	DecisionAllowSession
)

// ErrDenied is returned by Policy.Check when a call was denied.
var ErrDenied = errors.New("tool: call denied")

// ApprovalRequest describes one call awaiting a decision.
type ApprovalRequest struct {
	Tool   string
	Params json.RawMessage
}

// Approver asks something outside the loop to decide on a call: a blocking
// terminal prompt in headless mode, eventually an inline TUI widget.
type Approver interface {
	// RequestApproval returns the decision and, for a deny, an optional
	// feedback string to surface back to the model.
	RequestApproval(ctx context.Context, req ApprovalRequest) (Decision, string, error)
}

// Policy gates Mutating/Arbitrary-risk tool calls (AGENTS.md invariant #5).
// Allow-session decisions are tracked in memory for the life of one process;
// there is no cross-run persistence yet (that's SQLite's job, deferred).
type Policy struct {
	approver Approver
	yolo     bool

	mu           sync.Mutex
	sessionAllow map[string]bool
}

// NewPolicy builds a Policy. When yolo is true, every call is pre-approved
// and approver is never consulted -- callers must only set this from an
// explicit flag, never as a silent default.
func NewPolicy(approver Approver, yolo bool) *Policy {
	return &Policy{approver: approver, yolo: yolo, sessionAllow: map[string]bool{}}
}

// Check gates a call to the named tool. A nil return means the call may
// proceed; a non-nil return (typically wrapping ErrDenied) means it must
// not run.
func (p *Policy) Check(ctx context.Context, name string, risk RiskLevel, params json.RawMessage) error {
	if risk == RiskReadOnly || p.yolo {
		return nil
	}

	p.mu.Lock()
	allowed := p.sessionAllow[name]
	p.mu.Unlock()
	if allowed {
		return nil
	}

	decision, feedback, err := p.approver.RequestApproval(ctx, ApprovalRequest{Tool: name, Params: params})
	if err != nil {
		return fmt.Errorf("tool: request approval for %s: %w", name, err)
	}

	switch decision {
	case DecisionAllowOnce:
		return nil
	case DecisionAllowSession:
		p.mu.Lock()
		p.sessionAllow[name] = true
		p.mu.Unlock()
		return nil
	case DecisionDeny:
		if feedback != "" {
			return fmt.Errorf("%w: %s", ErrDenied, feedback)
		}
		return ErrDenied
	default:
		return ErrDenied
	}
}
