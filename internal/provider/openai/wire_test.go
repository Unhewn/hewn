package openai

import (
	"encoding/json"
	"testing"

	"github.com/unhewn/hewn/internal/provider"
)

func TestToWireRequest_SystemPromptBecomesAMessage(t *testing.T) {
	req := provider.Request{Model: "qwen2.5", System: []provider.ContentBlock{{Kind: provider.ContentText, Text: "be terse"}}}
	wr := toWireRequest(req)

	if len(wr.Messages) != 1 || wr.Messages[0].Role != "system" || wr.Messages[0].Content != "be terse" {
		t.Fatalf("Messages = %+v, want a single system message", wr.Messages)
	}
	if !wr.Stream {
		t.Error("Stream = false, want true")
	}
	if wr.StreamOptions == nil || !wr.StreamOptions.IncludeUsage {
		t.Error("StreamOptions.IncludeUsage not set")
	}
}

func TestToWireRequest_NoSystemPromptOmitsMessage(t *testing.T) {
	req := provider.Request{Model: "qwen2.5"}
	wr := toWireRequest(req)
	if len(wr.Messages) != 0 {
		t.Errorf("Messages = %+v, want none", wr.Messages)
	}
}

func TestToWireMessages_AssistantGathersTextAndToolCalls(t *testing.T) {
	msg := provider.Message{
		Role: provider.RoleAssistant,
		Content: []provider.ContentBlock{
			{Kind: provider.ContentText, Text: "let me check"},
			{Kind: provider.ContentToolUse, ToolUseID: "call_1", ToolName: "read", ToolInput: json.RawMessage(`{"path":"x"}`)},
		},
	}

	out := toWireMessages(msg)
	if len(out) != 1 {
		t.Fatalf("got %d messages, want 1 (assistant messages don't expand)", len(out))
	}
	wm := out[0]
	if wm.Role != "assistant" || wm.Content != "let me check" {
		t.Errorf("wm = %+v, unexpected role/content", wm)
	}
	if len(wm.ToolCalls) != 1 || wm.ToolCalls[0].ID != "call_1" || wm.ToolCalls[0].Type != "function" {
		t.Fatalf("ToolCalls = %+v", wm.ToolCalls)
	}
	if wm.ToolCalls[0].Function.Name != "read" || wm.ToolCalls[0].Function.Arguments != `{"path":"x"}` {
		t.Errorf("Function = %+v", wm.ToolCalls[0].Function)
	}
}

func TestToWireMessages_UserTextStaysOneMessage(t *testing.T) {
	msg := provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: "hello"}},
	}
	out := toWireMessages(msg)
	if len(out) != 1 || out[0].Role != "user" || out[0].Content != "hello" {
		t.Fatalf("got %+v, want one user message", out)
	}
}

func TestToWireMessages_ToolResultsExpandIntoSeparateMessages(t *testing.T) {
	// This is the key structural difference from Anthropic: a single
	// neutral message bundling multiple tool results (as
	// internal/agent.executeToolCalls produces) must expand into one
	// {"role":"tool",...} message per result.
	msg := provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{
			{Kind: provider.ContentToolResult, ToolResultID: "call_1", ToolResultContent: "result one"},
			{Kind: provider.ContentToolResult, ToolResultID: "call_2", ToolResultContent: "result two", ToolResultIsError: true},
		},
	}

	out := toWireMessages(msg)
	if len(out) != 2 {
		t.Fatalf("got %d messages, want 2 (one per tool result)", len(out))
	}
	if out[0].Role != "tool" || out[0].ToolCallID != "call_1" || out[0].Content != "result one" {
		t.Errorf("out[0] = %+v", out[0])
	}
	if out[1].Role != "tool" || out[1].ToolCallID != "call_2" || out[1].Content != "result two" {
		t.Errorf("out[1] = %+v", out[1])
	}
}

func TestToWireMessages_TextThenToolResultPreservesOrder(t *testing.T) {
	msg := provider.Message{
		Role: provider.RoleUser,
		Content: []provider.ContentBlock{
			{Kind: provider.ContentText, Text: "before"},
			{Kind: provider.ContentToolResult, ToolResultID: "call_1", ToolResultContent: "result"},
		},
	}
	out := toWireMessages(msg)
	if len(out) != 2 {
		t.Fatalf("got %d messages, want 2", len(out))
	}
	if out[0].Role != "user" || out[0].Content != "before" {
		t.Errorf("out[0] = %+v, want the text message first", out[0])
	}
	if out[1].Role != "tool" || out[1].ToolCallID != "call_1" {
		t.Errorf("out[1] = %+v, want the tool result second", out[1])
	}
}

func TestToWireRequest_ToolsTranslated(t *testing.T) {
	req := provider.Request{
		Model: "qwen2.5",
		Tools: []provider.ToolDef{
			{Name: "read", Description: "reads a file", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}
	wr := toWireRequest(req)
	if len(wr.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(wr.Tools))
	}
	tool := wr.Tools[0]
	if tool.Type != "function" || tool.Function.Name != "read" || tool.Function.Description != "reads a file" {
		t.Errorf("tool = %+v", tool)
	}
	if string(tool.Function.Parameters) != `{"type":"object"}` {
		t.Errorf("Parameters = %s", tool.Function.Parameters)
	}

	// Round-trip through JSON to catch struct tag mistakes.
	if _, err := json.Marshal(wr); err != nil {
		t.Fatalf("marshal wireRequest: %v", err)
	}
}
