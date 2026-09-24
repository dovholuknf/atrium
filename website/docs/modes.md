---
title: Two ways to run it
description: Watch and gate sessions in your own terminal, or let atrium supervise the terminal itself.
---

# Two ways to run it

Atrium can watch sessions that run in your own terminal, or it can own the terminal itself. Both give you a card,
the gate and the history. They differ in what atrium can do to the session.

| | Watch and gate | Supervised |
| --- | --- | --- |
| Where the session runs | Your terminal, your window manager | A pseudo terminal atrium owns |
| How it reaches the board | Its hooks report in, or `atrium join` | Launched from atrium |
| Card, activity badge, history | Yes | Yes |
| Permission gate, rules, auto mode | With the [gate hook](./hooks.md#the-permission-gate) | With the gate hook |
| Where you mostly look | **stack** and **board** | **terminals** |
| Type into it from a browser | No | Yes, from any browser |
| Pop out into its own window | No | Yes |
| A message you queue arrives | On its next tool call or turn end | Typed in when your line is free |
| Restart onto the same card | No | Yes, from the terminal cog |
| Survives an atrium restart | Yes, atrium was never holding it | Resumes its conversation on a new terminal |

## Watch and gate

This is where atrium started, and it is the lighter mode. Start `claude` wherever you like: a terminal tab, an IDE,
over ssh. Its hooks report to atrium, so it gets a card, a live badge and a place in the history. With the gate
hook added, and the session gated, every tool call it makes goes through the gate.

Atrium never touches the terminal. Nothing about how you work changes, and an atrium restart does not affect the
session at all. If atrium is down, the permission hook fails open and the session carries on under Claude Code's
own permission flow.

The views built for this mode are:

- **stack**, everything waiting on you, oldest first. It answers "who has been blocked longest".
- **board**, every card in columns by status, grouped by project, tag or your own groups.

Use this mode when you do not want atrium as your terminal: when you live in an IDE, when a session belongs to a
tool that starts it for you, or when you only want the gate and the record.

To put a running session on the board without restarting it:

```bash
atrium join
```

`atrium leave` takes it off again.

## Supervised

Atrium launches the runner under a pseudo terminal it owns and keeps its recent output. The browser attaches over a
websocket, so you read and type into the session from the **terminals** tab, from another browser, from a phone or
from another machine over an overlay.

Owning the terminal lets atrium do things it cannot do for a session in your terminal:

- **Type a queued message in** when your line is empty and the keyboard has been quiet for two seconds.
- **Restart the session onto the same card**, resuming its conversation, so it picks up new defaults.
- **Pop it out** into a window of its own that you alt-tab to, titled with the session's address.
- **Open a shell** beside a wedged agent on the same card.
- **Stop it** by walking ctrl-c, then `exit`, then closing the terminal, then a kill, and saying which step worked.

The cost is the terminal's lifetime. A supervised runner lives in atrium's process, so a room restart ends it.
Atrium records each session's resume id and brings it back on a new terminal with its conversation intact.

## Mixing them

Most days are a mix: a handful of supervised workers in the **terminals** tab, and a session or two in your own
editor that shows up on the **stack** when it needs you. The two share one board, one gate and one history.
