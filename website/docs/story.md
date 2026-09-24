---
title: Why atrium exists
description: The problems atrium hit, in the order it hit them, and what it changed to answer each one.
---

# Why atrium exists

Atrium was not designed and then built. It was built, used every day, and corrected by what went wrong. This page
tells that story in order: the problem, what was tried, and what changed. Most of what atrium does today is an
answer to one of these.

## It began as a chat window

The first atrium, in June 2026, was a terminal you typed into. Each claude session loaded a small MCP server whose
one tool, `submit`, posted to that terminal and waited for a reply. You typed a prompt, the agent acted, it
submitted again. `@fix-login` routed a prompt to one agent by name.

The hard parts were about not wasting the model's time:

- **The model never sees a disconnect.** Close the hub and the agent retries in silence, forever, with backoff.
- **The model never sees an empty prompt.** A long-poll timeout is absorbed as a keepalive.
- **Idle costs nothing.** Those two together mean a parked agent burns no tokens.
- **The permission hook fails open.** If the hub is down, Claude Code's own flow takes over. Failing closed would
  brick every session.

Those rules survive in atrium today. The hub itself was amnesiac on purpose: restart it and everything was gone.

## It answered the wrong question

The broker worked. It answered "let me talk to many sessions from one terminal". The question that cost time every
day was a different one: **what do I have running, which one needs me most, and what was I even doing in that
one?**

So the second version, in September, changed what atrium is. It stopped being a message broker and became **a task
board with live agents attached**. That reversed the first version's central rule. An amnesiac hub cannot remember
why you started something, so atrium got a SQLite database and an append-only event log for every card.

Durable state brought a new rule with it: **running without durable state is worse than not running.** If storage
fails, atrium closes the port agents talk to and leaves it closed. Agents see connection refused and park on the
backoff they already have. A second port, for you, stays up to say what broke.

## The board filled with ghosts

The first way sessions reached the board was to adopt them from a ledger kept by an external worktree tool. It
turned every session the ledger had ever seen into a card, and atrium could talk to none of them. The board filled
with hundreds of cards labelled as waiting on a human who had no way to answer.

It was removed. Sessions now **register through hooks**. `SessionStart` opens a card the moment a session begins,
before it has made a single tool call. `SessionEnd` is the more valuable half: it is the only reliable signal that
a session is over. Asking the model to announce itself would have spent a turn on something the harness already
knew.

Liveness went the same way. A card stores its runner's process id, and whether that process exists is a question
the operating system answers for free. That became a principle: **answer every question that does not need a
model.**

## Approving the same thing four hundred times

The permission gate made every tool call wait for a click. That is oversight for the first ten and a queue after
that.

- **Standing rules** came first: answer **always** or **never** once, and matching requests are answered without
  being shown. Importing Claude Code's own allow and deny lists brought 134 rules across on the first run. That was
  the difference between a usable board and a click farm.
- **Folder rules** came next, because writing "let it work in here" as a glob meant accounting for the quoting
  yourself. `rm -f "C:/x/*"` fails to match `rm -f "C:/x/y.db"` over the closing quote alone, silently.
- **Auto mode** came for the stretches where every answer is yes. It approves without asking and still records
  everything, then **what did it do?** reads the record back with identical calls folded and the unseen decisions
  first. The trade is stated plainly: interruption now for review later.

Auto mode then grew a deadline, "for the next hour", because approving everything is easy to leave on. The deadline
is checked inside the permission chain rather than by a timer, because a timer does not fire across a restart, and
auto mode surviving a restart it should not have is the failure worth designing against.

## "Ready" meant two things

Every card that stopped landed in `ready`. That covered an agent that ran out of work and would sit there forever
costing nothing, and an agent that asked a question and could not continue. Those want very different amounts of
hurry.

Claude Code's `Notification` hook fires only for the second, so atrium tells them apart. A card that asked says so
and sorts above one that merely stopped. Later, `atrium finish` let an agent say its work was over and what it did,
and `atrium ask` let it say it was stuck and what would unstick it.

The same split happened between status and activity. The columns are buckets of your attention, and a card sits in
one because you must act. What a runner is doing right now, thinking or running `Bash` or waiting on three
subagents, is a fact about the runner. It became a live badge and is **never stored**, because a stored activity is
a lie the moment the daemon restarts.

## Owning the terminal

The goal was never to open a runner's terminal by hand. So atrium learned to launch runners under a pseudo terminal
it owns, and the browser attaches over a websocket. ConPTY on Windows passed a spike with one trap worth an
afternoon: after tearing a pty down, returning normally from `main` exits with status 127.

Owning the terminal has a cost that shaped a lot later. On Windows, closing a pty takes the process with it, so
**a supervised runner dies with the daemon that owns it**. There is no reattach, so the answer is resume ids: a
restart costs a session its terminal but not its conversation. It also means **stopping is not killing**.
`atrium stop` winds down in order and gives runners ten seconds.

Sharing a terminal between a person and atrium raised its own questions. A narrow window shrank the session for
every viewer, so the width now follows the widest viewer and a narrow one scrolls sideways. Atrium once answered a
permission dialog by accident, so it now refuses to type into a dialog it did not raise. And it never types into a
line you are writing: every automated write waits for an empty line and two quiet seconds.

## Every board change restarted every agent

The board is served by the daemon that owns the ptys. Installing a new board meant restarting the daemon, which
meant killing every supervised agent, to look at a change nobody knew was any good yet.

The first answer was a **preview board**: a second daemon on a copy of the cards that takes no hooks and acts on
nothing. The lasting answer was to split atrium along lifetimes. **The hub** serves the board and holds no card
state, so it restarts freely. **The room** owns the database, the ptys and the agents, and runs for days. The room
dials the hub, never the reverse, and joining is one pasted string that pins the hub's certificate authority and
then speaks mutual TLS.

The split turned out to be the answer to two other problems:

- **Many machines.** A second machine is another room dialing the same hub. An older heartbeat federation was
  superseded by it.
- **Agents under their own account.** Running an agent as its own operating system user bounds what it can reach,
  and it used to make the agent vanish from your desktop. A room per account puts it back on the board, while the
  isolation stays the account boundary itself.

The first join strings let a room join as any name. That was removed: the hub now names each room when it is added,
and the room reads its name from the certificate the hub signed.

## Talking to a session you do not own

There is no way to type into a claude process from outside, and atrium will not invent one. A message you queue
waits until the session can hear it: typed into the terminal when atrium owns it and your line is empty, or carried
on the session's next tool call or turn end through a hook. It arrives framed as you talking, not as a policy
refusal.

Peer messages between sessions were queued at first, always, because the terminal was held for the human. That was
overruled: the terminal is shared, so a peer message is typed when the terminal is free and queued when it is not.
Stopped sessions stay token-conservative. Atrium reports a silent stop on a backoff and never forces a turn.

## Reaching it from somewhere else

"Use an overlay" was advice, not a feature. Atrium now opens a zrok share or an OpenZiti service itself: the SDK
hands back a listener and the board is one handler on it. It never proxies, never holds an identity, and never
decides who may connect. The loopback board still has no login.

A published board is different. Somebody putting a board on a public zrok share for an afternoon has no OIDC
provider to hand, and what they reach for instead is no login at all. So a published board can ask for a name and
a password, and a public share is refused without one.

## Installing it

An `atrium install` command was written and removed before it shipped. Copying a binary to a fixed path is the
shallow half of installing: no version, no uninstall, no upgrade. Packaging does the rest now: deb, rpm, a macOS
pkg, a Windows MSI, and a no-admin path on every platform.

On Windows the obvious answer, a service, runs in session 0 and cannot open a terminal you can attach to. It would
install, report `Running`, and supervise nothing anybody can use. Atrium starts from a logon task instead.

## Where it stands at 0.0.1

The **stack** and the **board**, where atrium started, see less use now that most work happens in supervised
terminals. They are still there, and they are the right tool when you do not want atrium as your terminal: a
lighter mode that watches and gates sessions running wherever you started them. [Two ways to run
it](./modes.md) covers both.

The pattern across all of this is the same. Something atrium did cost somebody time. The fix went in, and the rule
behind it was written down so the next change would not undo it.
