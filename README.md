Hewn - a minimal, hackable agent harness written in Go. Single static binary, TUI-first, provider agnostic, built by dogfooding; the foundation is just good enough to use Hewn to build the rest of Hewn. 


Hewn is a clean-room Go implementation inspired by the UX of minimalist agent harnesses such as Pi. No source was copied.

## Running Hewn

Build and install the binary, or run it directly with `go run`:

```bash
go build -o hewn ./cmd/hewn
./hewn
```

```bash
go run ./cmd/hewn
```

With no flags, this opens the TUI in the current directory using Anthropic's API, so `ANTHROPIC_API_KEY` must be set:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
hewn
```

### Modes

```bash
hewn                       # TUI
hewn -p "prompt"           # headless: run one prompt and exit
hewn --interactive         # REPL: slash commands + turns, no TUI
hewn --list                # list recent sessions and exit
hewn --resume              # resume the most recent session
hewn --resume=<id-or-prefix>  # resume a specific session (the = is required)
```

### Using a local model instead

To run against an OpenAI-compatible backend (Ollama, llama.cpp's server, LM Studio, or any hosted OpenAI-compatible API) instead of Anthropic:

```bash
hewn --provider openai --model <model-name>
```

`OPENAI_BASE_URL` defaults to `http://localhost:11434/v1` (a local Ollama instance); set it to point elsewhere. `OPENAI_API_KEY` is optional — omit it for backends like Ollama that don't require auth.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `-p, --prompt` | | run one prompt headless and exit |
| `--interactive` | `false` | run an interactive REPL (`/help` for slash commands) |
| `--provider` | `anthropic` | `anthropic` or `openai` (any OpenAI-compatible backend) |
| `--model` | `claude-opus-4-8` | model name; must be one your `--provider` actually serves |
| `--cwd` | current directory | project directory the agent reads/edits/runs commands in |
| `--no-tools` | `false` | disable tool use |
| `--yolo` | `false` | pre-approve every tool call for this run |
| `--db` | `~/.local/share/hewn/hewn.db` | session database path |
| `--list` | `false` | list recent sessions and exit |
| `--resume[=<id-or-prefix>]` | | resume the most recent session, or a specific one |

Every run — TUI, headless, or interactive — is recorded to the session database; there is no flag to disable persistence.

## Context files

Every session's system prompt is assembled from `AGENTS.md` files, no flag needed. Hewn walks up from `--cwd` (or the current directory), collecting each directory's `AGENTS.md` along the way, and stops once it reaches the directory containing `.git` (that directory's `AGENTS.md` is included, then the walk stops). `~/.config/hewn/AGENTS.md`, if present, is prepended before all of those as a user-global default. The pieces are concatenated general-to-specific — user-global first, repo root next, down to the closest `AGENTS.md` to `--cwd` last — so project-specific instructions have the final say over general ones.

A missing file at any level is normal and silent. A file that exists but can't be read is reported as a warning on startup, not a fatal error.

## Skills

Skills are declarative Markdown+front-matter command bundles — a system prompt plus an optional allowed-tool subset, invoked as an ordinary slash command. They're only available in `--interactive` and TUI sessions, not headless (`-p`) runs.

Drop a file per skill in `.hewn/skills/` under your project:

```markdown
---
name: code-review
description: review a diff for correctness and style
tools: [read, bash]
---
You are reviewing a code change. Focus on correctness bugs first,
then style. Be concise.
```

`name` defaults to the filename (`.hewn/skills/code-review.md` → `/code-review`) if omitted. `tools` defaults to no restriction (every registered tool stays available) when omitted. Everything after the closing `---` is appended to the AGENTS.md-derived base system prompt (see "Context files" above) for the rest of the session, the same persist-until-changed behavior as `/model` — activating a skill doesn't discard your project's `AGENTS.md` conventions.

A skill file that's missing front matter, or lists a tool that doesn't exist, is skipped with a warning on startup rather than failing the session.
