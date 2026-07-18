// Package provider is the LLM abstraction: the Provider interface, shared
// neutral types, and a registry of constructors. Per-provider wire formats
// live under subpackages (e.g. internal/provider/anthropic) and never leak
// out through the types defined here (AGENTS.md invariant #7).
package provider

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNoAPIKey is returned by a provider constructor when the credential it
// needs is not configured.
var ErrNoAPIKey = errors.New("provider: no API key configured")

// Provider is a streaming LLM backend.
type Provider interface {
	// Name is the provider's registry name, e.g. "anthropic".
	Name() string

	// Models lists the models this provider currently offers.
	Models(ctx context.Context) ([]ModelInfo, error)

	// Stream starts a turn and returns a Stream of events for it.
	Stream(ctx context.Context, req Request) (Stream, error)
}

// Stream yields the events of one in-flight turn.
type Stream interface {
	// Next returns the next event, or io.EOF once the turn is complete.
	Next() (Event, error)

	// Close releases any resources held by the stream (e.g. the underlying
	// HTTP response body). Safe to call after io.EOF.
	Close() error
}

// ModelInfo describes a model a Provider can be asked to use.
type ModelInfo struct {
	ID          string
	DisplayName string
}

// Role is who a Message is attributed to.
type Role string

// Role values.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentBlockKind identifies which fields of a ContentBlock are populated.
type ContentBlockKind int

// ContentBlockKind values.
const (
	ContentText ContentBlockKind = iota
	ContentToolUse
	ContentToolResult
)

// ContentBlock is one neutral content block within a Message. Exactly the
// fields matching Kind are meaningful.
type ContentBlock struct {
	Kind ContentBlockKind

	// ContentText
	Text string

	// ContentToolUse
	ToolUseID   string
	ToolName    string
	ToolInput   json.RawMessage
	ToolUseText string // human-readable echo of the call, for history rendering; may be empty

	// ContentToolResult
	ToolResultID      string
	ToolResultContent string
	ToolResultIsError bool
}

// Message is one neutral turn of conversation history.
type Message struct {
	Role    Role
	Content []ContentBlock
}

// ToolDef describes a tool the model is allowed to call, translated from a
// tool.Tool at the internal/agent boundary.
type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Request is one neutral turn request.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}
