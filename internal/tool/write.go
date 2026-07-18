package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/unhewn/hewn/internal/sandbox"
)

var writeSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "file path, relative to the project root"},
		"content": {"type": "string", "description": "full contents to write"}
	},
	"required": ["path", "content"]
}`)

type writeParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Write creates or overwrites a file with the given content, creating
// parent directories as needed. All access is routed through
// sandbox.Sandbox (AGENTS.md invariant #4).
type Write struct {
	sb *sandbox.Sandbox
}

// NewWrite builds a Write tool rooted at sb.
func NewWrite(sb *sandbox.Sandbox) *Write {
	return &Write{sb: sb}
}

// Name returns "write".
func (t *Write) Name() string { return "write" }

// Description describes the tool for the model.
func (t *Write) Description() string {
	return "Create or overwrite a file with the given content, given a path relative to the project root."
}

// Schema returns the tool's JSON Schema parameters.
func (t *Write) Schema() json.RawMessage { return writeSchema }

// Risk reports that write mutates the project directory.
func (t *Write) Risk() RiskLevel { return RiskMutating }

// Execute writes the requested file.
func (t *Write) Execute(_ context.Context, params json.RawMessage, _ IO) (Result, error) {
	var p writeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{Output: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}
	if p.Path == "" {
		return Result{Output: "path is required", IsError: true}, nil
	}

	if err := t.sb.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		// Expected, tool-reportable failure: surfaced via Result.IsError
		// rather than the error return (nilerr false positive -- see
		// Result's doc comment).
		return Result{Output: err.Error(), IsError: true}, nil //nolint:nilerr
	}
	return Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path)}, nil
}
