package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/unhewn/hewn/internal/sandbox"
)

var bashSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"command": {"type": "string", "description": "shell command to run in the project root"}
	},
	"required": ["command"]
}`)

type bashParams struct {
	Command string `json:"command"`
}

// Bash runs a shell command in the project root, through sandbox.Command.
// It is arbitrary execution gated by approval, not a sandbox (HEWN.md §3) --
// only file tools are jailed.
type Bash struct {
	sb      *sandbox.Sandbox
	keepEnv []string
}

// NewBash builds a Bash tool rooted at sb. keepEnv names environment
// variables exempt from sandbox.FilterEnv's denylist, e.g. the API key of
// the provider actually in use.
func NewBash(sb *sandbox.Sandbox, keepEnv []string) *Bash {
	return &Bash{sb: sb, keepEnv: keepEnv}
}

// Name returns "bash".
func (t *Bash) Name() string { return "bash" }

// Description describes the tool for the model.
func (t *Bash) Description() string {
	return "Run a shell command in the project root. Output is streamed as it runs."
}

// Schema returns the tool's JSON Schema parameters.
func (t *Bash) Schema() json.RawMessage { return bashSchema }

// Risk reports that bash is arbitrary execution.
func (t *Bash) Risk() RiskLevel { return RiskArbitrary }

// Execute runs the requested command to completion, streaming combined
// stdout/stderr through out as it arrives.
func (t *Bash) Execute(ctx context.Context, params json.RawMessage, out IO) (Result, error) {
	var p bashParams
	if err := json.Unmarshal(params, &p); err != nil {
		return Result{Output: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}
	if p.Command == "" {
		return Result{Output: "command is required", IsError: true}, nil
	}

	cmd := t.sb.Command(ctx, p.Command, t.keepEnv)

	sw := &streamWriter{onData: out.Output}
	cmd.Stdout = sw
	cmd.Stderr = sw

	runErr := cmd.Run()
	output := sw.String()

	if runErr != nil {
		if ctx.Err() != nil {
			// Cancellation, not a tool-reportable failure: surfaced via
			// Result.IsError rather than the error return (nilerr false
			// positive -- see Result's doc comment).
			return Result{Output: output, IsError: true}, nil //nolint:nilerr
		}
		if output != "" {
			output += "\n"
		}
		return Result{Output: output + runErr.Error(), IsError: true}, nil //nolint:nilerr
	}
	return Result{Output: output}, nil
}

// streamWriter fans written bytes out to a callback while also buffering
// them, so a running command's output can be both streamed live and
// returned in full once it exits.
type streamWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	onData func(string)
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	_, _ = w.buf.Write(p) // bytes.Buffer.Write never errors
	w.mu.Unlock()

	if w.onData != nil {
		w.onData(string(p))
	}
	return len(p), nil
}

func (w *streamWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
