package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/unhewn/hewn/internal/agent"
	"github.com/unhewn/hewn/internal/provider"
	_ "github.com/unhewn/hewn/internal/provider/anthropic" // registers itself with provider.Register
	"github.com/unhewn/hewn/internal/sandbox"
	"github.com/unhewn/hewn/internal/tool"
	"github.com/unhewn/hewn/internal/tui"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:           "hewn",
		Short:         "hewn - A minimalist Go agent harness",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			prompt, _ := cmd.Flags().GetString("prompt")
			if prompt == "" {
				tui.Start()
				return nil
			}
			return runHeadless(cmd, prompt)
		},
	}

	rootCmd.Flags().StringP("prompt", "p", "", "run prompt headless and exit")
	rootCmd.Flags().String("provider", "anthropic", "provider to use")
	rootCmd.Flags().String("model", "claude-opus-4-8", "model to use")
	rootCmd.Flags().String("cwd", "", "project directory (default: current directory)")
	rootCmd.Flags().Bool("no-tools", false, "disable tool use")
	rootCmd.Flags().Bool("yolo", false, "pre-approve every tool call for this run")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runHeadless drives one prompt through the agent loop with a plain-stdout
// renderer: HEWN.md §5's "same agent loop, only the event sink differs".
func runHeadless(cmd *cobra.Command, prompt string) error {
	providerName, _ := cmd.Flags().GetString("provider")
	model, _ := cmd.Flags().GetString("model")
	cwd, _ := cmd.Flags().GetString("cwd")
	noTools, _ := cmd.Flags().GetBool("no-tools")
	yolo, _ := cmd.Flags().GetBool("yolo")

	if cwd == "" {
		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("hewn: resolve cwd: %w", err)
		}
		cwd = dir
	}

	p, err := provider.New(providerName)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}

	sb, err := sandbox.New(cwd)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}
	defer sb.Close()

	registry := tool.NewRegistry()
	if !noTools {
		registry.Register(tool.NewRead(sb))
		registry.Register(tool.NewBash(sb, []string{"ANTHROPIC_API_KEY"}))
	}

	renderer := agent.NewHeadlessRenderer(os.Stdout, os.Stdin)
	loop := &agent.Loop{
		Provider: p,
		Tools:    registry,
		Approval: tool.NewPolicy(renderer, yolo),
		Model:    model,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return renderer.Render(loop.Run(ctx, prompt))
}
