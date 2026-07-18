package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/unhewn/hewn/internal/agent"
	"github.com/unhewn/hewn/internal/provider"
	_ "github.com/unhewn/hewn/internal/provider/anthropic" // registers itself with provider.Register
	"github.com/unhewn/hewn/internal/sandbox"
	"github.com/unhewn/hewn/internal/session"
	"github.com/unhewn/hewn/internal/tool"
	"github.com/unhewn/hewn/internal/tui"

	"github.com/spf13/cobra"
)

// resumeLatest is --resume's NoOptDefVal: cobra substitutes this when the
// flag is given with no argument ("hewn --resume"). It can never collide
// with a real session ID/prefix, since ULIDs are uppercase Crockford
// base32 and never contain lowercase letters.
const resumeLatest = "latest"

func main() {
	rootCmd := &cobra.Command{
		Use:           "hewn",
		Short:         "hewn - A minimalist Go agent harness",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, _ := cmd.Flags().GetBool("list")
			if list {
				return runList(cmd)
			}

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
	rootCmd.Flags().String("db", "", "session database path (default: ~/.local/share/hewn/hewn.db)")
	rootCmd.Flags().Bool("list", false, "list recent sessions and exit")
	rootCmd.Flags().String("resume", "", "resume a session: bare flag resumes the most recent, or --resume=<id-or-prefix> for a specific one (the = is required)")
	rootCmd.Flags().Lookup("resume").NoOptDefVal = resumeLatest

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// dbPath resolves --db, defaulting to HEWN.md §2 item 8's literal path.
func dbPath(cmd *cobra.Command) (string, error) {
	if v, _ := cmd.Flags().GetString("db"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("hewn: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "hewn", "hewn.db"), nil
}

// runList prints recent sessions and exits; it never touches a provider or
// the agent loop.
func runList(cmd *cobra.Command) error {
	path, err := dbPath(cmd)
	if err != nil {
		return err
	}

	ctx := context.Background()
	store, err := session.Open(ctx, path)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}
	defer store.Close()

	sessions, err := store.ListSessions(ctx, 50)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stdout, "no sessions yet")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUPDATED\tMODEL\tCWD\tTITLE")
	for _, s := range sessions {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.UpdatedAt.Format(time.RFC3339), s.Model, s.CWD, s.Title)
	}
	return w.Flush()
}

// resumed holds what --resume loaded from a prior session, overriding the
// corresponding CLI flags entirely (mixing an old session with a different
// provider/model/cwd is a correctness hazard, not a feature).
type resumed struct {
	sessionID string
	provider  string
	model     string
	cwd       string
	history   []provider.Message
}

func loadResumeTarget(ctx context.Context, store *session.Store, idOrPrefix string) (resumed, error) {
	if idOrPrefix == resumeLatest {
		idOrPrefix = ""
	}

	sess, err := store.LoadSession(ctx, idOrPrefix)
	if err != nil {
		return resumed{}, fmt.Errorf("hewn: %w", err)
	}

	messages, err := store.LoadMessages(ctx, sess.ID)
	if err != nil {
		return resumed{}, fmt.Errorf("hewn: %w", err)
	}
	history, err := agent.HistoryFromMessages(messages)
	if err != nil {
		return resumed{}, fmt.Errorf("hewn: %w", err)
	}

	return resumed{
		sessionID: sess.ID,
		provider:  sess.Provider,
		model:     sess.Model,
		cwd:       sess.CWD,
		history:   history,
	}, nil
}

// runHeadless drives one prompt through the agent loop with a plain-stdout
// renderer: HEWN.md §5's "same agent loop, only the event sink differs".
// Every run is durably recorded (HEWN.md §2 item 8) -- there is no flag to
// turn persistence off.
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

	dbFile, err := dbPath(cmd)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store, err := session.Open(ctx, dbFile)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}
	defer store.Close()

	var (
		sessionID string
		history   []provider.Message
	)

	if cmd.Flags().Changed("resume") {
		resumeArg, _ := cmd.Flags().GetString("resume")
		r, resumeErr := loadResumeTarget(ctx, store, resumeArg)
		if resumeErr != nil {
			return resumeErr
		}
		sessionID, providerName, model, cwd, history = r.sessionID, r.provider, r.model, r.cwd, r.history
	}

	// Construct the provider (and everything else rooted at cwd) only
	// after resolving provider/model/cwd above, and before creating a new
	// session, so a bad --provider or missing API key never leaves an
	// empty session row behind.
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
		registry.Register(tool.NewWrite(sb))
		registry.Register(tool.NewEdit(sb))
		registry.Register(tool.NewBash(sb, []string{"ANTHROPIC_API_KEY"}))
	}

	renderer := agent.NewHeadlessRenderer(os.Stdout, os.Stdin)
	loop := &agent.Loop{
		Provider: p,
		Tools:    registry,
		Approval: tool.NewPolicy(renderer, yolo),
		Model:    model,
		Session:  store,
	}
	if history != nil {
		loop.SeedHistory(history)
	}

	if sessionID == "" {
		sess, err := store.CreateSession(ctx, cwd, providerName, model, prompt)
		if err != nil {
			return fmt.Errorf("hewn: %w", err)
		}
		sessionID = sess.ID
	}
	loop.SessionID = sessionID

	return renderer.Render(loop.Run(ctx, prompt))
}
