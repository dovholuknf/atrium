---
title: Quick start
description: From your first card to your first gated request.
---

# Quick start

This page picks up where [Get started](./get-started.md) leaves off: atrium is running, Claude Code's hooks are
wired, and a card is on the board. If you run the room as `localai`, [the setup atrium recommends](./accounts.md),
"your Claude Code settings" and "your own terminal" below mean `localai`'s.

## 1. The board

Open `http://127.0.0.1:7778`.

The board you are looking at is the hub's, on `:7778`. The agents and their hooks talk to the room, on `:7777`.
They are separate on purpose. If the room's storage ever fails, it closes `:7777` and leaves it closed, while the
board stays up to tell you what broke. Hooks treat a closed port as "atrium is not there": sessions keep working,
and a gated tool call falls back to Claude Code's own permission prompt. If the hub is down, the room still serves
its own board on `:7781`.

## 2. Start an agent

Two ways, and you can mix them. [Two ways to run it](./modes.md) explains the difference.

- **In a terminal atrium owns.** Press **+ new agent** on the board, pick a directory and a runner, and launch, or
  run `atrium launch` in the directory. The session opens in the **terminals** tab, where you can type into it from
  this or any other browser.
- **In your own terminal.** Start `claude` as you always do. It appears on the **stack** and the **board**.

From a script:

```bash
atrium launch --cwd ~/src/api --title fix-login --tags "api,bug" \
  --prompt "the login test fails on a fresh database, find out why"
```

You should now see a card, and its badge change between **thinking** and the tools it runs.

## 3. Add the permission gate (optional)

The gate is one more `PreToolUse` hook. `atrium hook install` and the board's install all write it with the others,
because a room without it never puts a question on the board. To leave it out, run `atrium hook install --watch-only`,
which writes every hook but the gate, or remove its entry from `settings.json`.
[Hooks](./hooks.md#the-permission-gate) has the contract and the settings entry.

Which sessions it gates:

- Every session atrium launches is gated from the start. `ATRIUM_PERM_GATE=off` in its runner's environment keeps
  that runner's sessions ungated.
- A session you started yourself is gated when `ATRIUM_PERM_GATE=on` is in its environment.
- `atrium join`, run inside a session, gates that session from its next tool call. `atrium leave` stops.

To see it work, start a new gated session and ask it to run a harmless command, such as listing a directory. The
card moves to **needs permission**, and the request appears in the **perms** tab and as a notification. Press
**block**, type "use the other directory", and watch the agent read your reason and change course.

## 4. Import the rules you already trust

Open **perms** and press **import rules from claude**. Atrium previews what it would add before it adds anything.
It turns `Bash(go build:*)` into a prefix rule and `//c/temp/**` into a real folder, and it lists anything it
cannot map instead of dropping it.

On a working setup this starts you with a hundred rules or more, so the gate asks about what is new and nothing
else.

## What to try next

- Answer a request with **always** and watch the next one go through on its own.
- Turn on **auto** for one card for an hour, then press **what did it do?** on that card.
- Press `ctrl-shift-k` and type a few letters of a card's name.
- Pop a terminal into its own window from its menu, and alt-tab to it.
