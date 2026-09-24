---
title: Runners
description: claude, codex, gemini, ollama, a shell, or anything you add. A runner is configuration, not code.
---

# Runners

A runner is a row on the **rooms** tab: a command, arguments, a working directory, an environment, a way to
resume, a way to take a model, and the keys that make it exit. Adding one is configuration, not a code change.
Nothing about any particular runner is special-cased.

Seeded rows cover claude, codex, ollama and a shell. A runner whose command is not on the machine is left out of
the launch menus.

## Launching

Start a runner from any of these:

- **+ new agent** on the board: pick a room, a directory and a runner.
- **new agent here** on a card: a runner in that card's directory, with nothing asked.
- **atrium launch** from a script. See the [CLI](./cli.md).
- **atrium_launch** from another session. See the [control MCP server](./control-mcp.md).
- **A URL**: paste an issue or pull request link and a recogniser fills in the launch fields. `atrium open <url>`
  does the same from a shell.

### A model, once

A launch can name a model. The card remembers it across restarts, and the runner's row does not change, so it is
one time for the runner and sticky for the card. A runner declares how it takes a model with `model_args`. A
runner with none refuses a launch that asks for one, rather than quietly starting on the default.

### Resuming

Pick which claude conversation a card resumes. Atrium refuses to start a second runner on a conversation another
one already holds. When the conversation is gone, resuming falls back to a fresh start.

### Throwaway sessions

Tick **throw it away when the session ends**, and the session runs in a directory that deletes itself along with
its card and conversation. **keep this work** moves the directory somewhere real and makes the card permanent.

## Fixtures

A fixture is a session atrium keeps running: when atrium starts, it starts the fixture. Switch each one on or off
from its pill.

## Setup checks

A runner's row says what stops it working. The **setup** chip opens named checks with **fix** buttons, or a command
to copy when atrium may not fix it itself. Claude checks sign-in and hooks. Gemini checks folder trust and
sign-in, and a gemini card in a new folder is trusted before it starts. Every edit keeps a backup.

Atrium also checks for a newer version of a runner just before it starts one, by reading the installed package and
asking the registry once. It never runs the runner to find out.

## Providers

A provider tells atrium where your repositories live: a name, a root and a layout, such as
`<root>/<forge>/<org>/<repo>`. Defining one adopts every checkout under it, and the launch form offers them. Whether
a checkout is present is worked out fresh each time, so an unplugged drive greys rows and deletes nothing. A
provider can make a worktree for you, with `git worktree add` and no shell.

## The environment atrium gives a runner

Atrium scrubs the markers of the session it was started from before it launches anything. Passing them on would
make a launched runner believe it was a child session. Every launched runner gets `ATRIUM_AGENT_NAME`,
`ATRIUM_TASK_ID` and `ATRIUM_ROOM`, so its hooks and tools find their card.
