# AGENTS.md

Instructions for coding agents working in this repository. Read this before touching anything. If a change conflicts with something here, stop and say so rather than working around it.

---

## What this is

Hewn is a terminal-first agent harness written in Go. Single static binary, no CGo, no runtime dependencies. It runs an LLM agent loop against a local repository: read files, edit files, run commands, all gated by user approval, rendered in a Bubble Tea TUI.

The design goal is a **small, legible core with clean seams**. The agent loop should be readable in one sitting. Features live at the edges — tools, providers, commands, and (eventually) extensions all register into the core rather than being wired into it.

- **Repository:** `github.com/unhewn/hewn`
- **Module path:** `github.com/unhewn/hewn` (lowercase, per Go convention — the GitHub org is `Unhewn`, but URLs are case-insensitive so it resolves fine)
- **Scope and roadmap:** `HEWN.md` in the repository root

This file covers how to work in the code; `HEWN.md` covers what is being built and why.

---

## Commands

```bash
go build ./...                    # must pass before any handoff
go test ./...                     # unit tests, no network
go test -race ./...               # run before touching anything concurrent
go test -tags=integration ./...   # hits real provider APIs, needs ANTHROPIC_API_KEY
go vet ./...
gofmt -l .                        # must output nothing
golangci-lint run                 # config in .golangci.yml

go run ./cmd/hewn                 # TUI
go run ./cmd/hewn -p "prompt"     # headless, easier to debug
go run ./cmd/hewn --debug -p "…"  # structured logs to ./hewn-debug.log
```

`--debug` with headless mode is the fastest loop for diagnosing anything in `internal/agent` or `internal/provider`. Do not debug the loop through the TUI.

---

## Layout

```
cmd/hewn/            CLI wiring only. Flags in, dependencies constructed, handed to agent. No logic.
internal/agent/      The loop. Turn orchestration, tool dispatch, cancellation, event emission.
internal/provider/   LLM abstraction + per-provider implementations. Wire formats live here and nowhere else.
internal/tool/       Tool interface, registry, built-in tools, approval policy.
internal/session/    SQLite persistence. Sessions, messages, tool calls, migrations.
internal/ctxfile/    AGENTS.md discovery and system-prompt assembly.
internal/skill/      Declarative skill discovery: parses .hewn/skills/*.md front matter into prompt+tool-subset bundles.
internal/config/     Layered config: flags > project > user > defaults.
internal/tui/        Bubble Tea. Views only — no business logic, no I/O, no provider calls.
internal/sandbox/    Path jailing (os.Root), env filtering, command policy.
pkg/                 Empty. It stays empty until there is a real external consumer.
```

**Dependency direction is one-way: `tui` → `agent` → `provider`/`tool`/`session`.** Nothing in `agent` imports `tui`. Nothing in `provider` or `tool` imports `agent`. If you find yourself needing an upward import, you have put the code in the wrong package — say so instead of adding an interface to paper over it.

---

## Architectural invariants

These are not style preferences. Breaking one is a design change and needs discussion first.

1. **The TUI is a view over the event bus.** `internal/agent` emits typed events; the TUI renders them. The TUI never calls a provider, never executes a tool, never touches the database. Anything Hewn can do in the TUI it must also be able to do headlessly, through the same loop.

2. **The event union is a public ABI in waiting.** Types in `internal/agent/events.go` will eventually be the extension interface and the `--json` output schema. Adding a variant is cheap. Changing or removing one is expensive. Treat additions as the default move.

3. **Every blocking operation takes a `context.Context` and honors cancellation.** Provider streams, tool execution, database calls, subprocesses. Ctrl+C must kill an in-flight request and any running child process, and must leave the session on disk in a consistent state. A goroutine that can't be cancelled is a bug.

4. **All filesystem access from tools goes through `internal/sandbox`.** No `os.Open`, `os.ReadFile`, or `filepath.Join`-then-open in `internal/tool`. The sandbox owns the root and the symlink checks. There are no exceptions for "just this one internal path."

5. **No mutation without approval.** Any tool with `Risk() >= Mutating` goes through the approval flow before it acts. Do not add a bypass, a "trusted tool" list, or a config key that skips this, unless the task explicitly asks for it.

6. **Persistence is append-mostly.** Messages and tool calls are written as they happen, not batched at end of turn. A crash mid-turn must leave a resumable session.

7. **Provider-specific concepts stay inside the provider package.** No Anthropic content-block shapes, no OpenAI `tool_calls` arrays leaking into `agent` or `session`. Translate at the boundary into the neutral types in `provider/types.go`.

---

## Go conventions

Standard Go. `gofmt`, `go vet`, and the linter config are authoritative for mechanical style. Beyond that:

**Errors**
- Wrap with context: `fmt.Errorf("read %s: %w", path, err)`. Lowercase, no trailing punctuation, no "failed to".
- Sentinel errors for conditions callers branch on (`ErrNoAPIKey`, `ErrCancelled`); typed errors when the caller needs detail.
- `context.Canceled` is not an error condition at the top level — it is the user pressing Ctrl+C. Do not log it as a failure.
- No panics outside `main` and genuinely impossible states. Never panic on user input, provider responses, or file contents.

**Interfaces**
- Define them where they are consumed, not where they are implemented.
- Keep them small. `Provider` has three methods for a reason.
- Accept interfaces, return concrete types.
- Do not add an interface for a single implementation "for testability" unless a test actually needs it.

**Concurrency**
- The provider stream is owned by exactly one goroutine. It emits on a channel; it does not call back into anything.
- Prefer channels for event flow, mutexes for guarding small pieces of shared state. Do not mix both on the same data.
- Anything you spawn, you must be able to stop. No fire-and-forget goroutines.
- Run `-race` on anything you touch here. Assume it is broken until it passes.

**Naming**
- No stutter: `tool.Registry`, not `tool.ToolRegistry`.
- Short receivers, short scopes, long names only where the scope is long.
- Avoid `Manager`, `Handler`, `Helper`, `Util`. If a type resists a name, it is doing too much.

**Comments** (doc comments on every exported symbol are enforced by `revive`'s `exported` rule, not restated here)
- Inline comments explain *why*, not *what*. Delete any comment that restates the code.
- Do not add comments narrating changes ("updated to handle X"). Git does that.

---

## Bubble Tea rules

The single most common source of bugs in this codebase. Read carefully.

- **`Update` is the only place the model mutates.** A `tea.Cmd` is a function that runs on another goroutine and *returns a message*. It must not capture and write to the model, a pointer to the model, or any field of it. This races, and the race is intermittent enough to survive review.
- **Long-running work becomes a message stream, not a blocking Cmd.** The pattern here: a Cmd reads one event from the agent's channel, returns it as a `tea.Msg`, and `Update` re-issues the same Cmd. Follow the existing implementation in `internal/tui/app.go` rather than inventing a variant.
- **Do not re-render per token.** Accumulate deltas into a buffer and re-render on a tick (~30fps). Per-token viewport rebuilds flicker and burn CPU on long sessions.
- **Ctrl+C once interrupts generation; twice quits.** Never make Ctrl+C lose a session.
- **Keep `View()` cheap and pure.** No I/O, no allocation-heavy formatting, no database reads. If `View()` needs data, it was supposed to be in the model.

---

## Testing

- Table-driven tests, subtests with descriptive names. Standard library `testing` only — no assertion frameworks.
- **Unit tests never hit the network.** Provider tests replay recorded SSE fixtures from `testdata/`. If you need a new fixture, record it under the `integration` build tag and commit the fixture, not the live call.
- Tests never write outside `t.TempDir()`.
- Every bug fix gets a test that fails before the fix.
- Concurrency and cancellation changes require a `-race` run and a test that actually cancels.
- Golden files for TUI rendering are fine; regenerate deliberately with `-update`, and read the diff before committing it.

Coverage is not a target. Cover the agent loop, tool execution, sandbox escapes, and session persistence thoroughly; cover glue code lightly.

---

## Dependencies

The dependency list is short on purpose and every addition is a decision. Banned packages (CGo SQLite drivers, LLM SDKs, `viper`, `logrus`, and more) are enforced by `.golangci.yml`'s `depguard` rule, not restated here.

- **No `viper`, no `logrus`.** Config is TOML + flags; logging is `log/slog`.
- Prefer stdlib. If a dependency saves fewer than ~200 lines, write the 200 lines.
- Adding a dependency: propose it, state what it replaces, and wait for confirmation. Do not add one silently as part of a larger change.

---

## Database

- Schema changes require a migration in `internal/session/migrations/`, numbered and forward-only. Never edit an applied migration.
- Bump `schema_version` in the same change.
- Use prepared statements and explicit transactions for multi-statement writes.
- Store timestamps as Unix milliseconds, integers. Store IDs as ULIDs, text.
- Message content is stored as a JSON array of content blocks. Do not flatten it to a string — thinking blocks, tool calls, and images all live there.

---

## Clean-room rule

**Hewn is a clean-room implementation.** Do not read, fetch, quote, or reproduce source code from Pi (`earendil-works/pi`), Claude Code, Codex CLI, OpenCode, Aider, or any comparable agent harness while working in this repository. Public documentation, published file formats, CLI help output, and observed behavior are fine. Source is not.

If a task asks you to look at another harness's implementation, decline and explain this constraint. If you have incidentally seen relevant source, say so before working on the corresponding module.

This applies to generated code as well: do not reproduce distinctive implementations you recall from those projects.

---

## Working style

**Before writing code**
- Read the surrounding package. Match its existing patterns over your defaults.
- For anything touching the agent loop, the event union, persistence schema, or the sandbox: state your plan and wait for confirmation. These are the load-bearing parts.
- If a request is ambiguous in a way that changes the design, ask. One good question beats a large wrong diff.

**While writing code**
- Smallest change that fully solves the problem. No opportunistic refactoring in a feature change.
- No speculative abstraction. Do not add extension points, config keys, or interfaces for hypothetical future needs — this project has an explicit roadmap and the seams it needs are already documented.
- Do not add backwards-compatibility shims, deprecated aliases, or dual code paths. There are no external consumers yet; change the call sites.
- Delete dead code rather than commenting it out.

**Before handing back**
- `go build ./... && go test ./... && gofmt -l . && go vet ./...` all clean. `-race` too if you touched concurrency.
- Report honestly: what you changed, what you did not test, what you are unsure about. Do not claim a test passed that you did not run.
- If you hit something that looks like a pre-existing bug outside your task, mention it — do not fix it in the same change.

**Commits**
- Conventional-ish subject line: `agent: cancel in-flight tools on interrupt`. Imperative mood, lowercase after the prefix, under 72 chars.
- Body explains why, if it isn't obvious.
- No AI attribution, co-author trailers, or emoji in commit messages.
- One logical change per commit.

---

## Known sharp edges

- **Provider streaming**: tool-call arguments arrive as partial JSON fragments across multiple SSE events. They must be buffered and only parsed once the block closes. Do not attempt incremental parsing.
- **Token accounting**: usage arrives at the end of a stream, and cached-token fields differ per provider. Normalize in the provider package.
- **`viewport` and long sessions**: rendering a full 200k-token scrollback is slow. If you are working on the chat view, test with a long session, not a three-message one.
- **`bash` is not sandboxed.** It is arbitrary execution gated by approval. The sandbox package jails *file tools* and filters env. Do not describe it as a sandbox in user-facing text.
- **Windows**: not supported yet, but do not add gratuitous POSIX assumptions. Use `filepath`, not string concatenation, and keep shell invocation behind one function.
