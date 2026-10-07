---
title: Quick start
description: From an installed atrium to a card on the board, then to your first gated request.
---

# Quick start

This page assumes atrium is [installed](./install.md) and running. If you started it by hand, it is `atrium run`,
or `atrium run --no-room` with the room running as `localai`, [the setup atrium recommends](./accounts.md). In that
setup, "your Claude Code settings" and "your own terminal" below mean `localai`'s.

The first three steps put sessions on the board. Steps 4 and 5 add the permission gate, which is optional.

## 1. Open the board

Open `http://localhost:7778`.

The board you are looking at is the hub's, on `:7778`. The agents and their hooks talk to the room, on `:7777`.
They are separate on purpose. If the room's storage ever fails, it closes `:7777` and leaves it closed, while the
board stays up to tell you what broke. Hooks treat a closed port as "atrium is not there": sessions keep working,
and a gated tool call falls back to Claude Code's own permission prompt. If the hub is down, the room still serves
its own board on `:7781`.

## 2. Wire the reporting hooks

Open the **rooms** tab and find the claude runner's row. Its **hooks** button says how many are missing. Press it,
and atrium writes them into your Claude Code settings. It keeps the hooks you already have, and it keeps symlinks
as symlinks. `atrium hook install` does the same from a shell.

From then on, a claude session that **starts** on this machine gets a card the moment it begins, with a live badge
that says what it is doing. Sessions already running keep their old settings until they restart.

## 3. Start an agent

Two ways, and you can mix them. [Two ways to run it](./modes.md) explains the difference.

- **In a terminal atrium owns.** Press **+ new agent** on the board, pick a directory and a runner, and launch. The
  session opens in the **terminals** tab, where you can type into it from this or any other browser.
- **In your own terminal.** Start `claude` as you always do. It appears on the **stack** and the **board**.

From a script:

```bash
atrium launch --cwd ~/src/api --title fix-login --tags "api,bug" \
  --prompt "the login test fails on a fresh database, find out why"
```

You should now see a card, and its badge change between **thinking** and the tools it runs.

## 4. Add the permission gate (optional)

The gate is one more `PreToolUse` hook, and it is yours. The board does not write it, because it decides what runs
on your machine. [Hooks](./hooks.md#the-permission-gate) has the contract, a starting script and the settings
entry to register it.

Then choose which sessions to gate:

- `ATRIUM_PERM_GATE=on` in a session's environment gates it from the start. Put it in a runner's environment on
  the **rooms** tab to gate every session that runner launches.
- `atrium join`, run inside a session, gates that session from its next tool call. `atrium leave` stops.

To see it work, start a new gated session and ask it to run a harmless command, such as listing a directory. The
card moves to **needs permission**, and the request appears in the **perms** tab and as a notification. Press
**block**, type "use the other directory", and watch the agent read your reason and change course.

## 5. Import the rules you already trust

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
