package agent

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/unhewn/hewn/internal/provider"
	"github.com/unhewn/hewn/internal/tool"
)

// fakeStream replays a fixed slice of provider.Event values, then blocks on
// ctx.Done() if block is set (to simulate a long-running turn for
// cancellation tests), otherwise returns io.EOF.
type fakeStream struct {
	ctx    context.Context
	events []provider.Event
	i      int
	block  bool
}

func (s *fakeStream) Next() (provider.Event, error) {
	if s.i < len(s.events) {
		ev := s.events[s.i]
		s.i++
		return ev, nil
	}
	if s.block {
		<-s.ctx.Done()
		return provider.Event{}, s.ctx.Err()
	}
	return provider.Event{}, io.EOF
}

func (s *fakeStream) Close() error { return nil }

// fakeProvider returns one scripted turn (slice of events) per call to
// Stream, in order.
type fakeProvider struct {
	mu          sync.Mutex
	turns       [][]provider.Event
	call        int
	block       bool // if true, every Stream call blocks until ctx.Done()
	lastRequest provider.Request
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Models(context.Context) ([]provider.ModelInfo, error) { return nil, nil }

func (p *fakeProvider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	p.mu.Lock()
	p.lastRequest = req
	p.mu.Unlock()

	if p.block {
		return &fakeStream{ctx: ctx, block: true}, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.turns[p.call]
	p.call++
	return &fakeStream{events: events}, nil
}

// fakeTool always returns a fixed result and records how many times it ran.
type fakeTool struct {
	mu     sync.Mutex
	name   string
	risk   tool.RiskLevel
	result tool.Result
	calls  int
}

func (f *fakeTool) Name() string            { return f.name }
func (f *fakeTool) Description() string     { return "fake tool" }
func (f *fakeTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (f *fakeTool) Risk() tool.RiskLevel    { return f.risk }

func (f *fakeTool) Execute(_ context.Context, _ json.RawMessage, io tool.IO) (tool.Result, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	io.Output("partial")
	return f.result, nil
}

func (f *fakeTool) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fixedApprover always returns the same decision.
type fixedApprover struct {
	decision tool.Decision
	feedback string
}

func (a fixedApprover) RequestApproval(context.Context, tool.ApprovalRequest) (tool.Decision, string, error) {
	return a.decision, a.feedback, nil
}

func drainEvents(ch <-chan Event) []Event {
	var out []Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func kinds(events []Event) []EventKind {
	out := make([]EventKind, len(events))
	for i, ev := range events {
		out[i] = ev.Kind
	}
	return out
}

func TestLoop_SimpleText(t *testing.T) {
	p := &fakeProvider{turns: [][]provider.Event{
		{
			{Kind: provider.KindTextDelta, TextDelta: "Hi"},
			{Kind: provider.KindTextDelta, TextDelta: " there"},
			{Kind: provider.KindUsage, Usage: provider.Usage{InputTokens: 5, OutputTokens: 2}},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		},
	}}

	l := &Loop{
		Provider: p,
		Tools:    tool.NewRegistry(),
		Approval: tool.NewPolicy(fixedApprover{decision: tool.DecisionDeny}, false),
		Model:    "test-model",
	}

	events := drainEvents(l.Run(context.Background(), "hello"))

	want := []EventKind{KindTextDelta, KindTextDelta, KindUsage, KindStopReason}
	if got := kinds(events); !equalKinds(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}

	if len(l.history) != 2 {
		t.Fatalf("history length = %d, want 2 (user + assistant)", len(l.history))
	}
	if l.history[1].Role != provider.RoleAssistant || l.history[1].Content[0].Text != "Hi there" {
		t.Errorf("assistant message = %+v, want text %q", l.history[1], "Hi there")
	}
}

func TestLoop_ToolCallRoundTrip(t *testing.T) {
	p := &fakeProvider{turns: [][]provider.Event{
		{
			{Kind: provider.KindToolCallStart, ToolCallStart: provider.ToolCallStart{ID: "t1", Name: "read"}},
			{Kind: provider.KindToolCallDelta, ToolCallDelta: provider.ToolCallDelta{ID: "t1", InputDelta: `{"path":`}},
			{Kind: provider.KindToolCallDelta, ToolCallDelta: provider.ToolCallDelta{ID: "t1", InputDelta: `"x"}`}},
			{Kind: provider.KindToolCallEnd, ToolCallEnd: provider.ToolCallEnd{ID: "t1", Name: "read", Input: json.RawMessage(`{"path":"x"}`)}},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.KindTextDelta, TextDelta: "done"},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		},
	}}

	ft := &fakeTool{name: "read", risk: tool.RiskMutating, result: tool.Result{Output: "file contents"}}
	registry := tool.NewRegistry()
	registry.Register(ft)

	l := &Loop{
		Provider: p,
		Tools:    registry,
		Approval: tool.NewPolicy(fixedApprover{decision: tool.DecisionAllowOnce}, false),
		Model:    "test-model",
	}

	events := drainEvents(l.Run(context.Background(), "read x"))

	want := []EventKind{
		KindToolCallStart, KindToolCallDelta, KindToolCallDelta, KindToolCallEnd, KindStopReason,
		KindToolOutput, KindToolCallResult,
		KindTextDelta, KindStopReason,
	}
	if got := kinds(events); !equalKinds(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}

	if ft.callCount() != 1 {
		t.Errorf("tool called %d times, want 1", ft.callCount())
	}

	if len(l.history) != 4 {
		t.Fatalf("history length = %d, want 4 (user, assistant tool-use, user tool-result, assistant text)", len(l.history))
	}
	toolResultMsg := l.history[2]
	if toolResultMsg.Role != provider.RoleUser || toolResultMsg.Content[0].Kind != provider.ContentToolResult {
		t.Errorf("history[2] = %+v, want a user message with a tool_result block", toolResultMsg)
	}
	if toolResultMsg.Content[0].ToolResultContent != "file contents" {
		t.Errorf("tool result content = %q, want %q", toolResultMsg.Content[0].ToolResultContent, "file contents")
	}
}

func TestLoop_ApprovalDenied(t *testing.T) {
	p := &fakeProvider{turns: [][]provider.Event{
		{
			{Kind: provider.KindToolCallStart, ToolCallStart: provider.ToolCallStart{ID: "t1", Name: "bash"}},
			{Kind: provider.KindToolCallEnd, ToolCallEnd: provider.ToolCallEnd{ID: "t1", Name: "bash", Input: json.RawMessage(`{}`)}},
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.KindStopReason, StopReason: provider.StopReasonEndTurn},
		},
	}}

	ft := &fakeTool{name: "bash", risk: tool.RiskArbitrary, result: tool.Result{Output: "should not run"}}
	registry := tool.NewRegistry()
	registry.Register(ft)

	l := &Loop{
		Provider: p,
		Tools:    registry,
		Approval: tool.NewPolicy(fixedApprover{decision: tool.DecisionDeny, feedback: "not now"}, false),
		Model:    "test-model",
	}

	_ = drainEvents(l.Run(context.Background(), "rm stuff"))

	if ft.callCount() != 0 {
		t.Errorf("tool called %d times, want 0 (denied)", ft.callCount())
	}

	toolResultMsg := l.history[2]
	block := toolResultMsg.Content[0]
	if !block.ToolResultIsError {
		t.Error("denied call's tool_result IsError = false, want true")
	}
	if block.ToolResultContent == "" {
		t.Error("denied call's tool_result content is empty, want denial feedback")
	}
}

func TestLoop_Cancellation(t *testing.T) {
	p := &fakeProvider{block: true}

	l := &Loop{
		Provider: p,
		Tools:    tool.NewRegistry(),
		Approval: tool.NewPolicy(fixedApprover{decision: tool.DecisionDeny}, false),
		Model:    "test-model",
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := l.Run(ctx, "hello")

	done := make(chan struct{})
	var events []Event
	go func() {
		events = drainEvents(ch)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	for _, ev := range events {
		if ev.Kind == KindError {
			t.Errorf("got KindError event %+v after cancellation; context.Canceled must not be reported as a failure", ev)
		}
	}
}

func equalKinds(a, b []EventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
