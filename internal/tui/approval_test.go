package tui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/unhewn/hewn/internal/tool"
)

func TestTUIApprover_RoundTrip(t *testing.T) {
	a := NewApprover()

	type result struct {
		decision tool.Decision
		feedback string
		err      error
	}
	done := make(chan result, 1)

	go func() {
		decision, feedback, err := a.RequestApproval(context.Background(), tool.ApprovalRequest{
			Tool:   "bash",
			Params: json.RawMessage(`{"command":"ls"}`),
		})
		done <- result{decision, feedback, err}
	}()

	var req approvalRequest
	select {
	case req = <-a.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the approval request to arrive")
	}

	if req.req.Tool != "bash" {
		t.Errorf("req.Tool = %q, want %q", req.req.Tool, "bash")
	}
	req.response <- approvalResponse{decision: tool.DecisionAllowSession, feedback: ""}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("RequestApproval: %v", r.err)
		}
		if r.decision != tool.DecisionAllowSession {
			t.Errorf("decision = %v, want DecisionAllowSession", r.decision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RequestApproval to return")
	}
}

func TestTUIApprover_DenyWithFeedback(t *testing.T) {
	a := NewApprover()

	type result struct {
		decision tool.Decision
		feedback string
		err      error
	}
	done := make(chan result, 1)

	go func() {
		decision, feedback, err := a.RequestApproval(context.Background(), tool.ApprovalRequest{Tool: "bash"})
		done <- result{decision, feedback, err}
	}()

	req := <-a.requests
	req.response <- approvalResponse{decision: tool.DecisionDeny, feedback: "not now"}

	r := <-done
	if r.err != nil {
		t.Fatalf("RequestApproval: %v", r.err)
	}
	if r.decision != tool.DecisionDeny || r.feedback != "not now" {
		t.Errorf("got (%v, %q), want (DecisionDeny, %q)", r.decision, r.feedback, "not now")
	}
}

func TestTUIApprover_ContextCancelledBeforeAnyoneReceives(t *testing.T) {
	a := NewApprover() // unbuffered requests channel; nothing ever reads it here

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := a.RequestApproval(ctx, tool.ApprovalRequest{Tool: "bash"})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RequestApproval() error = nil, want ctx.Err()")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestApproval did not return promptly on a pre-canceled context")
	}
}

func TestTUIApprover_ContextCancelledWhileAwaitingResponse(t *testing.T) {
	a := NewApprover()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, _, err := a.RequestApproval(ctx, tool.ApprovalRequest{Tool: "bash"})
		done <- err
	}()

	// Receive the request so the send side completes, but never answer it.
	<-a.requests
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RequestApproval() error = nil, want ctx.Err() after cancellation while awaiting a response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestApproval did not unblock after ctx was canceled while awaiting a response")
	}
}
