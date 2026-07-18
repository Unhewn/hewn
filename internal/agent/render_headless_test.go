package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/unhewn/hewn/internal/tool"
)

func TestHeadlessRenderer_Render(t *testing.T) {
	events := make(chan Event, 10)
	events <- NewTextDelta("Hi")
	events <- NewTextDelta(" there")
	events <- NewToolCallEnd("t1", "read", json.RawMessage(`{"path":"x"}`))
	events <- NewToolOutput("t1", "line 1\n")
	events <- NewToolCallResult("t1", "boom", true)
	events <- NewStopReason(StopReasonEndTurn)
	close(events)

	var out bytes.Buffer
	r := NewHeadlessRenderer(&out, strings.NewReader(""))

	if err := r.Render(events); err != nil {
		t.Fatalf("Render: %v", err)
	}

	rendered := out.String()
	for _, want := range []string{"Hi there", "read", `"path":"x"`, "line 1", "boom"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered output missing %q:\n%s", want, rendered)
		}
	}
}

func TestHeadlessRenderer_Render_PropagatesError(t *testing.T) {
	events := make(chan Event, 1)
	wantErr := errors.New("stream broke")
	events <- NewError(wantErr)
	close(events)

	var out bytes.Buffer
	r := NewHeadlessRenderer(&out, strings.NewReader(""))

	err := r.Render(events)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Render() = %v, want %v", err, wantErr)
	}
}

func TestHeadlessRenderer_RequestApproval(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     tool.Decision
		feedback string
	}{
		{name: "allow once", input: "a\n", want: tool.DecisionAllowOnce},
		{name: "allow session", input: "A\n", want: tool.DecisionAllowSession},
		{name: "deny with feedback", input: "d\nnot now\n", want: tool.DecisionDeny, feedback: "not now"},
		{name: "reprompts on garbage", input: "garbage\na\n", want: tool.DecisionAllowOnce},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			r := NewHeadlessRenderer(&out, strings.NewReader(tt.input))

			decision, feedback, err := r.RequestApproval(context.Background(), tool.ApprovalRequest{
				Tool:   "bash",
				Params: json.RawMessage(`{"command":"ls"}`),
			})
			if err != nil {
				t.Fatalf("RequestApproval: %v", err)
			}
			if decision != tt.want {
				t.Errorf("decision = %v, want %v", decision, tt.want)
			}
			if feedback != tt.feedback {
				t.Errorf("feedback = %q, want %q", feedback, tt.feedback)
			}
		})
	}
}
