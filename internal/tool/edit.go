package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/unhewn/hewn/internal/sandbox"
)

var editSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "file path, relative to the project root"},
		"old_string": {"type": "string", "description": "exact text to replace; must match exactly once unless replace_all is set"},
		"new_string": {"type": "string", "description": "text to replace it with"},
		"replace_all": {"type": "boolean", "description": "replace every occurrence of old_string instead of requiring exactly one"}
	},
	"required": ["path", "old_string", "new_string"]
}`)

type editParams struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

// Edit replaces an exact substring of an existing file with new text. All
// access is routed through sandbox.Sandbox (AGENTS.md invariant #4).
type Edit struct {
	sb *sandbox.Sandbox
}

// NewEdit builds an Edit tool rooted at sb.
func NewEdit(sb *sandbox.Sandbox) *Edit {
	return &Edit{sb: sb}
}

// Name returns "edit".
func (t *Edit) Name() string { return "edit" }

// Description describes the tool for the model.
func (t *Edit) Description() string {
	return "Replace an exact block of text in an existing file with new text, given a path relative to the project root."
}

// Schema returns the tool's JSON Schema parameters.
func (t *Edit) Schema() json.RawMessage { return editSchema }

// Risk reports that edit mutates the project directory.
func (t *Edit) Risk() RiskLevel { return RiskMutating }

// Execute applies the requested replacement.
func (t *Edit) Execute(_ context.Context, params json.RawMessage, _ IO) (Result, error) {
	var p editParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{Output: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}
	if p.Path == "" {
		return Result{Output: "path is required", IsError: true}, nil
	}
	if p.OldString == "" {
		return Result{Output: "old_string is required", IsError: true}, nil
	}
	if p.OldString == p.NewString {
		return Result{Output: "old_string and new_string are identical; nothing to do", IsError: true}, nil
	}

	data, err := t.sb.ReadFile(p.Path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil //nolint:nilerr // expected failure, see Result's doc comment
	}
	content := string(data)

	count := strings.Count(content, p.OldString)
	switch {
	case count == 0:
		return Result{Output: fmt.Sprintf("old_string not found in %s", p.Path), IsError: true}, nil
	case count > 1 && !p.ReplaceAll:
		return Result{
			Output:  fmt.Sprintf("old_string appears %d times in %s; make it unique or set replace_all", count, p.Path),
			IsError: true,
		}, nil
	}

	replaceCount := 1
	if p.ReplaceAll {
		replaceCount = -1
	}
	newContent := strings.Replace(content, p.OldString, p.NewString, replaceCount)

	perm := os.FileMode(0o644)
	if info, statErr := t.sb.Stat(p.Path); statErr == nil {
		perm = info.Mode().Perm()
	}

	if err := t.sb.WriteFile(p.Path, []byte(newContent), perm); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil //nolint:nilerr // expected failure, see Result's doc comment
	}
	return Result{Output: fmt.Sprintf("replaced %d occurrence(s) in %s", count, p.Path)}, nil
}
