---
title: Permissions and auto mode
description: The gate on every tool call, standing rules, auto mode, and the order in which they decide.
---

# Permissions and auto mode

Every tool call a gated agent wants to make comes to atrium first, and the agent waits for the answer. This page
covers who can answer, and in what order.

## Answering a request

A request lands in **needs permission** on the board, in the **perms** tab, and as a notification. Each one says
which agent is asking, because with several running the same command means different things from different
sessions.

A pending edit shows a real diff. Unchanged context is dimmed and the changed words are picked out, because
"approve this edit" is not a question you can answer from a file path.

- **approve** lets it run.
- **block** refuses it, and your reason goes back to the agent. A refusal becomes "no, do this instead".
- **always** and **never** answer this one and write a [standing rule](#standing-rules).

## Standing rules

Answer once with **always** or **never**, and every matching request after that is answered at once and never
shown. A rule covers a command shape or a folder.

**A command shape** is a prefix by default, or a glob when it contains `*` or `?`. `go build` covers every later
build and leaves `go install` to ask on its own.

**A folder** covers work inside a directory, two ways:

- the command names an absolute path inside the folder, or
- the session works inside the folder and the command does not reach out of it.

The second way is what makes folders useful. Commands are written relative to where the session is, and
`go test ./...` names no path at all. A command that mentions an absolute path outside the folder, or climbs out
with `..`, still asks. A rule answers a request. It does not simulate a shell, so it does not follow a `cd`.

:::tip Why folders exist
Writing "let it work in here" as a glob means you must account for the quoting yourself. `rm -f "C:/x/*"` fails
to match `rm -f "C:/x/y.db"` because of the closing quote, and nothing tells you. A folder rule says what you
meant.
:::

**The most specific rule wins**, so a narrow rule overrides a broad one.

### Import what you already trust

**perms → import rules from claude** reads Claude Code's allow and deny lists. It previews what it would add before
it adds anything, turns `Bash(go build:*)` into a prefix and `//c/temp/**` into a real folder, and lists anything
it cannot map instead of dropping it.

## Auto mode

Auto mode stops the questions and keeps the record. Turn it on for one card, for the whole board, or for the next
hour. Requests are approved at once, and each is recorded with `auto` as the answerer, so the record separates "a
person said yes" from "nobody was asked".

- **Per card.** Trust one session for a stretch while the others keep asking.
- **Board-wide.** The switch is in the header and asks for confirmation. Atrium holds the setting, so two tabs
  cannot disagree about it. Turning it on approves what is already waiting, and says how many.
- **For the next hour.** Either switch can carry a deadline. The time left is shown on the switch itself.

The deadline is checked when a decision is made, not by a timer. A timer does not fire across a restart, and auto
mode surviving a restart it should not have survived is the failure worth designing against.

### What did it do?

**what did it do?** on a card reads the record back, shaped to be read to the end:

- identical calls fold into one line with a count,
- grouped by tool, the tools with the most unattended decisions first,
- the decisions nobody saw come first inside each group.

A session that ran one test four hundred times produces one line, and the single `rm -rf` that also went through
is not buried under it.

## The permission chain

Every gated request passes these steps in order. The first one that answers wins, and the order is the design.

1. **A replayed decision.** The same request already answered gets the same answer. A retry is never asked twice.
2. **A queued message.** The call is blocked and the block reason carries your message. This is how a working
   session hears from you. See [Messages](./messages.md).
3. **A shelved card.** Shelving is a standing no.
4. **A standing rule.** The most specific match wins.
5. **Auto mode.** Approve and record. It sits after the rules, so a **never** rule still holds.
6. **Ask you.** The card moves to needs permission and the agent waits.

:::info Auto mode does not forget your answers
Auto mode means "stop asking me new questions". It does not override a **never** rule, a shelved card or a message
you queued. A `never` rule for `taskkill` was written on purpose.
:::

Every decision is written to the history with what made it: you, a rule, auto mode, or board-wide auto.

## Shelving

**Shelve** on a card puts the work down. Its pending requests are answered with a block, and any later ones are
too, until you bring the card back.
