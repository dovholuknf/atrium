# Seen tracking

Whether the human has seen a session's latest turn, and whether that turn asked them questions they have not answered.
Atrium records both on the card, the board draws them, and an agent can ask for them.

## The problem

On 2026-09-23 the orchestrator ended turns with an `Open Questions:` block while eight workers were running. Clint
did not see several of those turns. The orchestrator kept saying "the four questions from my last message are still
open" about a message the operator had never read. Neither side knew.

Atrium already knew half of it. The Stop hook says when a turn ended, `UserPromptSubmit` says when somebody typed a
prompt, and the board knows which terminal is on screen. Nothing put the three together, and nothing wrote them down.

## What "seen" means

A turn is SEEN when any of these happens after it ended:

| via | what happened | who decides |
| --- | --- | --- |
| `viewed` | a board window showed that card's runner terminal, scrolled to the bottom, for 3 seconds | the board |
| `typed` | the operator sent keystrokes into that card's terminal over the attach socket | the room |
| `prompt` | the operator submitted a prompt to that card (`UserPromptSubmit`) | the room |
| `message` | the operator sent the card a message or a note from the board | the room |

`viewed` needs all of these at once, held for the whole 3 seconds:

- The window's `document.visibilityState` is `visible` and `document.hasFocus()` is true. A tab behind another
  tab, a minimised window, and a window that is open but not the one in front all fail this.
- The terminals view is the one showing, and the pane is attached to that card's RUNNER. A shell opened beside
  the runner is a different screen, and the turn's text is not on it.
- The attach socket is open. A pane still replaying, or one that has lost its socket, shows nothing trustworthy.
- The xterm viewport is at the bottom of the buffer (`viewportY >= baseY`). A turn ends at the bottom of the
  screen, so somebody scrolled up into the history is reading something else.
- The phone switcher is not open over the pane.

Three seconds, because a turn's last message is usually a few lines and the point is to rule out passing through.
A switch between cards on the way to another one takes well under a second. The dwell restarts if any condition
drops, and it is keyed to the turn: a turn that ends mid-dwell starts a fresh one.

What does NOT count:

- A card on the board, a row in the terminal strip, a stack row. None of them show the turn's text.
- A notification or a toast. They say a turn ended, not what it said.
- Hovering or clicking a card without attaching.

**On a phone**, the same rule holds. A phone reports `hidden` when the screen locks or another app comes to the
front, and `hasFocus()` is true for the page in front, so the rule needs no special case. The switcher dropdown
covers the pane, so an open switcher does not count.

**In a popped-out window**, the same rule holds. A popped-out window is the same page in terminal-only mode with
its own document, so it has its own visibility and focus. Two windows on one card are two viewers, and either one
seeing the turn is enough. A popped-out window behind the board does not count, which is the case it most needs
to get right: the terminal is there, the human is not looking at it.

### Why not ask the room to decide `viewed`

The room knows a pane is attached. It does not know whether the tab is in front, whether the window has focus, or
where the pane is scrolled, and an attached pane in a background tab is the normal state of a board with eight
workers. Only the browser can tell. The room decides everything it can see for itself.

### Why typing counts

Keystrokes into a terminal arrive over the attach socket, and only from a window that has that pane focused. The
room already records them (`noteOperatorTyped`) to decide whether a peer may type. Somebody typing into a card has
it in front of them, and the gate has already decided that is the operator and not atrium typing.

### A prompt that is not the operator

A peer message typed into a terminal atrium owns is submitted, and Claude Code fires `UserPromptSubmit` for it
exactly as it does for a human. Counting it would mark a turn seen, and its questions answered, by another agent.
That is the failure this feature exists to prevent, so the room tells them apart: `injectPeer` stamps the moment it
submitted a peer's message, and a prompt arriving within 30 seconds of that stamp, with no operator keystroke
since, is the peer's and counts for nothing.

A message the operator sends from the board goes through the same typing path with no banner and counts as the
operator. A queued message from the operator counts the moment it is queued, which is the rule `askAnswered`
already follows.

## Open questions in a turn

### The source

Atrium does not record session output, and `atrium_task` says so. This does not change that. The cheapest honest
source is the Stop hook's own payload:

1. `last_assistant_message`, which recent Claude Code puts in the Stop payload. The whole text of the turn's last
   message, already in hand.
2. Else `transcript_path`, read from the END, at most 256 KiB, for the last assistant entry that has text.
3. Else nothing. The turn's questions are unknown, and the card keeps whatever it had.

The hook extracts the questions and sends only them. The message and the transcript never leave the hook.

### The block

The shape the orchestrator is told to use:

```
Open Questions:

---

1. first question
2. second question
```

Read leniently. The heading may be bold (`**Open Questions:**`) or a markdown heading (`## Open Questions`), and
the `---` and blank lines are optional. Items are `1.` or `1)`. An indented line continues the item above it. The
first non-blank line after an item that is neither an item nor indented ends the block. The LAST block in the
message wins, because a message that quotes an earlier block and then asks its own means the second.

Bounded: 10 questions, 500 characters each, the same caps `ask` uses. A block over the cap keeps the first ten.

A heading with no items it can read is recorded as "asked, text unknown". The card shows a question mark with no
count, and an agent is told the questions exist and could not be read.

### Replace, keep, answer

- A turn with a block REPLACES the card's questions. An agent that re-surfaces its open questions restates them,
  and one that drops a question has decided it no longer needs asking.
- A turn with no block KEEPS the card's questions. A turn that ran because a peer reported in does not answer what
  the human was asked.
- A turn whose text could not be read keeps them too.
- The questions are ANSWERED when the operator next submits a prompt to the card, or sends it a message or a note.
  A peer's message does not answer them. Answering also marks the turn seen.

Answered is a state of the whole set, not per question. Atrium cannot tell which question a reply addressed, and
guessing would be worse than saying "the operator replied after you asked".

### Why not the `ask` table

`atrium ask` rows are questions an agent put deliberately, one at a time, with routing to a peer and a per-question
answer. A scraped block is a fact about one turn, replaced wholesale by the next block. Writing scraped questions
into `ask` would light the ask line, move `waiting_reason`, and feed `fleet.go` and `atrium peers`, all of which
read `ask` as the agent's own deliberate words. Kept separate, and listed below as a question.

## Storage

Durable, because seen-ness has to survive a restart. `docs/activity-design.md` draws the line: activity is about
now and dies with the process, and a card fact that answers "how long has this been sitting" is written down. "Has
the operator seen this" is the second kind. A board that restarts and forgets which turns were read would mark
every card unread, or none, and both are lies.

The keystroke path answers from an in-memory set of unseen cards, filled from the table at start, so a keystroke
costs one map lookup and the write happens off the path. The table is the truth and the set is a cache of it.

Its own table rather than columns on `task`:

```sql
CREATE TABLE IF NOT EXISTS turn_seen (
  task_id        TEXT PRIMARY KEY REFERENCES task(id) ON DELETE CASCADE,
  turn_ended_at  TEXT NOT NULL DEFAULT '',
  seen_at        TEXT NOT NULL DEFAULT '',
  seen_via       TEXT NOT NULL DEFAULT '',
  questions      TEXT NOT NULL DEFAULT '[]',   -- JSON list of strings
  questions_unparsed INTEGER NOT NULL DEFAULT 0, -- a block was there and could not be read
  questions_at   TEXT NOT NULL DEFAULT '',
  answered_at    TEXT NOT NULL DEFAULT '',
  answered_via   TEXT NOT NULL DEFAULT ''
)
```

A table because `task` is the most contended file in the repo: three branches are adding columns to it this week,
and every column added there changes `taskColumns`, `scanTask` and the insert's placeholder count. A side table
keyed by card is read for the whole list in one query, the way `OpenAskCounts` already is, and cascades with the
card.

Migration `0057_turn_seen`, at the end of the slice, `CREATE TABLE IF NOT EXISTS`, so a re-run is a no-op. 0055 and
0056 are taken by `claude/a2a-reliability`.

The hub does not cache it. `/v1/state` sends the stored task row and nothing else, so a shut room's cards carry no
unread mark on the hub's board. A room that is offline has nothing new to be unread.

## The wire

### Hook to room

The Stop hook's `/stop` body gains three fields. The hook posture holds: the transcript read is bounded in size,
every failure is swallowed, and the stdout contract is untouched.

```json
{ "agent": "...", "cwd": "...", "resume": "...", "resumable": true,
  "questions": ["..."], "questions_block": true, "questions_known": true }
```

`questions_known` false means the text was not reachable and the card's questions stay as they were.

The room records a turn ending only on the path where the Stop hook lets the turn end. A Stop that delivers a
queued message sends the model back to work, so the turn is not over and its end is recorded when it does end.

### Board to room

```
POST /v1/tasks/{id}/seen    {"turn_ended_at": "<the value the board was shown>"}
```

Answers with the card's seen state. A `turn_ended_at` older than the stored one is a board that has not heard about
the newest turn yet, and marks nothing: the answer carries `"stale": true` and the current state. Card-scoped, so
the hub proxy routes it to the owning room by id with no new rule.

### Room to board and to agents

Every card view (`/v1/tasks`, `/v1/tasks/{id}`, `/v1/waiting`) carries `seen` when a turn has ever ended:

```json
"seen": {
  "turn_ended_at": "2026-09-23T15:04:05.000Z",
  "seen_at": "2026-09-23T15:01:00.000Z",
  "seen_via": "viewed",
  "unseen": true,
  "open_questions": ["which branch is base"],
  "questions_unparsed": false,
  "questions_at": "2026-09-23T15:04:05.000Z",
  "answered": false
}
```

`answered` is absent when the card has never had a block. `open_questions` is absent once answered.

## Surfaces

### The board

Two marks, both chips, following "status is a column, activity is a badge". Neither moves a card.

- **Unread**: a small dot chip on a card whose latest turn is unseen. Clears when the turn is seen.
- **Questions**: a `? 3` chip, in the warn colour, on a card with unanswered questions. `?` alone when the block
  could not be read. The tooltip lists the questions. Clears when answered.

Drawn on the board card, the stack row and the terminal strip row, which is where the operator is when workers are
running. The detector is `js/seen.js`, ticking every half second.

### Agents

`atrium_task` gains a `seen` block, the same shape as above, and `card` becomes optional: empty means the calling
session's own card, so the orchestrator asks about itself with no handle.

`atrium_peers` gains `unseen` and `open_questions` (a count) per peer, so one call answers "who has something the
operator has not read".

The orchestrator's loop becomes: before saying "my questions are still open", call `atrium_task` with no card. If
`seen.unseen` is true, the operator has not read the last turn and the questions should be re-surfaced in full
rather than referred to. If `answered` is true, the operator replied, and the reply is in the prompt it is already
holding.

## Not built

- The board-level "unanswered questions" tray. The per-card chip and `atrium_peers` cover the case that prompted
  this. Listed below.
- `seen` on the stdio `atrium control` server in `internal/cli/control_peers.go`. The hub's control server is the
  one sessions use.

## Known gaps

- A guest typing into a lent session types over the same attach socket, so it counts as `typed`. A lent card is
  one somebody chose to hand over, and the guest is looking at it.
- A message posted to a card with no `from` is the operator's channel, whoever sent it. That is the rule the
  message path already follows for `askAnswered`, so a script posting without a `from` answers the questions.
- A runner that does not send `last_assistant_message` and writes its transcript late leaves the turn's questions
  unknown. The card keeps the previous set rather than guessing, and the turn is still recorded as unseen.

## Open questions

Open Questions:

---

1. Should scraped Open Questions also become `ask` rows, so they light the existing ask line and `fleet.go`? The
   default is no: they stay a separate turn fact, drawn by their own chip.
2. Is 3 seconds at the bottom the right dwell? The default is 3. Longer is safer against flicking past and slower to
   clear a mark the operator did read.
3. Should the board-level tray of unanswered questions be built? The default is not yet.
4. Should an unread mark ever ring or notify on its own, for example after 10 minutes unread? The default is no:
   the existing turn-end alert already rings once.
