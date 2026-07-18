// Package tool defines the Tool interface, the built-in tools, and the
// approval policy that gates them.
package tool

import (
	"context"
	"encoding/json"
)

// RiskLevel classifies how much latitude a Tool needs. Any tool with a
// Risk() of Mutating or higher goes through the approval flow before it
// acts (AGENTS.md invariant #5).
type RiskLevel int

// RiskLevel values.
const (
	RiskReadOnly RiskLevel = iota
	RiskMutating
	RiskArbitrary
)

// String returns the constant's name, for logging and approval prompts.
func (r RiskLevel) String() string {
	switch r {
	case RiskReadOnly:
		return "read_only"
	case RiskMutating:
		return "mutating"
	case RiskArbitrary:
		return "arbitrary"
	default:
		return "unknown"
	}
}

// Result is what a Tool call returns to the model as its tool_result.
// IsError marks an expected, tool-reportable failure (bad params, file not
// found, non-zero exit) that the model should see and can react to -- it is
// not the same as the error return from Execute, which signals a plumbing
// failure that should abort the turn.
type Result struct {
	Output  string
	IsError bool
}

// IO gives a running Tool a way to stream partial output, so bash can
// render live and (later) edit can show a diff before applying.
type IO interface {
	// Output streams one chunk of partial output while the tool runs.
	Output(chunk string)
}

// Tool is one callable capability exposed to the model.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON Schema for params
	Risk() RiskLevel
	Execute(ctx context.Context, params json.RawMessage, io IO) (Result, error)
}
