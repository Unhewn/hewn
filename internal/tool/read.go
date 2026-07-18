package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/unhewn/hewn/internal/sandbox"
)

var readSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "file path, relative to the project root"}
	},
	"required": ["path"]
}`)

type readParams struct {
	Path string `json:"path"`
}

// Read is the read-only file-reading tool. All access is routed through
// sandbox.Sandbox (AGENTS.md invariant #4).
type Read struct {
	sb *sandbox.Sandbox
}

// NewRead builds a Read tool rooted at sb.
func NewRead(sb *sandbox.Sandbox) *Read {
	return &Read{sb: sb}
}

// Name returns "read".
func (t *Read) Name() string { return "read" }

// Description describes the tool for the model.
func (t *Read) Description() string {
	return "Read a file's contents, given a path relative to the project root."
}

// Schema returns the tool's JSON Schema parameters.
func (t *Read) Schema() json.RawMessage { return readSchema }

// Risk reports that read is read-only and never needs approval.
func (t *Read) Risk() RiskLevel { return RiskReadOnly }

// Execute reads the requested file.
func (t *Read) Execute(_ context.Context, params json.RawMessage, _ IO) (Result, error) {
	var p readParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{Output: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}
	if p.Path == "" {
		return Result{Output: "path is required", IsError: true}, nil
	}

	data, err := t.sb.ReadFile(p.Path)
	if err != nil {
		// Expected, tool-reportable failure: surfaced via Result.IsError
		// rather than the error return (nilerr false positive -- see
		// Result's doc comment).
		return Result{Output: err.Error(), IsError: true}, nil //nolint:nilerr
	}
	return Result{Output: string(data)}, nil
}
