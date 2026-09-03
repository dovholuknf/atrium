# Auto mode, and reading the record afterwards

Stop asking. Keep recording. Read it back when the run is over.

## Why

The permission gate was built when being asked about every tool call was the only way to keep a grip on what an
agent was doing. There are stretches where the answer to every question is going to be yes, and pressing approve
four hundred times is a queue rather than oversight.

Turning the gate off loses the record: everything an agent did, in order, with the exact command. That record is
the reason to route tool calls through atrium at all, and it survives auto mode untouched.

Auto mode is therefore a trade: **interruption now for review later.**

## What it does

Per session, not global, so one session is trusted for a stretch while the others keep asking.

Turned on, that session's permission requests are answered `approve` immediately. Each is still recorded with its
command, its timestamp and `auto` as the answerer, which lets the review separate "a human said yes" from
"nobody was asked".

Stored on the card rather than held in memory. Losing it on a restart would start interrupting again with no sign
of why.

## What it does NOT override

Auto mode stops new questions. It does not discard answers already given, so it sits last in the chain, after
three things that still win:

1. **A queued message.** A message is you reaching out to a session. Auto mode means do not interrupt me, not do
   not let me speak.
2. **A shelved card.** Shelving puts work down and answers that work's requests with a block. Auto mode must not
   reopen it.
3. **A standing rule, including a `never`.** A `never` rule for `taskkill` was written on purpose, and a mode
   that swept it aside would make every rule conditional on a switch you might forget you flipped.

The review counts blocks separately: one reaching a session under auto mode means the session hit a wall and went
looking for a way around it.

## The review

`GET /v1/tasks/{id}/review`, and **what did it do?** on the card.

A session that ran one test command four hundred times produces four hundred rows that say nothing and hide the
single `rm -rf` that also went through. So the review is shaped to be read to the end:

- **Identical calls fold** into one line with a count. Same tool, same command, same decision, same answerer.
- **Grouped by tool**, tools with the most unattended decisions first. A tool whose every call matched a rule you
  wrote sorts last.
- **Unattended first inside a group**, then the most repeated.
- **Totals count decisions**, not lines, so folding never changes them.

Only `auto` counts as unattended. A rule match does not, even though no human saw it fire: the rule was a
decision made once, and re-surfacing it every time would bury the requests nobody considered.

Pending requests are not part of a review. Something still waiting has not happened yet.

## The board-wide switch

The same thing for every session at once, including ones that have not started yet. It lives in the header,
turning it on takes a confirmation that cannot be skipped, and the setting is held by the daemon rather than the
browser, so it is one answer for the machine and two tabs cannot disagree about it.

It is recorded as `global-auto` rather than `auto`. "I turned this session loose" and "I turned the whole board
loose" are different answers to be reading back six hours later.

### Turning it on empties the queue

The permission chain runs once per request, when that request arrives. Anything already waiting had asked before
the switch existed, so without this it would sit there under a header saying nothing will stop to ask, which is
the switch visibly failing to do the thing it says it does. Nobody turns auto mode on in the abstract. They turn
it on because something is waiting.

So the switch approves what is already queued. Three things about how:

- **Through the same decide path the buttons use**, not by writing approvals to the store. Each waiting agent is
  parked on an in-memory reply channel, and a decision it never sees leaves it blocked forever.
- **The chain's order is kept.** Shelving and standing rules sit ahead of auto mode, so a shelved card and a
  never rule both still hold. Auto mode stops new questions and does not discard answers already given. A card
  can be shelved after its request was recorded and a rule can be written while a request waits, so both are
  checked rather than assumed impossible.
- **The count is said out loud.** Those requests were in front of you a second ago and have just been answered
  on your behalf. The board also refreshes at once, so the toasts and notifications they raised are retired on
  the same click rather than shouting for another five seconds about requests that are already approved.

## What is not built

- **A time limit.** "Auto mode for the next hour" is the shape this should have. Today it stays on until
  switched off, with only the card's `auto` badge as a reminder.
- **Anything reading the review other than a person.** Feeding it to a model to summarise what changed is
  obvious and unbuilt.
