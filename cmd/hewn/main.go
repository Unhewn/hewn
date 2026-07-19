package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/unhewn/hewn/internal/agent"
	"github.com/unhewn/hewn/internal/config"
	"github.com/unhewn/hewn/internal/ctxfile"
	"github.com/unhewn/hewn/internal/mcp"
	"github.com/unhewn/hewn/internal/provider"
	_ "github.com/unhewn/hewn/internal/provider/anthropic" // registers itself with provider.Register
	_ "github.com/unhewn/hewn/internal/provider/openai"    // registers itself with provider.Register
	"github.com/unhewn/hewn/internal/sandbox"
	"github.com/unhewn/hewn/internal/session"
	"github.com/unhewn/hewn/internal/skill"
	"github.com/unhewn/hewn/internal/slash"
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
	var (
		flagProvider string
		flagModel    string
		flagCWD      string
		flagDB       string
		flagNoTools  bool
		flagYolo     bool
	)

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

			setup, _ := cmd.Flags().GetBool("setup")
			if setup {
				return runSetup(cmd)
			}

			// --- Load layered config ---
			cfg, err := config.Load("") // user-level only; cwd unknown yet
			if err != nil {
				return fmt.Errorf("hewn: config: %w", err)
			}

			// Apply CLI flag overrides, tracking which were explicitly set so
			// they take priority over both user and project config.
			changed := map[string]bool{
				"provider": cmd.Flags().Changed("provider"),
				"model":    cmd.Flags().Changed("model"),
				"db":       cmd.Flags().Changed("db"),
				"cwd":      cmd.Flags().Changed("cwd"),
				"no-tools": cmd.Flags().Changed("no-tools"),
				"yolo":     cmd.Flags().Changed("yolo"),
			}
			config.ApplyFlags(&cfg, changed, struct {
				Provider string
				Model    string
				DB       string
				CWD      string
				NoTools  bool
				Yolo     bool
			}{Provider: flagProvider, Model: flagModel, DB: flagDB, CWD: flagCWD, NoTools: flagNoTools, Yolo: flagYolo})

			// Resolve cwd: flag or config or os.Getwd()
			if cfg.CWD == "" {
				dir, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("hewn: resolve cwd: %w", err)
				}
				cfg.CWD = dir
			} else {
				abs, err := filepath.Abs(cfg.CWD)
				if err != nil {
					return fmt.Errorf("hewn: resolve cwd %q: %w", cfg.CWD, err)
				}
				cfg.CWD = abs
			}

			// Load project-level config (now that cwd is known) and merge
			// -- but only if the flag itself wasn't set, so project config
			// can't override an explicit --cwd.
			if !changed["cwd"] {
				if err := config.LoadProject(&cfg, cfg.CWD); err != nil {
					return fmt.Errorf("hewn: config: %w", err)
				}
			}
			// Re-apply flags so explicit --provider/--model etc. still beat
			// whatever the project file says.
			config.ApplyFlags(&cfg, changed, struct {
				Provider string
				Model    string
				DB       string
				CWD      string
				NoTools  bool
				Yolo     bool
			}{Provider: flagProvider, Model: flagModel, DB: flagDB, CWD: cfg.CWD, NoTools: flagNoTools, Yolo: flagYolo})

			// Resolve DB path to an absolute path (or the default).
			cfg.DB = config.ResolveDB(cfg.DB)

			// Check if we have a working provider. If not, run the setup
			// wizard (skipped in headless mode — shows an error instead).
			promptFlag, _ := cmd.Flags().GetString("prompt")
			isHeadless := promptFlag != ""
			var resolvedCfg *config.Config
			resolvedCfg, err = config.CheckOrSetup(&cfg, isHeadless)
			if err != nil {
				return fmt.Errorf("hewn: %w", err)
			}
			cfg = *resolvedCfg

			interactive, _ := cmd.Flags().GetBool("interactive")
			if interactive {
				return runInteractive(cmd, &cfg)
			}

			prompt, _ := cmd.Flags().GetString("prompt")
			if prompt == "" {
				return runTUI(cmd, &cfg)
			}
			return runHeadless(cmd, &cfg, prompt)
		},
	}

	rootCmd.Flags().StringVar(&flagProvider, "provider", "", `provider to use: "anthropic" or "openai" (any OpenAI-compatible backend -- Ollama, llama.cpp, LM Studio, Nous Research, OpenAI itself -- via OPENAI_BASE_URL and OPENAI_API_KEY). Default taken from config; first fallback is "anthropic."`)
	rootCmd.Flags().StringVar(&flagModel, "model", "", "model to use (overrides config; default from config, or claude-opus-4-8)")
	rootCmd.Flags().StringVar(&flagCWD, "cwd", "", "project directory (default: current directory)")
	rootCmd.Flags().StringVar(&flagDB, "db", "", "session database path (default: ~/.local/share/hewn/hewn.db)")
	rootCmd.Flags().BoolVar(&flagNoTools, "no-tools", false, "disable tool use")
	rootCmd.Flags().BoolVar(&flagYolo, "yolo", false, "pre-approve every tool call for this run")
	rootCmd.Flags().StringP("prompt", "p", "", "run a prompt headless and exit")
	rootCmd.Flags().Bool("setup", false, "run the setup wizard to reconfigure provider, model, and preferences")
	rootCmd.Flags().Bool("list", false, "list recent sessions and exit")
	rootCmd.Flags().String("resume", "", `resume a session: bare flag resumes the most recent, or --resume=<id-or-prefix> for a specific one (the = is required)`)
	rootCmd.Flags().Lookup("resume").NoOptDefVal = resumeLatest
	rootCmd.Flags().Bool("interactive", false, "run an interactive session with slash commands (/help for the list)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// userAgentsPath resolves ~/.config/hewn/AGENTS.md. Returns "" (skip) if
// the home directory can't be resolved -- an absent or unreadable
// user-global file is never fatal.
func userAgentsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "hewn", "AGENTS.md")
}

// runList prints recent sessions and exits; it never touches a provider or
// the agent loop.
func runList(cmd *cobra.Command) error {
	dbFlag, _ := cmd.Flags().GetString("db")
	db := config.ResolveDB(dbFlag)

	ctx := context.Background()
	store, err := session.Open(ctx, db)
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

// built is everything runHeadless and runInteractive both need, resolved
// once so the two modes can't drift out of sync with each other (e.g. one
// honoring --resume differently than the other).
type built struct {
	loop         *agent.Loop
	sandbox      *sandbox.Sandbox
	mcpServers   *mcp.Servers
	cwd          string
	providerName string
}

// buildLoop resolves flags (including any --resume target), constructs the
// provider, the sandbox-rooted tool registry, and an agent.Loop wired to
// store. titleSource seeds a new session's title when one is created (it's
// ignored when resuming). approver becomes the loop's approval policy.
func buildLoop(ctx context.Context, cfg *config.Config, cmd *cobra.Command, store *session.Store, approver tool.Approver, titleSource string) (built, error) {
	providerName := cfg.Provider
	model := cfg.Model
	cwd := cfg.CWD
	noTools := cfg.NoTools
	yolo := cfg.Yolo

	var (
		sessionID string
		history   []provider.Message
	)

	if cmd.Flags().Changed("resume") {
		resumeArg, _ := cmd.Flags().GetString("resume")
		r, err := loadResumeTarget(ctx, store, resumeArg)
		if err != nil {
			return built{}, err
		}
		sessionID, providerName, model, cwd, history = r.sessionID, r.provider, r.model, r.cwd, r.history
	}

	system, warnings, err := ctxfile.Assemble(cwd, userAgentsPath())
	if err != nil {
		return built{}, fmt.Errorf("hewn: %w", err)
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "hewn: %s\n", w)
	}

	// Construct the provider (and everything else rooted at cwd) only
	// after resolving provider/model/cwd above, and before creating a new
	// session, so a bad --provider or missing API key never leaves an
	// empty session row behind.
	p, err := provider.New(providerName)
	if err != nil {
		return built{}, fmt.Errorf("hewn: %w", err)
	}

	sb, err := sandbox.New(cwd)
	if err != nil {
		return built{}, fmt.Errorf("hewn: %w", err)
	}

	registry := tool.NewRegistry()
	var mcpServers *mcp.Servers
	if !noTools {
		registry.Register(tool.NewRead(sb))
		registry.Register(tool.NewWrite(sb))
		registry.Register(tool.NewEdit(sb))
		registry.Register(tool.NewBash(sb, []string{"ANTHROPIC_API_KEY"}))

		var mcpWarnings []string
		mcpServers, mcpWarnings, err = mcp.Connect(ctx, cwd)
		if err != nil {
			return built{}, fmt.Errorf("hewn: %w", err)
		}
		for _, w := range mcpWarnings {
			fmt.Fprintf(os.Stderr, "hewn: %s\n", w)
		}
		for _, t := range mcpServers.Tools() {
			registry.Register(t)
		}
	}

	loop := &agent.Loop{
		Provider: p,
		Tools:    registry,
		Approval: tool.NewPolicy(approver, yolo),
		Model:    model,
		System:   system,
		Session:  store,
	}
	if history != nil {
		loop.SeedHistory(history)
	}

	if sessionID == "" {
		sess, err := store.CreateSession(ctx, cwd, providerName, model, titleSource)
		if err != nil {
			return built{}, fmt.Errorf("hewn: %w", err)
		}
		sessionID = sess.ID
	}
	loop.SessionID = sessionID

	return built{loop: loop, sandbox: sb, mcpServers: mcpServers, cwd: cwd, providerName: providerName}, nil
}

// registerSkills loads .hewn/skills/ under cwd and adds each to registry as
// a slash command, printing any problems to stderr. A skill-loading
// problem is always non-fatal -- a session must start whether or not
// skills load cleanly. baseSystem is composed with each skill's own
// prompt on activation (slash.RegisterSkills) rather than replacing it.
func registerSkills(registry *slash.Registry, tools *tool.Registry, cwd, baseSystem string) {
	skillsDir := filepath.Join(cwd, ".hewn", "skills")
	skills, warnings, err := skill.Load(skillsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hewn: loading skills: %v\n", err)
		return
	}
	warnings = append(warnings, slash.RegisterSkills(registry, skills, tools, baseSystem)...)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "hewn: %s\n", w)
	}
}

// runHeadless drives one prompt through the agent loop with a plain-stdout
// renderer: HEWN.md §5's "same agent loop, only the event sink differs".
// Every run is durably recorded -- there is no flag to turn persistence off.
func runHeadless(cmd *cobra.Command, cfg *config.Config, prompt string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store, err := session.Open(ctx, cfg.DB)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}
	defer store.Close()

	renderer := agent.NewHeadlessRenderer(os.Stdout, os.Stdin)
	b, err := buildLoop(ctx, cfg, cmd, store, renderer, prompt)
	if err != nil {
		return err
	}
	defer b.sandbox.Close()
	defer b.mcpServers.Close()

	return renderer.Render(b.loop.Run(ctx, prompt))
}

// runInteractive is a REPL: it reads lines from stdin, dispatching
// "/command" lines through the slash registry and everything else as a
// user turn through the same agent loop and renderer runHeadless uses.
func runInteractive(cmd *cobra.Command, cfg *config.Config) error {
	setupCtx := context.Background()

	store, err := session.Open(setupCtx, cfg.DB)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}
	defer store.Close()

	// One shared reader over stdin for both REPL line input and the
	// renderer's approval prompts. Two independent bufio.Readers wrapping
	// the same os.Stdin would each buffer-ahead from the raw file
	// descriptor and silently steal bytes from each other; wrapping this
	// same reader again inside NewHeadlessRenderer is safe, since it just
	// delegates Read calls through the one real source.
	stdin := bufio.NewReader(os.Stdin)
	renderer := agent.NewHeadlessRenderer(os.Stdout, stdin)

	b, err := buildLoop(setupCtx, cfg, cmd, store, renderer, "interactive session")
	if err != nil {
		return err
	}
	defer b.sandbox.Close()
	defer b.mcpServers.Close()

	registry := slash.NewRegistry()
	slash.Register(registry)
	slashCtx := &slash.Context{
		Loop:         b.loop,
		Store:        store,
		Tools:        b.loop.Tools,
		Registry:     registry,
		Out:          os.Stdout,
		CWD:          b.cwd,
		ProviderName: b.providerName,
	}
	registerSkills(registry, b.loop.Tools, b.cwd, b.loop.System)

	fmt.Fprintln(os.Stdout, "hewn interactive -- /help for commands, /quit or Ctrl+D to exit")

	for {
		fmt.Fprintf(os.Stdout, "%s", "> ")

		line, err := stdin.ReadString('\n')
		if err != nil {
			return nil // EOF (Ctrl+D) ends the session cleanly
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if result, handled := registry.Dispatch(setupCtx, slashCtx, line); handled {
			fmt.Fprintln(os.Stdout, result.Output)
			if result.Quit {
				return nil
			}
			continue
		}

		// A fresh cancellable context per turn, not one for the whole
		// REPL: signal.NotifyContext's context is done permanently after
		// the first Ctrl+C, so reusing one across turns would mean
		// interrupting one generation silently breaks every turn after it.
		turnCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		if err := renderer.Render(b.loop.Run(turnCtx, line)); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		cancel()
	}
}

// runTUI launches the Bubble Tea front end, wired through the same
// buildLoop setup as the other two modes: AGENTS.md invariant #1 means the
// TUI itself must never construct a provider, tool registry, sandbox, or
// session store, so all of that happens here and an already-wired
// *agent.Loop is handed to tui.Start. Ctrl+C is handled inside the TUI
// itself (Bubble Tea reads raw terminal input, so it arrives as an
// ordinary keypress, not an OS signal), so there's no signal.NotifyContext
// here unlike the other two modes.
func runTUI(cmd *cobra.Command, cfg *config.Config) error {
	ctx := context.Background()

	store, err := session.Open(ctx, cfg.DB)
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}
	defer store.Close()

	approver := tui.NewApprover()
	b, err := buildLoop(ctx, cfg, cmd, store, approver, "tui session")
	if err != nil {
		return err
	}
	defer b.sandbox.Close()
	defer b.mcpServers.Close()

	registry := slash.NewRegistry()
	slash.Register(registry)
	slashCtx := &slash.Context{
		Loop:         b.loop,
		Store:        store,
		Tools:        b.loop.Tools,
		Registry:     registry,
		CWD:          b.cwd,
		ProviderName: b.providerName,
	}
	registerSkills(registry, b.loop.Tools, b.cwd, b.loop.System)

	return tui.Start(b.loop, approver, slashCtx, b.cwd, b.providerName, cfg.Name)
}

// runSetup forces the setup wizard to re-run. It ignores the current
// provider and walks the user through choosing a new one.
func runSetup(cmd *cobra.Command) error {
	// If --db was explicitly passed, we don't need it for setup, but note
	// it since the new config won't touch it.
	cfg, err := config.ForceSetup()
	if err != nil {
		return fmt.Errorf("hewn: %w", err)
	}

	// Apply config env vars in case the user picked a provider that needs them.
	config.ApplyEnv(*cfg)

	_ = cmd // unused in setup mode
	return nil
}
