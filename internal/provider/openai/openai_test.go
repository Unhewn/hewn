package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unhewn/hewn/internal/provider"
)

func TestCountTokens_Approximates(t *testing.T) {
	c := &Client{}
	text := strings.Repeat("a", 400) // 400 chars / 4 chars-per-token = 100 tokens

	req := provider.Request{
		Model: "gemma3:12b",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: text}}},
		},
	}

	got, err := c.CountTokens(context.Background(), req)
	if err != nil {
		t.Fatalf("CountTokens: %v", err)
	}
	if want := 100; got != want {
		t.Errorf("CountTokens = %d, want %d", got, want)
	}
}

func TestCountTokens_CountsSystemToolsAndHistory(t *testing.T) {
	c := &Client{}

	empty, err := c.CountTokens(context.Background(), provider.Request{Model: "gemma3:12b"})
	if err != nil {
		t.Fatalf("CountTokens(empty): %v", err)
	}
	if empty != 0 {
		t.Fatalf("CountTokens(empty request) = %d, want 0", empty)
	}

	req := provider.Request{
		Model:  "gemma3:12b",
		System: []provider.ContentBlock{{Kind: provider.ContentText, Text: strings.Repeat("s", 40)}},
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.ContentBlock{{Kind: provider.ContentText, Text: strings.Repeat("u", 40)}}},
			{Role: provider.RoleAssistant, Content: []provider.ContentBlock{
				{Kind: provider.ContentToolUse, ToolUseText: strings.Repeat("t", 40)},
			}},
			{Role: provider.RoleUser, Content: []provider.ContentBlock{
				{Kind: provider.ContentToolResult, ToolResultContent: strings.Repeat("r", 40)},
			}},
		},
		Tools: []provider.ToolDef{
			{Name: "read", Description: strings.Repeat("d", 40), InputSchema: json.RawMessage(`{}`)},
		},
	}

	got, err := c.CountTokens(context.Background(), req)
	if err != nil {
		t.Fatalf("CountTokens: %v", err)
	}
	if got <= empty {
		t.Errorf("CountTokens(populated request) = %d, want more than the empty request's %d", got, empty)
	}
}
