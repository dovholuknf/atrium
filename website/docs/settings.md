---
title: Settings and operations
description: Settings, backups, starting at login, restarting, and what atrium does when storage fails.
---

# Settings and operations

## Where settings live

The gear in the header opens the board's settings: skins, text size, density, notifications and the switcher key.
Settings that belong to a machine sit behind that **room's cog**: the editor command, the paste directory, browse
roots, shells, scrollback and the like. Some settings are per browser, such as terminal font size and folded
groups.

## Back it up

**back it up** in the settings cog exports atrium's configuration. The API is `GET /v1/config/export` and
`POST /v1/config/import`, and an import is a dry run unless you say otherwise.

## Starting at login

The [install](./install.md) sets this up: a systemd user unit on Linux, a LaunchAgent on macOS, and a logon task on
Windows. All three run atrium as you, in your session, because that is the only way the agents it starts can use
your environment.

## Restarting

A restart of the room ends every supervised session, then brings back what was open, each resuming its
conversation. A session can restart atrium from inside atrium with the `restart_atrium` tool: other working agents
are parked first, and a detached restarter brings the room back as the same room.

A hub restart costs no session anything, because the hub holds nothing.

## Stopping

`atrium stop` winds a room down the way ctrl-c does: event streams released, supervised runners given ten
seconds, listeners closed in order. Killing the process closes every pseudo terminal at once and takes the runners
with it.

## When storage fails

Atrium refuses to start if its database will not open or migrate. If storage fails later, the agent port closes
and stays closed. Agents see connection refused and park on the backoff they already have, burning nothing. The
board stays up to say what broke. Running without durable state is worse than not running.

## One database

Atrium warns when it opens a different database than last time. Which database it opens depends on `--db`, or on
`WORKTREE_ROOT` in the shell it started from. The autostart pins one command line, so it opens the same one every
time.

## Finding the room

Hooks and scripts find the running room through an address file. `shared_location` names a directory both
accounts can read, so a script running as you can find a room running under another account.
