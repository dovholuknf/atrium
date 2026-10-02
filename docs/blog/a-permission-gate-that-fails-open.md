# A permission gate in the binary, and why it fails open

Series: The permission chain and auto mode. Status: idea. Audience: people writing Claude Code hooks.

**Hook.** From a PowerShell script in the operator's dotfiles to `atrium hook --event permission`, with a 24-hour timeout and a deliberate fail-open.

**Angle.** Where the gate lives and what it does when atrium is down are the two design choices that matter.

**Rests on:** permission gate, machine-wide gating, codex as a second target. See `docs/blog/inventory.md`.

## Story beats

1. Every PreToolUse call blocks until approved or blocked; the reason goes back to the agent.
2. Why fail open: an agent machine with atrium down should not freeze every session.
3. Moving the gate from a dotfiles script into the binary, and rewriting the old hooks row in place.
4. Writing missing hooks while preserving siblings and symlinks.
5. Codex as a second target.

## Screenshots and demos

- the pending edit with a real diff
- the hooks row before and after

## Sources

- docs/runtime/hooks.md
- changelog/runtime/2026-09-29-r-023.md
- changelog/fabric/2026-09-30-f-006-replace.md
- README.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
