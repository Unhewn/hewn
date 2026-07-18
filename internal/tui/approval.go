package tui

import (
	"context"

	"github.com/unhewn/hewn/internal/tool"
)

// approvalRequest is one pending call awaiting a decision, paired with the
// channel its answer must arrive on.
type approvalRequest struct {
	req      tool.ApprovalRequest
	response chan approvalResponse
}

// approvalResponse is the human's answer to one approvalRequest.
type approvalResponse struct {
	decision tool.Decision
	feedback string
}

// Approver implements tool.Approver by routing requests through a channel
// instead of blocking on stdin -- there is no stdin read available to it,
// since Bubble Tea owns the terminal in raw mode. RequestApproval runs on
// the agent loop's own goroutine (from inside Loop.executeOne) and can
// legitimately block there: the root Model reads pending requests via a
// tea.Cmd and eventually sends the human's decision back on the
// per-request response channel.
type Approver struct {
	requests chan approvalRequest
}

// NewApprover builds the tool.Approver the TUI passes into buildLoop, with
// an unbuffered request channel: a send only completes once something is
// actually waiting to receive it. The same value must also be passed to
// Start, so its Update loop can read the requests it's approving.
func NewApprover() *Approver {
	return &Approver{requests: make(chan approvalRequest)}
}

// RequestApproval implements tool.Approver. Both the request send and the
// response wait respect ctx cancellation, so a canceled turn (e.g. Ctrl+C
// while an approval is genuinely pending) can't hang the loop goroutine
// forever even if nothing ever answers.
func (a *Approver) RequestApproval(ctx context.Context, req tool.ApprovalRequest) (tool.Decision, string, error) {
	respCh := make(chan approvalResponse, 1)

	select {
	case a.requests <- approvalRequest{req: req, response: respCh}:
	case <-ctx.Done():
		return tool.DecisionDeny, "", ctx.Err()
	}

	select {
	case resp := <-respCh:
		return resp.decision, resp.feedback, nil
	case <-ctx.Done():
		return tool.DecisionDeny, "", ctx.Err()
	}
}
