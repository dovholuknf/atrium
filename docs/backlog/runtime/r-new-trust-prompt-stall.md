# r-new-trust-prompt-stall: a launch that sits at Claude Code's "trust this folder" prompt

Status: parked (2026-09-30). @runtime.

## Why

`sg3-profile` was launched on sg3 with `cwd` `C:/Users/claude`, a folder Claude Code had not been told to trust.
It sat at the trust prompt for 45 minutes, with the brief never read, and the card looked like it was running.
clint found it by opening the terminal.

## Wanted

- The screen model already sees dialogs (`dialogOpen`). Recognise the trust prompt on a card that has not had a
  first tool call, and file the card as needing the human, with the reason "Claude Code asks whether to trust
  <folder>", not as running.
- For a launch the orchestrator or a director made (an `origin:agent` card), offer the answer on the card: trust
  this folder once. Never answer it automatically: trusting a folder lets its settings and hooks run.
- `atrium_launch` warns at launch time when `cwd` is a folder that runner has never been started in on that room.
