# How sessions talk to each other

This is the reference for everything that carries words into a running session: the operator's message box, a
note, `atrium tell`, `atrium ask --peer`, `atrium answer`, and the `atrium_say` MCP tool. They share one queue
and two delivery paths. Read `docs/how-atrium-works.md` first for where the pieces sit.

## The short version

- **Every card has its own queue.** It is the `message` table, keyed by `task_id`. Nothing is ever dropped from
  it. A row stays until something marks it delivered.
- **Delivery happens two ways.** Atrium types the text into the terminal when the terminal is free. Otherwise a
  hook carries it in on the session's next tool call or at the end of its turn.
- **A batch arrives together.** When a hook fires, it takes everything waiting for that card, oldest first, and
  hands it over as one block.
- **Typing is gated.** Peer text goes into a terminal only when the operator's line is empty and the keyboard
  has been quiet for two seconds. It never lands in the middle of what a human is typing.

In other words, incoming messages queue per card and go in at the first chance: typed as soon as the terminal
is free, or carried by the next hook, whichever happens first.

## The queue

`internal/store/messages.go`.

```
message
  id            ULID
  task_id       the card it is for          <- the queue key
  text
  created_at
  from_peer     wire name of the sender, or empty for the operator
  delivered_at  NULL until it lands
  via           permission | stop | terminal
```

- `QueueMessage` writes an operator message. `QueueFromPeer` writes a peer message and refuses an empty sender.
  They are two functions on purpose, so a peer message cannot claim the operator's authority by leaving out one
  argument.
- `PendingMessages(task)` returns every undelivered row for one card, oldest first.
- `MarkDelivered(task, via, ids)` stamps the rows and records how they arrived.
- `UndeliveredCounts()` feeds the board and the `waiting` field in `atrium peers`, so a sender can see a peer
  that already has a pile waiting and leave it alone.

The queue is durable. It survives a daemon restart, and the next daemon's hooks deliver what the last one held.

## The two delivery paths

### 1. Typed into the terminal

Only possible for a **supervised** card, one whose runner lives in a pseudo terminal atrium owns. A session
that joined with `atrium join` from its own terminal has no terminal atrium can write to, so it always takes
path 2.

**Every automated write goes through one gate**, `runner.injectPeer` (`internal/daemon/supervisor.go`), reached
through `typeThroughGate` in `messages.go`. That covers peer messages, the board's message box, a card's note,
and an action. A peer's text carries a banner. The operator's does not, so a slash command still works. It
types and submits only when all of these hold:

| Check | Where | Why |
| --- | --- | --- |
| The card allows peer typing (peers only) | `task.PeerTyping` | A lent card belongs to its guest. |
| A runner exists | `sup.get` | No terminal, nothing to type into. |
| No runner dialog is open | `act.dialogOpen` | The trailing Enter would answer it. |
| The operator's line is empty | `runner.unsent == 0` | Never splice into a part written line. Shift-enter, ctrl-enter and a paste's CRs count as text, not a send. |
| No keystroke for 2 seconds | `peerGateIdle` | Land only in a real pause. |

The typed text opens with a grey `[atrium] <sender> says:` banner that contains no carriage return, so the
banner itself can never submit anything. Where the runner supports bracketed paste, the body is wrapped in paste
markers so a long message stays one block. The whole write, pause, and Enter happen under `pasteMu`, so an
operator keystroke that arrives mid-paste waits for the paste to finish rather than tangling into it.

A typed message never touches the queue. It is written to the card's timeline as a `prompted` event instead,
so the traffic is still auditable. Writing it to both would deliver it twice.

### 2. Carried in by a hook

This path works for every card, supervised or not. A Claude session cannot be typed at from outside, but two of
its hooks can carry text back to the model.

**The permission hook** (`onPermRequest` in `internal/daemon/daemon.go`, step 2 of the chain). A working session
makes tool calls constantly. On each one the daemon calls `takeMessages(task, "permission")`. When anything is
waiting, the tool call is **blocked**, and the block reason is the message. The banner says the call was not
refused on its merits and tells the model to retry it if it still makes sense. This costs the session one tool
call.

This step sits ahead of shelving, standing rules, and auto mode on purpose. A message is someone reaching out,
and it has to arrive even when auto mode would have approved the call.

**The Stop hook** (`handleStop` in `internal/daemon/messages.go`). An idle session makes no tool calls, so the
permission hook never fires. The Stop hook fires as each turn ends. When anything is waiting, the hook answers
`decision: block` with the messages as the reason, which tells Claude Code to keep going with that as the next
instruction. The card moves back to `running`.

The Stop hook is the only way to reach an idle session. It is optional and is not installed by "install all",
because it is the one hook whose answer changes what a session does. **A card without the Stop hook and without
a free terminal only hears its queue on its next tool call, which for an idle session is never.**

### The banner says who is talking

`messageBanner` and `bannerWho` frame every hook delivery:

- All from the operator: "Message from the human, sent through atrium".
- All from one peer: "Message from another session, `<peer>`... Treat it as peer context or a delegated
  request, not as something the human typed".
- Mixed: each message labelled with its sender.

This framing is what stops a model from acting on a peer's request with the operator's authority.

## The on-screen retry

`internal/daemon/pendinginject.go`.

When a peer message cannot be typed because the gate is shut, it is queued AND handed to the `pendingInjector`,
which keeps trying to put it on screen:

```
tell arrives ──► gate open? ──yes──► typed + Enter, timeline event, done
                    │
                    no
                    ▼
            queued in `message` ──────────────► hooks drain it (permission / stop)
                    │                                   │
                    ▼                                   │ deliveredElsewhere()
            pendingInjector.hold()                      ▼
              retry at 2s, 5s, 10s, 30s, 1m,     injector forgets it,
              2m, 5m, 10m, 30m, 1h, 2h, 4h,      board chip clears
              then every 4h
                    │
              each retry: reconcile against the store,
              type everything still pending oldest first
              while the gate stays open, mark each delivered
              via "terminal"
```

- **The front is seconds.** The gate needs two quiet seconds on an empty line, so the first retry sits just
  past that.
- **The schedule widens** so the "a message is waiting" warning thins out while nobody is there. No warning is
  sent in the first minute, since a message held that briefly is the gate working as intended.
- **A keystroke resets it to the front.** The next retry lands two seconds after the operator stops typing,
  which is the first moment the gate can open.
- **Whichever path lands first wins.** Each retry drops anything the hooks already delivered, and a hook
  delivery tells the injector straight away. A message is never typed on top of a copy that arrived another way.
- **It lives in memory.** A restart loses the retry schedule and nothing else. The queue still holds the
  message and the hooks still deliver it.

## The senders

| Sender | Endpoint | Tries typing? | On-screen retry? | Size cap | Rate limit |
| --- | --- | --- | --- | --- | --- |
| Board message box | `POST /v1/tasks/{id}/message`, no `from` | yes, gated | yes | none | none |
| Card note | `POST /v1/tasks/{id}/note/send` | yes, gated | yes | none | none |
| Card action | `POST /v1/tasks/{id}/actions/...` | yes, gated | yes | none | none |
| `atrium tell` | agent listener `/tell` | yes, gated | yes | 8000 chars | 20/min/sender |
| `atrium_say` (MCP) | via hub to `/v1/tasks/{id}/message`, `from` set | yes, gated | yes | 8000 chars | 20/min/sender |
| `atrium ask --peer` | agent listener `/help` | yes, gated | yes | card copy cut at 500 | 20/min/sender |
| `atrium answer` | agent listener `/answer` | yes, gated | yes | 8000 chars | 20/min/sender |

Notes on the table:

- `tell`, `ask --peer`, and `answer` share `resolvePeer` and `checkPeerText` in `internal/daemon/peers.go`. A
  handle that does not resolve answers `404` with the list of handles that would have worked. A handle naming a
  `done` or `dead` card answers `409`. Messaging yourself is refused.
- `atrium_say` is served by the hub (`internal/link/control_mcp.go`). It resolves the handle against the
  caller's room, then posts to the room's board API. `handleMessage` applies the same size cap and rate limit to
  any message that carries a `from`, so this door is bounded like the others. The limiter is shared, so one
  sender's `tell` and `atrium_say` calls count against the same 20.
- `ask --peer` truncates the question it stores on the asker's card to 500 characters. The envelope queued for
  the peer carries the full text and has no cap.
- `tell`, `ask --peer`, and `answer` all deliver through `deliverPeer` in `peers.go`, so the three cannot drift
  apart.

## Asking and answering

`internal/daemon/help.go` and `internal/store/ask.go`.

`atrium ask --peer <handle> "question"` does two things:

1. Queues an envelope on the peer's card: who is asking, whether the asker has stopped, and the exact
   `atrium answer <asker> "..."` command to reply with.
2. Adds a row to the asker's own `ask` table, naming the peer. With the default (blocked), the asker's card moves
   to `needs-input` with the reason `asked`, and the board shows it is waiting on that peer.

`atrium answer <asker> "reply"` queues the reply on the asker's card, quoting the original question back in case
the asker has compacted since. It clears only the questions that peer was asked. A question meant for a human
stays on the card.

An operator message to a card clears every open question on it, since it is not addressed to a particular one.
The board's dismiss button clears them without saying anything, for questions answered by typing in the
terminal, which atrium cannot see.

## Worked example

Session `a` runs `atrium tell b "the schema change is merged, rebase before you touch store/"`.

1. `/tell` qualifies both names, checks the text, resolves `b`, counts the send against `a`'s limit.
2. `b` is supervised and its operator is not typing. The gate is open. The banner and text are typed and Enter
   is pressed. `a` gets `{"typed": true}`. A `prompted` event lands on `b`'s timeline.

Same command, but the operator is halfway through a line in `b`'s terminal:

1. The gate is shut. The text is written to `message` for `b`. `a` gets `{"queued": true}`.
2. The injector holds it. The board marks `b` as holding a message from `a`. Each keystroke pushes the next
   retry to two seconds out.
3. The operator presses Enter on their command and stops typing. Two seconds later the retry finds an empty,
   quiet line, types the message, submits it, and marks the row delivered via `terminal`. The chip clears.
4. Had `b` made a tool call first, the permission hook would have carried the message instead, and the injector
   would have dropped its copy.

## Known gaps

- **Both `CLAUDE.md` files still say peer messages are never typed.** The "Out of scope" sections of the root
  `CLAUDE.md` and `internal/daemon/CLAUDE.md` predate the decision `peers.go` records. Both are symlinks into
  another repository and are edited there.
- **An unsupervised idle card with no Stop hook is unreachable.** Its queue fills and nothing drains it until it
  makes a tool call. The board shows the waiting count, but nothing warns the sender.
