---
title: The board
description: The stack, the board and the terminals list, and the tools for moving between sessions.
---

# The board

The board answers three questions: what is running, which session needs you most, and what you were doing in each
one. It is a plain page served by atrium, and it speaks the same JSON and event-stream API as any other client.

## The views

The header holds the views. The number on a tab counts what is waiting there.

| View | What it is for |
| --- | --- |
| **stack** | Everything waiting on you, oldest first. It answers "who has been blocked longest". |
| **board** | Every card in columns by status, grouped and sorted the way you choose. |
| **terminals** | The supervised sessions, grouped by host, org and repo, with the attached terminal beside them. |
| **history** | Every card ever created, searchable, exportable as JSON or CSV. |
| **rooms** | Rooms, runners, fixtures, sources, providers and actions: how atrium is set up. |
| **perms** | Pending requests, standing rules, and the decision log. |

The **stack** and the **board** are where atrium started. When your sessions run in your own terminals, they are
where you live. When atrium holds the terminals, **terminals** is. See [Two ways to run it](./modes.md).

## Columns

The columns are buckets of your attention. A card sits in one because you must act, or because you decided
something.

| Column | Means |
| --- | --- |
| **needs permission** | A tool call is waiting for your answer. |
| **ready** | The turn ended. A card that asked you a question sorts above one that ran out of work. |
| **running** | The session is working. |
| **finished** | The session ended, the agent said `atrium finish`, or you declared the work done. |
| **shelved** | You put the work down. Its requests are answered no. |
| **inbox** | Work that arrived from a [source](./intake.md) and has not started. It appears only when it holds something. |

An empty column gives its width back to the others. A column that grows tall folds away. You cannot move a card
into needs permission or ready yourself, because only an agent puts one there.

## Grouping and sorting

- **Group** by project, by tag, by your own groups, or not at all. Group headings stay alphabetical, so the list
  does not rearrange itself while you read it.
- **Sort** by activity, name or manual order from the pill in the board's bar. The sort applies inside each group
  and never reorders the groups.
- **Your own groups.** Under `by group`, add a named group, then use **into group** on a card's menu to file it
  there. A card can sit in more than one group.
- **Collapse** a group and it stays collapsed, even when a card moves between columns.
- **Pin** a card to keep it at the top of its column and of the switcher. Pinned cards keep the order you set with
  **move up** and **move down**, whatever the sort says.

## The switcher

Press `ctrl-shift-k` over anything on screen, type a few letters of the title, directory or tags, and press Enter.
The sessions you visited last come first, so moving between two sessions is one key and one more.

Inside a popped-out terminal window, the switcher moves that window to another card instead of opening a second
one. The key is a setting, because browsers keep different keys for themselves.

## Skins

Pick one of twenty skins in the settings cog, dark and light. The skin follows the room picker: **ALL** has one,
and each room can have its own, so you can tell at a glance which machine you are looking at.

You can also draw every card in its own terminal's colours, so you find a session by colour. It is off by default,
under **board cards wear their terminal colours**. Every title and chip stays at 4.5:1 contrast against its card.

## On a phone

The board works on a phone. The header holds its shape, tap targets are 40 pixels, the terminals list collapses to
a dropdown, and you drag the split between the list and the terminal.

## Small things that save time

- Clicking outside a dialog closes it, unless the dialog holds unsaved edits.
- The board keeps your scroll position and your place across repaints, and browser back and forward move between
  views.
- A new build of the board reloads open pages on its own.
- The tab wears the atrium A, drawn by the same code that draws the notification mark.
