package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/unhewn/hewn/internal/provider"
)

// wire* types mirror the Anthropic Messages API JSON shapes. They never
// leave this package (AGENTS.md invariant #7).

type wireContent struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type wireMessage struct {
	Role    string        `json:"role"`
	Content []wireContent `json:"content"`
}

type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type wireRequest struct {
	Model     string        `json:"model"`
	System    string        `json:"system,omitempty"`
	Messages  []wireMessage `json:"messages"`
	Tools     []wireTool    `json:"tools,omitempty"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
}

func toWireRequest(req provider.Request) (wireRequest, error) {
	wr := wireRequest{
		Model:     req.Model,
		System:    req.System,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}

	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role)}
		for _, c := range m.Content {
			wc, err := toWireContent(c)
			if err != nil {
				return wireRequest{}, err
			}
			wm.Content = append(wm.Content, wc)
		}
		wr.Messages = append(wr.Messages, wm)
	}

	for _, t := range req.Tools {
		wr.Tools = append(wr.Tools, wireTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	return wr, nil
}

func toWireContent(c provider.ContentBlock) (wireContent, error) {
	switch c.Kind {
	case provider.ContentText:
		return wireContent{Type: "text", Text: c.Text}, nil
	case provider.ContentToolUse:
		return wireContent{
			Type:  "tool_use",
			ID:    c.ToolUseID,
			Name:  c.ToolName,
			Input: c.ToolInput,
		}, nil
	case provider.ContentToolResult:
		return wireContent{
			Type:      "tool_result",
			ToolUseID: c.ToolResultID,
			Content:   c.ToolResultContent,
			IsError:   c.ToolResultIsError,
		}, nil
	default:
		return wireContent{}, fmt.Errorf("anthropic: unknown content block kind %v", c.Kind)
	}
}

type wireModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
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
