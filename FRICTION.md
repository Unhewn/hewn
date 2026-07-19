# FRICTION.md

A running log of every moment using Hewn was worse than using another agent harness.

**This file is the real roadmap.** The phase list in `HEWN.md` is what I guessed I'd need before writing any code. This is what I actually needed, discovered by using the thing. Where they disagree, this file wins.

---

## Rules

1. **Write the entry during the session, not after.** Ten seconds, one line. Stopping to think about the fix defeats the purpose.
2. **Describe the friction, not the feature.** "Lost 4k tokens re-explaining the tool registry after a crash" — not "add session resume." The symptom is the data; the fix is a later inference.
3. **Bump the tally on repeats.** Do not write a new entry. Frequency is the signal — a small annoyance hitting fifteen times outranks a large one hitting twice.
4. **Log it even when the fix is obvious.** Especially then. Obvious fixes are how you find out what order to build in.
5. **Log when you reached for Claude Code instead.** That's the highest-value entry in the file. Note what the task was and why Hewn wasn't up to it.
6. **Never delete an entry.** Move it to Resolved with the commit that fixed it.

---

## Open

| # | Friction | Hits | First seen | Suspected fix | Surface |
|---|----------|------|------------|---------------|---------|
| 5 | No real terminal available in the dev environment used to build the TUI -- a sandboxed/piped shell never sends a `tea.WindowSizeMsg` at all, so any captured output renders at effectively zero width and isn't representative of real usage. | 1 | 2026-07-18 | No real fix available in that environment; verify layout via unit tests that send a synthetic `WindowSizeMsg` instead of trying to screenshot. | core |

**Surface** maps to the extension surfaces in `HEWN.md` §6 (A tools · B commands · C skills · D hooks · E UI · F providers), or `core` if it belongs in the harness itself. Leave blank if unclear — the pattern usually emerges after a dozen entries, and that pattern is what finally decides the extension mechanism.

---

## Resolved

| # | Friction | Hits | Fixed in | Notes |
|---|----------|------|----------|-------|
| 1 | `agent.Loop.MaxTokens` was never set in `cmd/hewn/main.go`'s `buildLoop`, silently defaulting to 0. Both providers bump a zero `MaxTokens` to their own internal fallback before sending (Anthropic: 4096; OpenAI-compatible: already separately bumped to 16384 in an uncommitted fix, reasoning-capable local models burn a large chunk of budget on hidden reasoning before any visible output) -- every response was capped well below what a real coding turn needs, read as "the model can't handle much." | 1 | 2026-07-18 (uncommitted) | Added `MaxTokens` to `config.Config`, defaulting to 16384 to match the reasoning-aware figure regardless of provider; threaded through `buildLoop` into `agent.Loop.MaxTokens`. |
| 2 | lipgloss `Style.Width` sets *total content+padding* width, not pure content width (padding is subtracted from it when wrapping, then re-added before aligning) -- got this backwards when adding an outer bordered frame with its own `Padding(0,1)`, which silently wrapped the input box's own border onto an extra line. | 1 | 2026-07-18 (uncommitted) | `frame.Width(cw + 2)` accounts for the frame's own padding. Documented inline in `internal/tui/app.go`; `TestView_FramedOutputFillsExactlyToHeight` added and verified to actually catch the regression (reverted the fix, watched it fail, restored it). |
| 3 | A colored `▏` left-bar indent on user/system/tool-call transcript lines, with assistant lines getting a bare space instead, read as an inconsistent/misaligned left edge rather than a helpful color signal. | 1 | 2026-07-18 (uncommitted) | Removed (`barFor` deleted from `internal/tui/chat.go`); back to a plain uniform indent for every item kind. |
| 4 | The status bar showed `loop.TotalUsage()` (cumulative tokens across the whole session) where "how much is in context right now" was the actually useful number -- actively misleading, not just imprecise. | 1 | 2026-07-18 (uncommitted) | Added `Loop.lastUsage`, tracking only the most recent turn's usage, exposed via `Loop.LastUsage()`; kept `TotalUsage()` for the separate cumulative `Σ` figure now also shown in the status bar. |

---

## Reached for another tool instead

Log every time. Be specific about what Hewn couldn't do.

| Date | Task | Used | Why not Hewn |
|------|------|------|--------------|
| | | | |

---

## Review

Read this file at the end of every week before touching `HEWN.md`.

- Anything at **5+ hits** goes to the top of the backlog regardless of what the phase list says.
- Three or more entries pointing at the same **surface** means that surface needs a real extension mechanism, not another built-in.
- If the "reached for another tool" table is still growing after a month, the gap is structural — figure out what it is before adding more features.
- If an entry has sat open for three weeks at 1 hit, delete the urgency, not the entry. It wasn't real friction.
