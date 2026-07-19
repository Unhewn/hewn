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
	reg.Register(modelsCommand())
	reg.Register(setupCommand())
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
		Description: "show or set the model for subsequent turns (e.g. /model claude-sonnet-4-20250514, /model gpt-4o, /model gemma4:12b)",
		Run: func(ctx context.Context, c *Context, args string) Result {
			if args != "" {
				c.Loop.Model = args
				return Result{Output: fmt.Sprintf("model set to %s", args)}
			}
			return listModelsResult(ctx, c, "model")
		},
	}
}

// listModelsResult builds the current-model-plus-available-models listing
// shared by /model (no args) and /models. It always sets Output (plain
// text, for a frontend that can only print it); when the provider actually
// returns models, it also sets Choices so a frontend that can render an
// interactive picker (the TUI) can offer arrow-key selection instead of
// making the user retype the exact model name. Selecting a choice
// dispatches "/<selectCommand> <id>".
func listModelsResult(ctx context.Context, c *Context, selectCommand string) Result {
	current := ""
	if c.Loop != nil {
		current = c.Loop.Model
	}
	header := fmt.Sprintf("current model: %s", current)

	if c.Loop == nil || c.Loop.Provider == nil {
		return Result{Output: header + "\nchange it with: /model <name>"}
	}

	models, err := c.Loop.Provider.Models(ctx)
	if err != nil {
		return Result{Output: fmt.Sprintf("%s\nerror listing available models: %v\nchange it with: /model <name>", header, err)}
	}
	if len(models) == 0 {
		return Result{Output: fmt.Sprintf("%s\nno models reported by the provider\nchange it with: /model <name>", header)}
	}

	ids := make([]string, len(models))
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nmodels available at %s:\n", header, c.ProviderName)
	for i, m := range models {
		ids[i] = m.ID
		b.WriteString("  " + m.ID + "\n")
	}
	b.WriteString("\nswitch with: /model <name>")

	return Result{
		Output:        strings.TrimRight(b.String(), "\n"),
		Choices:       ids,
		SelectCommand: selectCommand,
	}
}

func setupCommand() Command {
	return Command{
		Name:        "setup",
		Description: "reconfigure Hewn (provider, model, name)",
		Run: func(_ context.Context, _ *Context, _ string) Result {
			return Result{Output: "To reconfigure Hewn:\n  1. Exit this session (ctrl+c twice or /quit)\n  2. Run: hewn --setup\n  3. The wizard will walk you through picking a model, API key, and name."}
		},
	}
}

func modelsCommand() Command {
	return Command{
		Name:        "models",
		Description: "list models available from the current provider",
		Run: func(ctx context.Context, c *Context, _ string) Result {
			return listModelsResult(ctx, c, "model")
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
			return Result{Output: fmt.Sprintf("started new session %s", sess.ID), ClearTranscript: true}
		},
	}
}

func clearCommand() Command {
	return Command{
		Name:        "clear",
		Description: "clear context, keeping the same session",
		Run: func(_ context.Context, c *Context, _ string) Result {
			c.Loop.SeedHistory(nil)
			return Result{Output: "context cleared", ClearTranscript: true}
		},
	}
}

func compactCommand() Command {
	return Command{
		Name:        "compact",
		Description: "summarize older context to save tokens",
		Run: func(ctx context.Context, c *Context, _ string) Result {
			result, err := c.Loop.Compact(ctx, 0)
			if err != nil {
				return Result{Output: fmt.Sprintf("compact failed: %v", err)}
			}
			if result.MessagesBefore == result.MessagesAfter {
				return Result{Output: "nothing to compact -- history is already short enough"}
			}
			return Result{Output: fmt.Sprintf(
				"compacted %d messages (~%d tokens) into a summary -- history is now %d messages",
				result.MessagesBefore-result.MessagesAfter+1, result.TokensBefore, result.MessagesAfter,
			)}
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
