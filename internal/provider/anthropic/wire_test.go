package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/unhewn/hewn/internal/provider"
)

func TestToWireRequest(t *testing.T) {
	req := provider.Request{
		Model:  "claude-opus-4-8",
		System: []provider.ContentBlock{{Kind: provider.ContentText, Text: "be terse", Cacheable: true}},
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

	if wr.Model != "claude-opus-4-8" || wr.MaxTokens != 1024 || !wr.Stream {
		t.Fatalf("unexpected top-level fields: %+v", wr)
	}
	if len(wr.System) != 1 || wr.System[0].Type != "text" || wr.System[0].Text != "be terse" {
		t.Fatalf("System = %+v, want one cacheable text block", wr.System)
	}
	if wr.System[0].CacheControl == nil || wr.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("System[0].CacheControl = %+v, want an ephemeral breakpoint", wr.System[0].CacheControl)
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

func TestToWireContent_CacheableSetsCacheControl(t *testing.T) {
	cacheable, err := toWireContent(provider.ContentBlock{Kind: provider.ContentText, Text: "hi", Cacheable: true})
	if err != nil {
		t.Fatalf("toWireContent: %v", err)
	}
	if cacheable.CacheControl == nil || cacheable.CacheControl.Type != "ephemeral" {
		t.Errorf("CacheControl = %+v, want an ephemeral breakpoint", cacheable.CacheControl)
	}

	plain, err := toWireContent(provider.ContentBlock{Kind: provider.ContentText, Text: "hi"})
	if err != nil {
		t.Fatalf("toWireContent: %v", err)
	}
	if plain.CacheControl != nil {
		t.Errorf("CacheControl = %+v, want nil for a non-cacheable block", plain.CacheControl)
	}
}
