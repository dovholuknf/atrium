---
title: What atrium is
description: One board for every coding agent session you run, with a gate on their tool calls.
slug: /intro
---

# What atrium is

Atrium puts every coding agent session you run on one board. It tells you which one needs you, it can gate the
tool calls those agents make, and it can hold their terminals so you reach any of them from a browser.

It is self-hosted, for one operator. There are no accounts, no tenants and no hosted service: atrium runs on your
machines, and your agents talk to their model providers exactly as they did before. The board listens on
loopback, and reaching it from somewhere else is a job for an overlay such as zrok or OpenZiti.

## What works out of the box, and what you add

| Capability | What it needs |
| --- | --- |
| Cards, live activity, history | The reporting hooks. The board writes them for claude and codex with one button. |
| Supervised terminals in the browser | Nothing more. Launch from the board. Any runner works, including a shell. |
| Messages to a running session | Supervised terminals, or the reporting hooks. |
| The permission gate, rules, auto mode | A `PreToolUse` gate hook you add. See [Hooks](./hooks.md#the-permission-gate). |
| One board over many machines or accounts | A room on each machine, joined to one hub. See [Rooms](./rooms.md). |

## The name

A Roman house was built around one open room. Every other room had a door onto it. The roof was open above it, so
it was the only room with its own light. Anyone crossing the house crossed it. You stood in the atrium and saw the
whole household at once.

That is the shape of the problem atrium solves. Half a dozen agents are running, each in its own directory, each
with its own conversation, none aware of the others. Without somewhere to stand, you alt-tab between terminals and
ask the same four questions:

1. Which one needs me?
2. How long has it been waiting?
3. What was I doing in that one?
4. Is it still alive?

Atrium is the room you stand in. The agents keep their own rooms. Nothing moves between them except through the
hall.

## Three rules that follow from the name

**You stand in the hall, not in a room.** The board is not a terminal multiplexer. It answers what needs you, and
it gets out of the way when nothing does.

**A room outlives whoever is in it.** A card is not an agent. It is a place where work happens. It survives the
process, a restart and the conversation.

**Rooms do not connect to each other.** When one session talks to another, the message goes through the hall and
waits in a queue. Atrium never types into a line somebody is writing.

## What you get

- A [board](./board.md) of cards in columns that are buckets of your attention: needs permission, ready, running,
  finished and shelved.
- A [permission gate](./permissions.md) on gated sessions' tool calls, with the real diff for an edit, standing
  rules, and an auto mode that still keeps the record.
- [Supervised terminals](./terminals.md): claude, codex, gemini, ollama or a shell, under a pseudo terminal atrium
  owns, attached from any browser.
- [Messages](./messages.md) you queue to a running session, and a way for sessions to ask each other.
- [Rooms](./rooms.md): one board over many machines, or over several accounts on one machine.
- [Overlays](./overlays.md) to reach the board from elsewhere, or to lend one session to one person.
- A [history](./history.md) of every card and every decision, and notifications that carry approve and block.

## Where to go next

- New here? Read [Install](./install.md), then the [Quick start](./quick-start.md).
- Want to keep your own terminal? Read [Two ways to run it](./modes.md).
- Curious why it looks the way it does? Read [Why atrium exists](./story.md).
