package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/unhewn/hewn/internal/provider"
)

func TestToWireRequest(t *testing.T) {
	req := provider.Request{
		Model:  "claude-opus-4-8",
		System: "be terse",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.ContentBlock{
				{Kind: provider.ContentText, Text: "hi"},
			}},
			{Role: provider.RoleAssistant, Content: []provider.ContentBlock{
				{Kind: provider.ContentToolUse, ToolUseID: "toolu_1", ToolName: "read", ToolInput: json.RawMessage(`{"path":"x"}`)},
			}},
			{Role: provider.RoleUser, Content: []provider.ContentBlock{
				{Kind: provider.ContentToolResult, ToolResultID: "toolu_1", ToolResultContent: "contents", ToolResultIsError: false},
			}},
		},
		Tools: []provider.ToolDef{
			{Name: "read", Description: "reads a file", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
		MaxTokens: 1024,
	}

	wr, err := toWireRequest(req)
	if err != nil {
		t.Fatalf("toWireRequest: %v", err)
	}

	if wr.Model != "claude-opus-4-8" || wr.System != "be terse" || wr.MaxTokens != 1024 || !wr.Stream {
		t.Fatalf("unexpected top-level fields: %+v", wr)
	}
	if len(wr.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(wr.Messages))
	}

	if got := wr.Messages[0].Content[0]; got.Type != "text" || got.Text != "hi" {
		t.Errorf("message 0 content = %+v", got)
	}

	toolUse := wr.Messages[1].Content[0]
	if toolUse.Type != "tool_use" || toolUse.ID != "toolu_1" || toolUse.Name != "read" || string(toolUse.Input) != `{"path":"x"}` {
		t.Errorf("message 1 content = %+v", toolUse)
	}

	toolResult := wr.Messages[2].Content[0]
	if toolResult.Type != "tool_result" || toolResult.ToolUseID != "toolu_1" || toolResult.Content != "contents" {
		t.Errorf("message 2 content = %+v", toolResult)
	}

	if len(wr.Tools) != 1 || wr.Tools[0].Name != "read" {
		t.Errorf("tools = %+v", wr.Tools)
	}

	// Round-trip through JSON to catch struct tag mistakes.
	if _, err := json.Marshal(wr); err != nil {
		t.Fatalf("marshal wireRequest: %v", err)
	}
}
