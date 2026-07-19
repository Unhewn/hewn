package openai

import (
	"encoding/json"
	"strings"

	"github.com/unhewn/hewn/internal/provider"
)

// wire* types mirror the OpenAI-compatible Chat Completions API JSON
// shapes (the same wire format Ollama, llama.cpp's server, LM Studio, and
// many hosted backends implement). They never leave this package
// (AGENTS.md invariant #7).

type wireFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // a JSON object, encoded as a string
}

// wireToolCall is one entry of an outgoing assistant message's tool_calls.
type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function wireFunctionCall `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireTool struct {
	Type     string          `json:"type"` // "function"
	Function wireFunctionDef `json:"function"`
}

type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireRequest struct {
	Model         string             `json:"model"`
	Messages      []wireMessage      `json:"messages"`
	Tools         []wireTool         `json:"tools,omitempty"`
	MaxTokens     int                `json:"max_tokens,omitempty"`
	Stream        bool               `json:"stream"`
	StreamOptions *wireStreamOptions `json:"stream_options,omitempty"`
}

// toWireRequest translates a neutral Request into the wire shape.
// stream_options.include_usage is always set; a backend that doesn't
// support it (or doesn't populate usage regardless) just means no
// KindUsage event that turn, exactly like Anthropic's optional usage.
func toWireRequest(req provider.Request) wireRequest {
	wr := wireRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		StreamOptions: &wireStreamOptions{IncludeUsage: true},
	}

	if system := systemText(req.System); system != "" {
		wr.Messages = append(wr.Messages, wireMessage{Role: "system", Content: system})
	}
	for _, m := range req.Messages {
		wr.Messages = append(wr.Messages, toWireMessages(m)...)
	}
	for _, t := range req.Tools {
		wr.Tools = append(wr.Tools, wireTool{
			Type:     "function",
			Function: wireFunctionDef{Name: t.Name, Description: t.Description, Parameters: t.InputSchema},
		})
	}

	return wr
}

// systemText concatenates the text of every system content block into one
// plain string. This backend has no cache_control equivalent (OpenAI and
// most OpenAI-compatible backends cache automatically, with no explicit
// breakpoint needed), so a Cacheable marker on any block is simply ignored.
func systemText(blocks []provider.ContentBlock) string {
	var b strings.Builder
	for _, c := range blocks {
		if c.Kind == provider.ContentText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// toWireMessages translates one neutral Message. An assistant message
// stays as one wire message (text plus a gathered tool_calls array); a
// user message expands into one wire message per content block, since
// OpenAI represents each tool result as its own {"role":"tool",...}
// message rather than bundling them -- unlike Anthropic, where they stay
// bundled in one user message's content blocks.
func toWireMessages(m provider.Message) []wireMessage {
	switch m.Role {
	case provider.RoleAssistant:
		return []wireMessage{assistantWireMessage(m)}
	case provider.RoleUser:
		return userWireMessages(m)
	default:
		return nil
	}
}

func assistantWireMessage(m provider.Message) wireMessage {
	wm := wireMessage{Role: "assistant"}
	for _, c := range m.Content {
		switch c.Kind {
		case provider.ContentText:
			wm.Content += c.Text
		case provider.ContentToolUse:
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:       c.ToolUseID,
				Type:     "function",
				Function: wireFunctionCall{Name: c.ToolName, Arguments: string(c.ToolInput)},
			})
		case provider.ContentToolResult:
			// Not a valid shape for an assistant message; nothing to do.
		}
	}
	return wm
}

func userWireMessages(m provider.Message) []wireMessage {
	var out []wireMessage
	var text string

	flushText := func() {
		if text != "" {
			out = append(out, wireMessage{Role: "user", Content: text})
			text = ""
		}
	}

	for _, c := range m.Content {
		switch c.Kind {
		case provider.ContentText:
			text += c.Text
		case provider.ContentToolResult:
			flushText()
			out = append(out, wireMessage{Role: "tool", ToolCallID: c.ToolResultID, Content: c.ToolResultContent})
		case provider.ContentToolUse:
			// Not expected in a user-role message under this codebase's
			// usage; nothing meaningful to translate it to here.
		}
	}
	flushText()

	return out
}

type wireModel struct {
	ID string `json:"id"`
}

type wireModelsResponse struct {
	Data []wireModel `json:"data"`
}

type wireAPIError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
