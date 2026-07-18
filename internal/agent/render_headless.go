package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/unhewn/hewn/internal/tool"
)

// HeadlessRenderer consumes a Loop's event channel and renders it as plain
// text, with tool approval as a blocking prompt read from stdin. This is
// HEWN.md §5's "same agent loop, only the event sink differs" -- the
// substrate for testing the loop without a terminal.
type HeadlessRenderer struct {
	out io.Writer
	in  *bufio.Reader
}

// NewHeadlessRenderer builds a renderer writing to out and reading approval
// answers from in.
func NewHeadlessRenderer(out io.Writer, in io.Reader) *HeadlessRenderer {
	return &HeadlessRenderer{out: out, in: bufio.NewReader(in)}
}

// Render drains events, printing each to the renderer's writer, until the
// channel closes. It returns the turn's error, if any (a KindError event);
// cmd/hewn uses this to decide the process exit code.
func (r *HeadlessRenderer) Render(events <-chan Event) error {
	var turnErr error

	for ev := range events {
		switch ev.Kind {
		case KindTextDelta:
			fmt.Fprintf(r.out, "%s", ev.TextDelta)
		case KindThinkingDelta:
			// v0.1: thinking is not surfaced headlessly (HEWN.md open
			// question #6 is undecided; suppress rather than guess).
		case KindToolCallStart:
			// No params yet -- those arrive with KindToolCallEnd.
		case KindToolCallDelta:
			// Raw partial JSON fragment, not for display.
		case KindToolCallEnd:
			fmt.Fprintf(r.out, "\n● %s %s\n", ev.ToolCallEnd.Name, string(ev.ToolCallEnd.Input))
		case KindToolOutput:
			fmt.Fprintf(r.out, "%s", ev.ToolOutput.Chunk)
		case KindToolCallResult:
			if ev.ToolCallResult.IsError {
				fmt.Fprintf(r.out, "  ✗ %s\n", ev.ToolCallResult.Output)
			}
		case KindUsage:
			// v0.1: no live token display headlessly.
		case KindStopReason:
			if ev.StopReason != StopReasonToolUse {
				fmt.Fprintln(r.out)
			}
		case KindError:
			turnErr = ev.Err
		}
	}

	return turnErr
}

// RequestApproval implements tool.Approver as a blocking terminal prompt,
// per HEWN.md item 5: a = allow once, A = allow this tool for the session,
// d = deny with feedback.
func (r *HeadlessRenderer) RequestApproval(_ context.Context, req tool.ApprovalRequest) (tool.Decision, string, error) {
	fmt.Fprintf(r.out, "\n%s wants to run %q with %s\n", "hewn", req.Tool, string(req.Params))

	for {
		fmt.Fprintf(r.out, "%s", "allow? [a] once / [A] always this session / [d] deny: ")

		line, err := r.in.ReadString('\n')
		if err != nil {
			return tool.DecisionDeny, "", fmt.Errorf("agent: read approval answer: %w", err)
		}

		switch strings.TrimSpace(line) {
		case "a":
			return tool.DecisionAllowOnce, "", nil
		case "A":
			return tool.DecisionAllowSession, "", nil
		case "d":
			fmt.Fprintf(r.out, "%s", "feedback (optional, press enter to skip): ")
			feedback, _ := r.in.ReadString('\n')
			return tool.DecisionDeny, strings.TrimSpace(feedback), nil
		default:
			fmt.Fprintln(r.out, "please answer a / A / d")
		}
	}
}
