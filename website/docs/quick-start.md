---
title: Quick start
description: From an installed atrium to a gated session on the board in five steps.
---

# Quick start

This page assumes atrium is [installed](./install.md) and running. If you started it by hand, it is `atrium daemon`.

## 1. Open the board

Open `http://localhost:7778`.

Atrium listens on two ports on purpose. Agents talk to `:7777`. You talk to `:7778`. If storage ever fails, the
agent port closes and stays closed, so agents park and burn nothing, while the board stays up to tell you what
broke.

## 2. Wire the hooks

Atrium sees a session when its hooks report in. Open the **rooms** tab and find the claude runner's row. Its
**hooks** button says how many are missing. Press it, and atrium writes them into your Claude Code settings. It
keeps the hooks you already have, and it keeps symlinks as symlinks.

From then on, a claude session you start anywhere on this machine gets a card the moment it begins, with a live
badge that says what it is doing.

## 3. Add the permission gate

The gate is one more `PreToolUse` hook, and it is yours. The board does not write it, because it decides what runs
on your machine. It posts each tool call to atrium and blocks until you answer. [Hooks](./hooks.md#the-permission-gate)
has the contract and a starting script.

Two signals tell the hook which sessions to gate. `ATRIUM_PERM_GATE=on` gates every session on the machine.
Otherwise the hook asks atrium, and a session is gated from the moment it runs `atrium join`.

## 4. Import the rules you already trust

Open **perms** and press **import rules from claude**. Atrium previews what it would add before it adds anything.
It turns `Bash(go build:*)` into a prefix rule and `//c/temp/**` into a real folder, and it lists anything it
cannot map instead of dropping it.

On a working setup this starts you with a hundred rules or more, so the gate asks about what is new and nothing
else.

## 5. Start an agent

Two ways, and you can mix them. [Two ways to run it](./modes.md) explains the difference.

- **In your own terminal.** Start `claude` as you always do. The hooks put it on the board and gate its tool
  calls. Run `atrium join` inside a session that was already running to put it on the board without a restart.
- **In a terminal atrium owns.** Press **new agent** on the board, pick a directory and a runner, and launch. The
  session opens in the **terminals** tab, where you can type into it from this or any other browser.

From a script:

```bash
atrium launch --cwd ~/src/api --title fix-login --tags "api,bug" \
  --prompt "the login test fails on a fresh database, find out why"
```

## What to try next

- Answer a request with **always** and watch the next one go through on its own.
- Turn on **auto** for one card for an hour, then press **what did it do?** on that card.
- Press `ctrl-shift-k` and type a few letters of a card's name.
- Pop a terminal into its own window from its menu, and alt-tab to it.
