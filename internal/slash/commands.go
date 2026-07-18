package slash

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Register adds every built-in command to reg.
func Register(reg *Registry) {
	reg.Register(helpCommand())
	reg.Register(modelCommand())
	reg.Register(newCommand())
	reg.Register(clearCommand())
	reg.Register(compactCommand())
	reg.Register(quitCommand())
	reg.Register(toolsCommand())
	reg.Register(costCommand())
	reg.Register(exportCommand())
}

func helpCommand() Command {
	return Command{
		Name:        "help",
		Description: "list available commands",
		Run: func(_ context.Context, c *Context, _ string) Result {
			var b strings.Builder
			for _, cmd := range c.Registry.List() {
				fmt.Fprintf(&b, "/%-8s %s\n", cmd.Name, cmd.Description)
			}
			return Result{Output: strings.TrimRight(b.String(), "\n")}
		},
	}
}

func modelCommand() Command {
	return Command{
		Name:        "model",
		Description: "show or set the model for subsequent turns",
		Run: func(_ context.Context, c *Context, args string) Result {
			if args == "" {
				return Result{Output: fmt.Sprintf("current model: %s", c.Loop.Model)}
			}
			c.Loop.Model = args
			return Result{Output: fmt.Sprintf("model set to %s", args)}
		},
	}
}

func newCommand() Command {
	return Command{
		Name:        "new",
		Description: "start a new session, clearing context",
		Run: func(ctx context.Context, c *Context, _ string) Result {
			if c.Store == nil {
				return Result{Output: "no session store configured"}
			}
			sess, err := c.Store.CreateSession(ctx, c.CWD, c.ProviderName, c.Loop.Model, "")
			if err != nil {
				return Result{Output: fmt.Sprintf("failed to start new session: %v", err)}
			}
			c.Loop.SessionID = sess.ID
			c.Loop.SeedHistory(nil)
			return Result{Output: fmt.Sprintf("started new session %s", sess.ID)}
		},
	}
}

func clearCommand() Command {
	return Command{
		Name:        "clear",
		Description: "clear context, keeping the same session",
		Run: func(_ context.Context, c *Context, _ string) Result {
			c.Loop.SeedHistory(nil)
			return Result{Output: "context cleared"}
		},
	}
}

func compactCommand() Command {
	return Command{
		Name:        "compact",
		Description: "summarize older context to save tokens (not implemented yet)",
		Run: func(_ context.Context, _ *Context, _ string) Result {
			return Result{Output: "compaction isn't implemented yet -- see HEWN.md's v0.2 auto-compaction plan"}
		},
	}
}

func quitCommand() Command {
	return Command{
		Name:        "quit",
		Description: "exit",
		Run: func(_ context.Context, _ *Context, _ string) Result {
			return Result{Output: "bye", Quit: true}
		},
	}
}

func toolsCommand() Command {
	return Command{
		Name:        "tools",
		Description: "list available tools",
		Run: func(_ context.Context, c *Context, _ string) Result {
			if c.Tools == nil {
				return Result{Output: "no tools registered"}
			}
			tools := c.Tools.List()
			if len(tools) == 0 {
				return Result{Output: "no tools registered"}
			}
			var b strings.Builder
			for _, t := range tools {
				fmt.Fprintf(&b, "%-8s [%s] %s\n", t.Name(), t.Risk(), t.Description())
			}
			return Result{Output: strings.TrimRight(b.String(), "\n")}
		},
	}
}

func costCommand() Command {
	return Command{
		Name:        "cost",
		Description: "show cumulative token usage for this session",
		Run: func(_ context.Context, c *Context, _ string) Result {
			u := c.Loop.TotalUsage()
			return Result{Output: fmt.Sprintf(
				"input: %d, output: %d, cache read: %d, cache write: %d",
				u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens,
			)}
		},
	}
}

func exportCommand() Command {
	return Command{
		Name:        "export",
		Description: "export this session's transcript to a JSON file",
		Run: func(ctx context.Context, c *Context, args string) Result {
			if c.Store == nil {
				return Result{Output: "no session store configured"}
			}

			messages, err := c.Store.LoadMessages(ctx, c.Loop.SessionID)
			if err != nil {
				return Result{Output: fmt.Sprintf("export failed: %v", err)}
			}

			data, err := json.MarshalIndent(messages, "", "  ")
			if err != nil {
				return Result{Output: fmt.Sprintf("export failed: %v", err)}
			}

			path := strings.TrimSpace(args)
			if path == "" {
				path = fmt.Sprintf("hewn-export-%s.json", c.Loop.SessionID)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return Result{Output: fmt.Sprintf("export failed: %v", err)}
			}
			return Result{Output: fmt.Sprintf("exported %d messages to %s", len(messages), path)}
		},
	}
}
