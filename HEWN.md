# Hewn — Scope, Architecture, and Next Steps

> **Hewn** — a minimal, hackable agent harness written in Go. Single static binary, TUI-first, provider-agnostic, built by dogfooding: the foundation is just good enough to use Hewn to build the rest of Hewn.

**Repository:** `github.com/unhewn/hewn` · **Module path:** `github.com/unhewn/hewn`

**Status:** v0.1 complete (items 1–10 built and committed). Config system built (item 10), using YAML instead of the originally-planned TOML for consistency with the rest of the project. **Ready for dogfooding.** The event bus (item 11) remains unimplemented — the agent loop currently renders directly to the TUI rather than through a typed event stream. This is the gap to close before calling v0.1 fully done, but it doesn't block dogfooding: Hewn can already read its own source, propose edits, apply them, run tests, and report the result. Starting dogfooding now and logging friction to `FRICTION.md` is the highest-value next step.

---

## 0. Provenance & legal posture

The design conversation that seeded this project referenced Pi (`@earendil-works/pi-coding-agent`), Claude Code, Codex CLI, Aider, OpenCode, and others.

**Clean-room rule (non-negotiable):**

- Do **not** read Pi's (or any competitor's) source while implementing Hewn.
- Design only from public docs, observed CLI behavior, and published file formats.
- Interfaces, file formats, and UX conventions (e.g. `AGENTS.md`, slash commands, JSON session logs) are **not** copyrightable as such — reimplementing them is standard practice.
- Ship MIT, and put a line in the README: *"Hewn is a clean-room Go implementation inspired by the UX of minimalist agent harnesses such as Pi. No source was copied."*
- If you ever *do* read their source, note it and avoid touching the corresponding Hewn module for a while. Keep the boundary clean.

**Caveat on the source thread:** several factual claims in the Grok conversation (product names, launch dates, license of specific repos, install URLs) should be independently verified before they land in the README or docs. Treat them as leads, not facts.

---

## 1. What Hewn is (and isn't)

### Is
- A **harness**: the loop, the tools, the context, the UI, the persistence. The model is a pluggable dependency.
- **Minimal core, wide seams.** Small built-in toolset, small system prompt, everything else opt-in.
- **Local-first.** Runs on your machine, in your repo, against your API keys.
- **Terminal-native.** TUI is the primary interface; headless is a first-class second.

### Isn't (at least not in v0.1)
- Not a hosted service, not a web app, not a team collaboration product.
- Not an opinionated autonomous workflow engine (no forced plan/execute/review pipeline).
- Not a model. Not a RAG system. Not an IDE plugin.

### Design principles
1. **Dogfood gate.** A feature ships when Hewn was used to build it, or when its absence blocked building something else.
2. **Boring core.** The agent loop should be ~300 lines you can read in one sitting.
3. **Everything observable.** Every message, tool call, and token count is inspectable from the TUI and from disk.
4. **No surprises on disk.** Hewn never writes outside the project dir or `~/.config/hewn` without asking.
5. **Seams before features.** Establish the extension seam early even if nothing uses it, so features can move outward later.

---

## 2. Scope

### `[IN v0.1]` — The foundation (the thing this doc is actually about)

The goal of v0.1 is exactly one thing: **a TUI you can hold a real conversation in, that can read and edit files and run commands in your repo, and that remembers the session.** That's it. From there you build Hewn with Hewn.

| # | Item | Definition of done | Status |
|---|---|---|---|
| 1 | CLI skeleton | `hewn` opens TUI; `hewn -p "..."` runs headless and exits; `--model`, `--provider`, `--cwd`, `--no-tools` flags | ✓ |
| 2 | One provider, streaming | Anthropic Messages API with SSE streaming + tool use. Interface designed for N providers, implemented for 1. | ✓ |
| 3 | Agent loop | user msg → stream assistant → parse tool calls → execute → feed results → repeat until stop. Cancellable mid-stream. | ✓ |
| 4 | Four tools | `read`, `write`, `edit` (exact-string replace), `bash`. All rooted to project dir. | ✓ |
| 5 | Tool approval | Interactive prompt per call; `a` = allow once, `A` = allow this tool for the session, `d` = deny with feedback. | ✓ |
| 6 | TUI | Scrollback viewport, multiline input, status bar (model / tokens / cwd / state), streaming render, Ctrl+C interrupt, mouse scroll. | ✓ |
| 7 | Slash commands | `/help /model /new /clear /compact /quit /tools /cost /export` — dispatched through a registry, not a switch statement. | ✓ |
| 8 | Persistence | SQLite at `~/.local/share/hewn/hewn.db`. Sessions + messages + tool calls. `hewn --resume`, `hewn --list`. | ✓ |
| 9 | Context files | Load `AGENTS.md` walking up from cwd, plus `~/.config/hewn/AGENTS.md`. Concatenate into system prompt. | ✓ |
| 10 | Config | `~/.config/hewn/config.yaml` + `.hewn/config.yaml` project override + env vars. YAML (not TOML) for consistency with skill front matter. Precedence: flags > project > user > defaults. | ✓ |
| 11 | Event bus | Internal typed event stream (`agent → UI`). Every UI update goes through it. **This is the seam extensions will hook later.** | ❌ — not yet built |

**Non-goals for v0.1, explicitly:** no subagents, no planning mode, no memory vault, no extensions, no web dashboard, no voice, no session tree/branching (linear sessions only), no vector search, no MCP.

### `[NEXT]` — v0.2, in rough dependency order

- ✓ Additional providers — OpenAI-compatible provider already built (`internal/provider/openai/`), covering Ollama, llama.cpp, LM Studio, Nous Research, and OpenAI itself via `OPENAI_BASE_URL`. `/model` switching mid-session still pending.
- ✓ Declarative skills — Markdown + front matter in `.hewn/skills/`. Built in `internal/skill/` and wired via `internal/slash/skills.go`.
- ✓ MCP client — connecting to servers declared in `.hewn/mcp.json`. Built in `internal/mcp/`.
- Auto-compaction when context crosses a threshold (summarize oldest N, keep pinned)
- Session tree: fork / branch / `/tree` navigation (schema already supports it via `parent_id`)
- Background bash (long-running processes, streamed output, `/jobs`)
- Planning mode (`/plan`): read-only tool profile + structured plan → approve → execute
- Better diffs: preview edits as unified diff before applying
- **Extensions** — see §6

### `[LATER]`
Parallel/dynamic subagents · memory vault + embeddings · MCP client (and maybe server) · queue/steer while thinking · git-native integration · themes · web dashboard · JSON-RPC SDK mode · voice

### `[UNDECIDED]`
Extension mechanism (§6) · whether MCP replaces a native extension system for *tools* specifically · session storage format (SQLite-only vs SQLite + JSONL mirror) · whether `pkg/` exposes a public SDK at all in year one.

---

## 3. Architecture

```
hewn/
├── cmd/hewn/main.go            # cobra wiring only, ~100 lines
├── internal/
│   ├── agent/                  # the loop, cancellation, tool dispatch
│   │   ├── loop.go
│   │   └── events.go           # typed event bus (agent -> UI/extensions)
│   ├── provider/               # LLM abstraction
│   │   ├── provider.go         # interface + shared types
│   │   ├── anthropic/
│   │   └── registry.go
│   ├── tool/                   # Tool interface + registry
│   │   ├── tool.go
│   │   ├── read.go write.go edit.go bash.go
│   │   └── approval.go
│   ├── session/                # SQLite persistence, session model
│   ├── ctxfile/                # AGENTS.md discovery + assembly
│   ├── config/                 # layered config
│   ├── tui/                    # Bubble Tea
│   │   ├── app.go              # root Model
│   │   ├── chat.go input.go status.go approval.go
│   │   ├── slash/              # command registry
│   │   └── theme.go
│   └── sandbox/                # os.Root path jailing, command policy
├── pkg/                        # empty for now. Resist.
├── go.mod
├── LICENSE (MIT)
└── README.md
```

**Toolchain:** latest stable Go (needs ≥1.24 for `os.Root`). No CGo — use `modernc.org/sqlite`, not `mattn/go-sqlite3`, so cross-compilation stays trivial.

**Dependencies (keep this list short and defend additions):**

| Purpose | Choice | Note |
|---|---|---|
| TUI | `charmbracelet/bubbletea` + `bubbles` + `lipgloss` | v2 if stable; check API churn |
| Markdown render | `charmbracelet/glamour` | for assistant output |
| CLI | `spf13/cobra` | 14 flags and growing — cobra's flag-binding suits this better than urfave/cli's positional-arg shape |
| SQLite | `modernc.org/sqlite` | pure Go, no CGo |
| Config | `gopkg.in/yaml.v3` | already pulled in by internal/skill for front matter; reused by internal/config |
| Diff | `sergi/go-diff` or hand-rolled | only needed for preview UI |
| LLM client | **hand-rolled** | do *not* pull langchaingo; the Anthropic/OpenAI wire formats are simple and you want full control over streaming + tool-call deltas |

### Core interfaces (sketch — expect these to change once real code exists)

```go
// provider
type Provider interface {
    Name() string
    Models(ctx context.Context) ([]ModelInfo, error)
    Stream(ctx context.Context, req Request) (Stream, error)
}

type Stream interface {
    Next() (Event, error) // io.EOF terminates
    Close() error
}

// Event is a discriminated union: TextDelta, ThinkingDelta, ToolCallStart,
// ToolCallDelta, ToolCallEnd, Usage, StopReason.

// tool
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage      // JSON Schema for params
    Risk() RiskLevel              // ReadOnly | Mutating | Arbitrary
    Execute(ctx context.Context, params json.RawMessage, io ToolIO) (Result, error)
}

// ToolIO gives tools a way to stream partial output and request approval,
// so `bash` can render live and `edit` can show a diff before applying.
```

### Streaming & concurrency model

- One goroutine owns the provider stream; it emits `agent.Event` values on a channel.
- Bubble Tea consumes that channel via a `tea.Cmd` that reads one event and re-issues itself. **Never mutate the Model from inside a Cmd closure** — the sketch in the source thread does exactly that and it's a data race. Cmds must return messages; only `Update` mutates.
- `context.Context` cancellation on Ctrl+C or Esc kills the in-flight request and any running tool.
- Tool execution runs in its own goroutine, streams partial output as events, and can be individually cancelled.

### SQLite schema (v0.1 minimum)

```sql
CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,          -- ULID
  parent_id    TEXT REFERENCES sessions(id),  -- forking, unused in v0.1
  fork_point   INTEGER,                   -- message seq forked from
  title        TEXT,
  cwd          TEXT NOT NULL,
  provider     TEXT NOT NULL,
  model        TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

CREATE TABLE messages (
  id           TEXT PRIMARY KEY,
  session_id   TEXT NOT NULL REFERENCES sessions(id),
  seq          INTEGER NOT NULL,
  role         TEXT NOT NULL,             -- user|assistant|tool|system
  content      TEXT NOT NULL,             -- JSON: array of content blocks
  input_tokens  INTEGER,
  output_tokens INTEGER,
  created_at   INTEGER NOT NULL,
  UNIQUE(session_id, seq)
);

CREATE TABLE tool_calls (
  id           TEXT PRIMARY KEY,
  message_id   TEXT NOT NULL REFERENCES messages(id),
  tool         TEXT NOT NULL,
  params       TEXT NOT NULL,
  result       TEXT,
  approved     INTEGER,                   -- 0 denied, 1 once, 2 session-wide
  duration_ms  INTEGER,
  created_at   INTEGER NOT NULL
);

CREATE TABLE approvals (
  session_id   TEXT NOT NULL,
  tool         TEXT NOT NULL,
  scope        TEXT NOT NULL,             -- e.g. "bash:git *" or "*"
  PRIMARY KEY (session_id, tool, scope)
);
```

Add a `schema_version` pragma/table from day one so migrations aren't archaeology later.

### Sandboxing (v0.1 = honest, not bulletproof)

- File tools: `os.Root` rooted at project dir. Refuse symlink escapes, refuse absolute paths outside root.
- `bash`: runs with cwd = project dir, inherits env minus a denylist (`AWS_*`, `*_TOKEN`, `*_KEY` except the one in use — configurable). **It is not a sandbox.** Approval prompts are the real control.
- Be explicit in the README that `bash` is arbitrary execution gated by approval, not confinement. Real sandboxing (seccomp/landlock/container) is `[LATER]` and platform-specific.

---

## 4. TUI design

Single-pane chat, not a multi-pane IDE. Panes come later if they earn it.

```
┌──────────────────────────────────────────────────┐
│ hewn · claude-opus-4-8 · ~/src/hewn · 12.4k tok  │  status (top)
├──────────────────────────────────────────────────┤
│                                                  │
│  you  refactor the tool registry                 │
│                                                  │
│  ● reading internal/tool/tool.go                 │  tool call, collapsed
│  ● bash  go test ./...            [12 lines ▸]   │  expandable
│                                                  │
│  hewn  The registry currently...                 │  glamour-rendered
│                                                  │
├──────────────────────────────────────────────────┤
│ > _                                              │  textarea, grows to 8 lines
└──────────────────────────────────────────────────┘
  ⏎ send · ⇧⏎ newline · ^C interrupt · / commands
```

Details that matter for daily use:
- **Streaming must not flicker.** Render deltas into a buffer; re-render the viewport at ~30fps, not per-token.
- **Tool calls collapse by default**, expand with a keybind. `bash` output tails live while running.
- **Approval is inline**, not a modal that loses your scroll position.
- **Ctrl+C once = interrupt generation. Twice = quit.** Never lose a session to a stray Ctrl+C.
- Scrollback must handle a 200k-token session without choking — consider virtualized rendering early if `viewport` struggles.
- Slash commands: registry-based, with completion popup on `/`.

---

## 5. Headless mode

`hewn -p "prompt"` shares the exact same agent loop; only the event sink differs (plain stdout renderer instead of Bubble Tea). Get this right in v0.1 — it's how you test the loop without a terminal, and it's the substrate for CI use and for the eventual SDK.

- `--json` emits one JSON event per line (same event types as the bus).
- `--yolo` / `--allow` for non-interactive approval policy.
- Exit code reflects success/failure of the final turn.

---

## 6. Extensions — the undecided part

**What needs deciding first: what does an extension *extend*?** The mechanism follows from the answer. Candidate surfaces:

| Surface | Example | Needs code? |
|---|---|---|
| A. New tools | `grep`, `web_fetch`, `jira_issue` | Yes (or MCP) |
| B. Slash commands | `/review`, `/standup` | Sometimes |
| C. Prompt/skill bundles | "code review" persona + tool subset | No — data only |
| D. Hooks on events | pre-tool-call lint, post-edit format, cost logging | Yes |
| E. UI panels / themes | token graph, file tree | Yes, and deeply coupled |
| F. Providers | new LLM backend | Yes |

Most real-world value is in **A, C, and D**. E is where every plugin system goes to die.

### Options

**1. In-process Go registry (compile-time)**
Extensions are Go packages; users fork or build a custom binary.
- ➕ Zero machinery, full type safety, best performance, trivially debuggable.
- ➖ No hot reload, users must have Go, no sharing without recompiling. Kills the "ask Hewn to write an extension" magic.
- *Verdict:* Correct for v0.1 internals regardless of what wins. Not a user-facing story.

**2. Go `plugin` package (.so)**
- ➕ Native Go.
- ➖ Linux/macOS only, exact-toolchain-and-dependency-version match required, no unloading. Widely regarded as unusable in practice.
- *Verdict:* No.

**3. Embedded scripting — Lua (`gopher-lua`), Starlark, Risor, Tengo, `expr`**
- ➕ Hot reload, no build step, sandboxable, small. Lua is fast and familiar; Starlark is deterministic and Python-ish; Risor is Go-ish with a batteries-included stdlib.
- ➖ Second language in the project. Weak tooling/LSP for the user. Ecosystem is whatever you expose. Async/streaming from a script is awkward.
- *Verdict:* Best fit if hooks (D) and small custom tools (A) are the target. **Starlark or Risor over Lua** if you want the script language to feel less alien to a Go/Python-shaped audience; Lua if raw speed and embedding maturity matter most.

**4. WASM via `wazero` (or Extism on top of it)**
- ➕ Pure Go host, no CGo, real sandbox (memory + no ambient syscalls), any source language (Rust/Go/TS via Javy/Zig). Extism gives you a plugin ABI and SDKs for free.
- ➖ Host↔guest ABI is fiddly (strings/JSON over linear memory — Extism hides most of this). Toolchain burden on extension authors. Debugging is worse. Streaming and long-running calls need design.
- *Verdict:* The strongest "serious" answer if you want a real third-party extension ecosystem with untrusted code. Heavier than v0.2 needs.

**5. Subprocess + JSON-RPC over stdio (the "write it in any language" model)**
Extensions are executables speaking a documented line-protocol; Hewn spawns and supervises them.
- ➕ Any language including TypeScript (recovers Pi's actual advantage). Crash isolation. Hot reload = restart the process. OS-level sandboxing available per-process. You already need this shape for MCP.
- ➖ Process overhead, lifecycle management, protocol versioning is now a real API you must not break.
- *Verdict:* **Probably the pragmatic winner.** It's the same machinery as MCP, so you build one transport and get two features.

**6. MCP (Model Context Protocol) as the extension system for tools**
- ➕ Enormous existing ecosystem of servers — you get dozens of integrations for free on day one. Standard, documented, well-supported. Go SDK exists.
- ➖ Only covers tools/resources/prompts. **Cannot** do hooks, UI, slash commands, or provider plugins. Per-server process overhead; some servers are low quality.
- *Verdict:* Ship MCP client support and you've solved surface A almost entirely. Then a native mechanism only needs to cover C and D — a much smaller problem.

**7. Declarative YAML/Markdown only (skills)**
Front-mattered Markdown: prompt + allowed tools + trigger.
- ➕ Trivial to write, trivial to share, zero risk, users can author them *in* Hewn (great dogfooding). Covers C entirely.
- ➖ No logic. Not an extension system, and shouldn't pretend to be.
- *Verdict:* Do this in v0.2 regardless. Cheapest value in the whole doc.

### Recommended path (actual progress as of 2026-07-18)

```
v0.1  Internal Go registries for tools/commands/providers.    ✓
      Config system (YAML).                                    ✓
      Event bus not yet built — deferred behind dogfooding.     ❌

v0.2  (a) Declarative skills — Markdown + front matter.       ✓
      (b) MCP client.                                          ✓
      OpenAI-compatible provider (Ollama, LM Studio, etc.).    ✓

now   Dogfood. Log friction. Let FRICTION.md reorder the
      remaining v0.2 backlog: compaction, session tree,
      background bash, planning mode, better diffs, event bus.

v0.3  Decide between subprocess-RPC and WASM for hooks (D) and
      first-class extensions, informed by what actually annoyed you
      while dogfooding v0.2. Default lean: subprocess-RPC, reusing
      the MCP transport layer.

???   Scripting (Starlark/Risor) only if D turns out to need something
      lighter than a process per hook.
```

The whole point of the dogfood approach is that this decision gets *easier* the longer you defer it — so defer it, but keep the event bus clean enough that any of these can bolt on.

---

## 7. Next steps

### Current state (2026-07-18)

v0.1 items 1-10 are built and committed. The event bus (item 11) is the only v0.1 gap — it doesn't block dogfooding so it's been deferred to let real usage surface what's actually needed from it.

Already built ahead of v0.2 deadline:
- ✓ OpenAI-compatible provider (covers Ollama, llama.cpp, LM Studio, Nous, OpenAI itself)
- ✓ Declarative skills (`.hewn/skills/*.md` with YAML front matter)
- ✓ MCP client (`.hewn/mcp.json` server declarations)

### Next: start dogfooding

The "self-hosting" milestone is already reachable — Hewn can read its own source, write edits, run tests. The real next step is **using it** and logging every friction point to `FRICTION.md`. That file is the actual roadmap from here.

1. Use Hewn for real work. Log everything that's worse than using another agent harness to `FRICTION.md`.
2. When a friction hits repeat-3-or-more or blocks completing a task, fix it.
3. After a week of dogfooding, review `FRICTION.md` for pattern — that pattern decides the v0.2 build order, not this doc.

### Immediate backlog (unordered, will be reordered by FRICTION.md)

- Auto-compaction (`/compact`) — summarize oldest N turns to free context
- Session tree: fork / branch / `/tree` navigation
- Background bash (long-running processes, streamed output, `/jobs`)
- Planning mode (`/plan`): read-only tool profile + structured plan → approve → execute
- Better diffs: preview edits as unified diff before applying
- Event bus (item 11) — typed event stream for the agent → UI seam
- Extensions mechanism (deferred per §6 recommended path)

### Things to get right early because they're painful later
- Schema versioning + migrations
- The event type union (it becomes the extension ABI)
- Cancellation plumbed through every layer
- Token accounting + cost display (you will want this on day two)
- Structured logging to a file, off by default, `--debug` to enable

---

## 8. Open questions

These block or shape decisions above — answers can be dropped straight into the relevant section.

**Scope & use**
1. Is Hewn primarily a **coding** agent, or a general-purpose one that happens to be good at code? (Affects tool set and whether git integration is core.)
2. Personal tool, or something you intend to open-source and support? (Changes how much the extension ABI matters, and how much the clean-room paperwork matters.)
3. Single active session, or do you want multiple concurrent agents in one TUI eventually? Worth knowing now — it's a structural difference.

**Models & providers**
4. Which provider first, and do you have API keys or are you hoping to use subscription OAuth (Claude Pro / ChatGPT Plus)? OAuth-against-subscription is a meaningfully different and murkier engineering path — worth an explicit decision.
5. Do you need local models (Ollama / llama.cpp) in the first six months?
6. Reasoning/thinking-token rendering — do you want it visible in the TUI?

**Tools & safety**
7. Is `bash` gated by approval acceptable to you personally, or do you want real sandboxing (container/landlock) before you'll run it unattended?
8. Do you want an `--allow-all` mode at all? (Easy to add, hard to remove.)

**TUI**
9. Bubble Tea confirmed? Any interest in alternatives (`tview`, or raw `tcell`) — Bubble Tea's Elm architecture is great for state clarity but streaming-heavy rendering takes care.
10. Single-pane chat, or do you have a multi-pane layout in mind?
11. Any strong theme/aesthetic preference, or default to a dark 256-color palette and add theming later?

**Extensions**
12. **Which of surfaces A–F in §6 do you actually care about?** This is the single highest-leverage answer in this document.
13. Would you personally write extensions in TypeScript if you could, or would you rather everything stay Go?
14. Is MCP support something you want, or something you'd rather avoid (extra process management, variable server quality)?

**Practical**
15. Any hard dependency constraints — e.g. "stdlib-heavy," "no CGo" (I've assumed no CGo), max dependency count?
16. Repo public from day one, or private until it's usable?
17. Do you want tests/CI from the start, or move fast and add them at v0.2?
