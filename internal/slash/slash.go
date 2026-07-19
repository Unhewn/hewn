// Package slash is the slash-command registry: dispatched by name rather
// than a switch statement (HEWN.md item 7). It lives at the top level
// rather than nested under internal/tui because it's usable from any
// front end -- today the interactive headless mode, eventually a real
// TUI too (AGENTS.md's tui -> agent -> provider/tool/session dependency
// direction means a future tui package would import this, not the other
// way around).
package slash

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/unhewn/hewn/internal/agent"
	"github.com/unhewn/hewn/internal/session"
	"github.com/unhewn/hewn/internal/tool"
)

// Result is what a Command reports back to its caller.
type Result struct {
	Output string
	Quit   bool

	// ClearTranscript is set by commands that reset the loop's history
	// (/new, /clear). A caller that keeps its own view-layer transcript
	// separate from Loop's internal history (the TUI, per AGENTS.md
	// invariant #1) has no other way to learn that history and needs to
	// clear its own display too.
	ClearTranscript bool

	// Choices, if non-empty, offers a list to pick from instead of just
	// printing Output -- e.g. /model with no args listing models. A
	// frontend that can render an interactive picker (the TUI) should
	// present Choices and, on selection, dispatch "/<SelectCommand>
	// <choice>" through the registry rather than showing Output. A
	// frontend that can't (headless, --interactive) just falls back to
	// Output, which already carries the same information as plain text.
	Choices       []string
	SelectCommand string
}

// Context is everything a Command needs to act.
type Context struct {
	Loop         *agent.Loop
	Store        *session.Store
	Tools        *tool.Registry
	Registry     *Registry
	Out          io.Writer
	CWD          string
	ProviderName string
}

// Command is one slash command.
type Command struct {
	Name        string
	Description string
	Run         func(ctx context.Context, c *Context, args string) Result
}

// Registry is the set of available commands, in registration order.
type Registry struct {
	commands map[string]Command
	order    []string
}

// NewRegistry builds an empty Registry.
func NewRegistry() *Registry {
	return &Registry{commands: map[string]Command{}}
}

// Register adds c, replacing any existing command of the same name in
// place.
func (r *Registry) Register(c Command) {
	if _, exists := r.commands[c.Name]; !exists {
		r.order = append(r.order, c.Name)
	}
	r.commands[c.Name] = c
}

// List returns every registered command, in registration order.
func (r *Registry) List() []Command {
	out := make([]Command, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.commands[name])
	}
	return out
}

// Dispatch parses line. If it doesn't start with "/", handled is false and
// the caller should treat line as a normal message to the model.
// Otherwise handled is true: either the named command ran, or -- for an
// unrecognized name -- Result carries an explanatory message, so an
// unrecognized "/xyz" is never mistakenly forwarded to the model as text.
func (r *Registry) Dispatch(ctx context.Context, c *Context, line string) (Result, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return Result{}, false
	}

	name, args, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	cmd, ok := r.commands[name]
	if !ok {
		return Result{Output: fmt.Sprintf("unknown command: /%s (try /help)", name)}, true
	}
	return cmd.Run(ctx, c, strings.TrimSpace(args)), true
}
