---
title: History and notifications
description: The record of every card and decision, the audit tab, and alerts that reach you.
---

# History and notifications

## History

Every card has an append-only event log. Every card ever created stays searchable in the **history** tab, whether
or not it is still on the board, cut by its recap. Filter it, and export it as JSON or CSV.

Every permission decision records which agent asked and who answered: you, the rule that matched, auto mode, or
board-wide auto. The decision log sits in the **perms** tab.

The **audit** tab shows hub and room events: rooms attaching and dropping, launches refused by the session cap,
permissions, and sessions starting and exiting.

### Keeping the database small

History grows. Two opt-in settings bound it:

- `event_sink` sends events to the database, to a file, or both. Kinds you name can go to the file only.
- A per-card event window rolls old events off the database, and the feed reports what rolled off.

Freed space goes back to disk. `atrium db compact` writes a compacted copy of an old database offline.

## Notifications

One alert per event, in one form, wherever you are looking:

- With an atrium window focused, it is an in-page toast.
- With no atrium window focused, it is one desktop notification, with **approve** and **block** buttons. It works
  with no atrium tab open.

Alerts name the agent and its state, and carry the card's own mark, so which agent it came from is a picture
rather than a sentence. Sounds are per card, because knowing which session wants you without looking is the thing
you cannot get any other way.

## The bell

The bell keeps the last 200 things the board told you, newest first, with repeats folded and a copy icon on each
row. A toast is gone in seconds, and the bell is where you find it again. **clear** empties it.
