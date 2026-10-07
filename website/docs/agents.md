---
title: For people who run coding agents
description: An agnostic harness for the agents you already run. Claude, codex, gemini, opencode, ollama or a shell.
---

# An agnostic harness for the agents you already run

You already have the agents. Claude Code, codex, gemini, opencode, a model on ollama, a shell script that loops. What
you lack is somewhere to stand while six of them run at once. Atrium is that place, and it does not care which agents
they are.

## A runner is a row, not an assumption

Every agent atrium starts comes from a row on the **rooms** tab: a command, its arguments, a working directory, an
environment, and how it resumes. Claude, codex, gemini, opencode, ollama and a plain shell are rows like any other.
Add your own the same way. Nothing in atrium is written around one vendor's agent, so the day you switch, you add a
row and keep your board, your rules and your history. [Runners](./runners.md).

What a runner gets depends on what it offers, never on a list atrium keeps of favourites:

| The runner has | Atrium gives it |
| --- | --- |
| a terminal | a card, a supervised terminal you reach from any browser, messages typed in when your line is free |
| hooks | a live badge, a card the moment the session starts, and the permission gate on every tool call |
| a resume id | the same conversation back after a restart, on the same card |
| none of the above | still a card and a terminal, and whatever you type on the card |

Claude and codex hooks are wired with one button. For any other runner, `docs/runtime/wiring-a-runner.md` in the
repository is the brief an agent follows to measure what that runner's hooks report.

## What you get, whichever agent it is

- **One board.** Every session is a card in a column that says whether it needs you. Which one needs me, how long
  has it waited, what was I doing in it, is it still alive. [The board](./board.md).
- **Real terminals.** Atrium owns the pseudo terminal, so you type into any session from any browser, a phone
  included, and pop it into its own window. [Supervised terminals](./terminals.md).
- **A gate on every tool call.** Answer once with **always** or **never**, set folder rules, or turn on auto mode for
  an hour and read back what it did. [Permissions](./permissions.md).
- **Agents that talk to each other.** A session finds another by handle and says something, on this machine or
  another. A session can launch a colleague on its own card. [Messages](./messages.md),
  [the control MCP server](./control-mcp.md).
- **State that survives.** Cards, decisions and history live in a SQLite file per machine. Restart atrium and the
  board is where you left it.
- **Many machines, one board.** Each machine runs a room, and every room dials one hub. Agents under their own
  account are a room too. [Rooms and the hub](./rooms.md).
- **Your keys stay yours.** Each runner signs in to its own provider as it always did. Atrium never holds a model key.

## What atrium is not

It is not an agent and not a framework. It writes no prompts for you and runs no model of its own. It does not
replace the agent's own tools, only watches and gates them. It is self-hosted for one operator, with no accounts and
no hosted service.

If you want to see how it compares with the other tools in this space, read [How atrium differs](./differs.md).
