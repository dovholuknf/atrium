## Test plan

## @LETTER@. An agent sets atrium up from docs/try-atrium.md

### @LETTER@1. A clean machine

1. On a machine with git and Go but no atrium, no `~/.atrium` and no atrium hooks, start a fresh coding agent.
2. Paste the fenced `text` block from `docs/try-atrium.md` and let it run.

**Expected:** the agent builds into `build.claude/`, installs to `~/.atrium/bin`, starts `atrium run`, backs up
`settings.json` before `atrium hook install`, shows the diff, and reports each step as passed or failed. A card
appears for a new Claude session, and a launched agent's terminal opens in the browser.

### @LETTER@2. Undo

1. Follow the prompt's undo note: restore the `settings.json` backup, run `atrium stop`, delete `~/.atrium/bin`.

**Expected:** `atrium hook status` reports no hook wired and nothing listens on 7778.
