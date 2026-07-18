package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeApprover struct {
	decision Decision
	feedback string
	err      error
	calls    int
}

func (f *fakeApprover) RequestApproval(context.Context, ApprovalRequest) (Decision, string, error) {
	f.calls++
	return f.decision, f.feedback, f.err
}

func TestPolicy_ReadOnlyNeverPrompts(t *testing.T) {
	ap := &fakeApprover{decision: DecisionDeny}
	p := NewPolicy(ap, false)

	if err := p.Check(context.Background(), "read", RiskReadOnly, nil); err != nil {
		t.Errorf("Check(read-only) = %v, want nil", err)
	}
	if ap.calls != 0 {
		t.Errorf("approver called %d times, want 0", ap.calls)
	}
}

func TestPolicy_Yolo(t *testing.T) {
	ap := &fakeApprover{decision: DecisionDeny}
	p := NewPolicy(ap, true)

	if err := p.Check(context.Background(), "bash", RiskArbitrary, nil); err != nil {
		t.Errorf("Check(yolo) = %v, want nil", err)
	}
	if ap.calls != 0 {
		t.Errorf("approver called %d times under yolo, want 0", ap.calls)
	}
}

func TestPolicy_AllowOnce(t *testing.T) {
	ap := &fakeApprover{decision: DecisionAllowOnce}
	p := NewPolicy(ap, false)

	if err := p.Check(context.Background(), "bash", RiskArbitrary, nil); err != nil {
		t.Fatalf("Check #1 = %v, want nil", err)
	}
	// Allow-once must not stick: the next call prompts again.
	if err := p.Check(context.Background(), "bash", RiskArbitrary, nil); err != nil {
		t.Fatalf("Check #2 = %v, want nil", err)
	}
	if ap.calls != 2 {
		t.Errorf("approver called %d times, want 2 (allow-once doesn't persist)", ap.calls)
	}
}

func TestPolicy_AllowSessionPersists(t *testing.T) {
	ap := &fakeApprover{decision: DecisionAllowSession}
	p := NewPolicy(ap, false)

	for i := 0; i < 3; i++ {
		if err := p.Check(context.Background(), "bash", RiskArbitrary, nil); err != nil {
			t.Fatalf("Check #%d = %v, want nil", i, err)
		}
	}
	if ap.calls != 1 {
		t.Errorf("approver called %d times, want 1 (allow-session persists)", ap.calls)
	}
}

func TestPolicy_DenyWithFeedback(t *testing.T) {
	ap := &fakeApprover{decision: DecisionDeny, feedback: "not right now"}
	p := NewPolicy(ap, false)

	err := p.Check(context.Background(), "bash", RiskArbitrary, nil)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("Check() = %v, want wrapping ErrDenied", err)
	}
	if err.Error() == ErrDenied.Error() {
		t.Errorf("Check() error lost feedback: %v", err)
	}
}

func TestPolicy_ApproverError(t *testing.T) {
	wantErr := errors.New("stdin closed")
	ap := &fakeApprover{err: wantErr}
	p := NewPolicy(ap, false)

	err := p.Check(context.Background(), "bash", RiskArbitrary, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Check() = %v, want wrapping %v", err, wantErr)
	}
}

func TestPolicy_ScopedPerTool(t *testing.T) {
	ap := &fakeApprover{decision: DecisionAllowSession}
	p := NewPolicy(ap, false)

	if err := p.Check(context.Background(), "bash", RiskArbitrary, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Check(bash) = %v", err)
	}
	if ap.calls != 1 {
		t.Fatalf("calls after bash = %d, want 1", ap.calls)
	}
	// A different tool name still needs its own approval.
	if err := p.Check(context.Background(), "write", RiskMutating, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Check(write) = %v", err)
	}
	if ap.calls != 2 {
		t.Errorf("calls after write = %d, want 2 (session-allow is per-tool)", ap.calls)
	}
}
