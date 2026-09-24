---
title: Cards
description: What a card holds, the live badge, seen and unanswered turns, and the card menu.
---

# Cards

A card is a place where work happens. It is not the agent. It survives the runner exiting, an atrium restart and
the end of the conversation. The runner's process id is only a hint for reconnecting.

## What a card holds

- A **title**, which you can rename. What you typed wins everywhere: the board, the terminal pane, the switcher and
  the window title.
- A **why**: what the card is for, still true next week.
- The **directory**, and the repository atrium works out from its path, such as `github/acme/api`. **which repo**
  on the card menu corrects it.
- **Tags**, free words you choose, used to group, filter and scope actions.
- An append-only **event log**, a queue of **messages**, and any open **questions**.
- A **priority**, and an icon of its own that appears on its desktop notifications.
- A **wire name**, the handle other sessions use to address it.

What a hook reports and what you typed are kept apart. Your values always win over observed ones.

## The live badge

Each card shows what its runner is doing right now: **thinking**, running a named tool such as `Bash`, or how many
subagents are working, and for how long.

This is the difference between "leave it alone" and "go look at it". "Running Bash for 40 minutes" is not
something a status column can tell you. The badge is never stored, because a stored activity would be wrong the
moment atrium restarted.

A statusline script can also post how much context a session has used and its account limits, and the card draws
them.

## A question or a finish

When a turn ends, atrium tells two cases apart:

- **The session asked you something** and cannot continue. The card says so and sorts above the others.
- **The session ran out of work.** It will sit there forever, costing nothing.

Claude Code's `Notification` hook fires only for the first case, which is how atrium knows.

## Seen, and unanswered questions

An **unread turn** wears a teal dot. A turn is seen when a focused window shows that card's terminal scrolled to
the bottom for three seconds, or when you type into it, submit a prompt or send it a message. A message from
another session never counts.

A turn that ended on **Open Questions** you have not answered wears `? N`, with the questions in its tooltip. A
reply answers them. Both survive a restart, and an agent can ask atrium whether you read its last turn.

## Liveness

A card stores its runner's process id, and whether that process still exists is a question the operating system
answers. It costs no turn and no token. A card with no known process is left alone, because not knowing is not
the same as being dead.

## The card menu

Right-click a card, or press its menu. Selecting text on a card never opens the menu, because copying a path or a
branch off a card is a daily thing.

- **new agent here** starts another runner in the card's directory, with nothing asked.
- **open a shell here** opens a shell on the card beside a wedged agent.
- **move it to** files the card in another column. **move it up or down** changes its rank.
- **into group**, **rename** and **which repo**.
- Auto mode, shelving, and **what did it do?**
- **share this session** lends it to one person. See [Overlays](./overlays.md#lend-one-session).
- **archive** takes it off the board and keeps it in history. Reviving it brings it back, and resuming falls back
  to a fresh start when the conversation is gone.

## A machine that is not answering

Cards from a room that is offline are drawn from what it last reported, in one folded group at the bottom of every
column. Attach, resume and start are not offered, and nothing is queued for its return.
