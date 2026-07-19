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

With no flags and no config file, this opens the TUI using Anthropic's API, so `ANTHROPIC_API_KEY` must be set:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
hewn
```

### Modes

```bash
hewn                       # TUI (no -p, no --interactive)
hewn -p "prompt"           # headless: run one prompt and exit
hewn --interactive         # REPL: slash commands + turns, no TUI
hewn --list                # list recent sessions and exit
hewn --resume              # resume the most recent session
hewn --resume=<id-or-prefix>  # resume a specific session (the = is required)
```

### Config file

Default settings can be saved to `~/.config/hewn/config.yaml` so you don't
have to pass `--provider` and `--model` every time:

```yaml
provider: openai
model: gemma4:12b
```

Project-level overrides go in `.hewn/config.yaml` under your project
directory and take precedence over the user-level config. CLI flags still
win over everything.

Environment variable credentials (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`OPENAI_BASE_URL`) are never stored in config files.

### Using a local model (Ollama, llama.cpp, LM Studio)

Hewn's `openai` provider speaks the OpenAI-compatible Chat Completions
format that Ollama, llama.cpp's server, LM Studio, and similar local
backends all expose. To use it:

```bash
# One-off (no config file):
OPENAI_API_KEY=not-needed hewn --provider openai --model gemma4:12b

# Or with a persistent config:
# ~/.config/hewn/config.yaml:
#   provider: openai
#   model: gemma4:12b
OPENAI_API_KEY=not-needed hewn
```

`OPENAI_BASE_URL` defaults to `http://localhost:11434/v1` — Ollama's
OpenAI-compatible endpoint. If your local backend runs on a different port
or address, set the env var:

```bash
export OPENAI_BASE_URL=http://localhost:8080/v1
```

`OPENAI_API_KEY` must be set to something non-empty (Hewn checks for it),
but Ollama and most local backends don't validate the value. Set it to
any string:

```bash
export OPENAI_API_KEY=not-needed
hewn
```

#### Starting the TUI with a local model

Just run `hewn` with no `-p` flag. With a config file pointing at
`openai`/`gemma4:12b`, the TUI opens immediately:

```bash
OPENAI_API_KEY=not-needed hewn
```

With no config, pass the flags explicitly:

```bash
OPENAI_API_KEY=not-needed hewn --provider openai --model gemma4:12b
```

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `-p, --prompt` | | run one prompt headless and exit |
| `--interactive` | `false` | run an interactive REPL (`/help` for slash commands) |
| `--provider` | *(config > defaults)* | `anthropic` or `openai` (any OpenAI-compatible backend) |
| `--model` | *(config > defaults)* | model name; must be one your `--provider` actually serves |
| `--cwd` | current directory | project directory the agent reads/edits/runs commands in |
| `--no-tools` | `false` | disable tool use, including any configured MCP servers |
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

## MCP servers

Hewn connects to [MCP](https://modelcontextprotocol.io) servers declared in `.hewn/mcp.json`, in every mode (headless, interactive, and TUI) — unlike skills, these are real capabilities, not a persona switch:

```json
{
  "mcpServers": {
    "example": {
      "command": "npx",
      "args": ["-y", "@some/mcp-server"],
      "env": { "API_KEY": "..." }
    }
  }
}
```

The shape matches Claude Desktop's config, so an existing `mcpServers` block can usually be copied in directly. Each server's tools appear to the model as `mcp__<server>__<tool>`, and are **always** approval-gated (`mutating` risk) — Hewn can't verify what a third-party server's tool actually does, so there's no auto-read-only path for MCP tools the way there is for its own `read` tool.

A server that fails to start, or times out (15s) connecting, is skipped with a warning on startup; the rest of the session proceeds normally.
