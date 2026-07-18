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

// Decision values. Deny/AllowOnce/AllowSession's numeric values match
// HEWN.md's tool_calls.approved column ("0 denied, 1 once, 2
// session-wide"); DecisionNotGated has no place in that column (a
// read-only or yolo-bypassed call is persisted with a nil approved value
// instead) and exists purely so Check can report what actually happened.
const (
	DecisionDeny Decision = iota
	DecisionAllowOnce
	DecisionAllowSession
	DecisionNotGated
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

// Check gates a call to the named tool. A nil error means the call may
// proceed; a non-nil error (typically wrapping ErrDenied) means it must
// not run. The returned Decision records what actually happened, for
// persistence -- DecisionNotGated for a read-only or yolo-bypassed call
// that was never actually put to the approver.
func (p *Policy) Check(ctx context.Context, name string, risk RiskLevel, params json.RawMessage) (Decision, error) {
	if risk == RiskReadOnly || p.yolo {
		return DecisionNotGated, nil
	}

	p.mu.Lock()
	allowed := p.sessionAllow[name]
	p.mu.Unlock()
	if allowed {
		return DecisionAllowSession, nil
	}

	decision, feedback, err := p.approver.RequestApproval(ctx, ApprovalRequest{Tool: name, Params: params})
	if err != nil {
		return DecisionDeny, fmt.Errorf("tool: request approval for %s: %w", name, err)
	}

	switch decision {
	case DecisionAllowOnce:
		return DecisionAllowOnce, nil
	case DecisionAllowSession:
		p.mu.Lock()
		p.sessionAllow[name] = true
		p.mu.Unlock()
		return DecisionAllowSession, nil
	case DecisionDeny:
		if feedback != "" {
			return DecisionDeny, fmt.Errorf("%w: %s", ErrDenied, feedback)
		}
		return DecisionDeny, ErrDenied
	case DecisionNotGated:
		// An Approver must never return this -- it's Policy's own signal
		// for calls it never put to a decision. Treat a misbehaving
		// Approver the same as an explicit deny.
		return DecisionDeny, ErrDenied
	default:
		return DecisionDeny, ErrDenied
	}
}
