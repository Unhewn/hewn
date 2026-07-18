package tui

import (
	"context"

	"github.com/unhewn/hewn/internal/slash"
)

// newTestSlashRegistry builds a minimal registry for tests that only need
// something to filter/dispatch against, not the real nine built-ins.
func newTestSlashRegistry() *slash.Registry {
	reg := slash.NewRegistry()
	reg.Register(slash.Command{
		Name:        "help",
		Description: "list commands",
		Run: func(_ context.Context, _ *slash.Context, _ string) slash.Result {
			return slash.Result{Output: "help text"}
		},
	})
	reg.Register(slash.Command{
		Name:        "model",
		Description: "show or set the model",
		Run: func(_ context.Context, _ *slash.Context, args string) slash.Result {
			return slash.Result{Output: "model: " + args}
		},
	})
	return reg
}
