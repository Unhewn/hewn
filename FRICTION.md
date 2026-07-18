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
| | | | | | |

**Surface** maps to the extension surfaces in `HEWN.md` §6 (A tools · B commands · C skills · D hooks · E UI · F providers), or `core` if it belongs in the harness itself. Leave blank if unclear — the pattern usually emerges after a dozen entries, and that pattern is what finally decides the extension mechanism.

---

## Resolved

| # | Friction | Hits | Fixed in | Notes |
|---|----------|------|----------|-------|
| | | | | |

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
