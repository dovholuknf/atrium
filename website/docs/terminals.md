---
title: Supervised terminals
description: Agents under a pseudo terminal atrium owns, attached from any browser.
---

# Supervised terminals

When atrium launches a runner, it runs it under a pseudo terminal it owns and keeps its recent output. The browser
attaches over a websocket. You read the session, type into it, stop it and restart it from any browser that can
reach the board.

## The terminals list

The **terminals** tab lists every supervised session, grouped by host, org and repo. Every level keeps its own
heading and a guide line down its left edge, and groups fold. Sessions whose directory names no forge or org sit
under **uncategorized** at the bottom.

- **Pinned** is a bucket you arrange by drag. A pinned row stays after its session exits and is drawn cold. Click
  it to start it again on the same card. The order is stored on the card, so it follows you to another browser.
- **hide inactive** has two halves. **agents** hides rows with no live connection. **subagents** hides subagents
  that are not working now. A heading reads `5/15` while something is hidden.
- The controls fold into one summary line, such as `sorted by activity · by project · hiding inactive subagents
  (2)`.
- A row pulses while it holds a message from another session, and wears a badge in its room's colour.

## Attaching

Click a row to attach. The terminal keeps a deep scrollback, replayed through a screen model so that output drawn
with cursor movement comes back as it looked. After an atrium restart, the saved history is joined in front of the
new session's output so older turns show once. Search it from the terminal's cog.

**A narrow window does not shrink a shared session.** The terminal's width follows the widest attached viewer and
its height the shortest, and a narrower window scrolls sideways. A claude terminal never goes narrower than 120
columns, the `terminal_min_cols` setting. A shell is exempt.

**Paths and URLs are links.** A path that is a real file in the card's directory opens in atrium's editor in the
browser you are sitting at. A directory opens the file drawer. A URL opens a new tab. `internal/api/api.go:248:1`
opens the file without the line number. Selecting text never opens anything.

**A paste arrives as one paste.** Runners that understand bracketed paste get it, however long the session has run.

## Its own window

Pop a terminal into its own window from its menu. Alt-tab beats a click into an app and another onto a tab. The
window is titled with the session's whole address, marked in the title bar when that session wants you, and
closes itself when the runner exits. It rides out a hub restart and reconnects.

## Atrium never types into your line

Atrium types things into supervised sessions: messages you queue, messages from other sessions, action prompts.
Every one of those writes waits until your input line is empty and the keyboard has been quiet for two seconds. A
held write is queued and retried. Atrium also refuses to type into a permission dialog it did not raise.

## Restarting and stopping

- **restart** in the terminal cog ends the session and resumes its conversation on the same card, so it picks up
  new defaults without losing context.
- **stop** walks ctrl-c, then `exit`, then closing the terminal, then a kill, and says which step worked.
- **open a shell here** opens a shell on the card, beside an agent that is stuck.

:::warning By default a supervised runner lives in the room's process
On Windows, closing a pseudo terminal ends the process attached to it, so a room restart ends every supervised
session at once, unless the `pty_host` setting is on. Then the terminals live in a separate pty host and the
runners keep running through the restart. Without it, atrium records each session's resume id, and a restart
reopens what was open, resuming each conversation on a new terminal. Alerts stay quiet until every card named has
come back, and permission requests still ring.

This is also why `atrium stop` exists. It winds down in order and gives runners ten seconds. Killing the process
ends every agent at once.
:::

## Themes and settings

Each terminal has its own theme, chosen per card, and you can import terminal themes. Agent-launched workers
usually wear `active-work`, the green on this page's examples, so they stand out. Font size and other terminal
settings are kept per browser, and text scales on its own.

## Keeping keystrokes smooth

On a busy Windows machine the hub and the room run at above-normal priority, so typing does not stutter.
`ATRIUM_PRIORITY=normal` turns that off. If typing still lags, one checkbox, **log terminal input lag**, times the
whole path from the browser through the hub to the room, live, with no restart.

A terminal that draws something wrong can hand over the bytes that drew it: **save a terminal trace** in its cog
keeps the last 64KB of raw traffic, and `atrium replay` renders a captured stream the way an attach would.
