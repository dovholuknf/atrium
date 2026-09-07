# Statusline telemetry

How much context a session has burned, and how close its account is to a limit, drawn on the card.

This document is the contract. It is written so the other half, a statusline script that lives in a different
repository, can be implemented against it without asking anybody a question.

## Why this exists

The board answers "which of these wants me". It cannot answer "which of these is about to lose the plot".

A session at ninety two percent of its context window is about to compact, and compacting is where it forgets
what it was told at the start. That is the single most useful input to "which of sixteen agents do I interrupt",
and it is the one thing about a running agent atrium could not see. Hooks carry which tool started and which
subagent ended. None of them carries a token count.

Claude Code hands its statusline script the richest per-session payload it exposes: context used and the window
size, the rolling limits, the model, the transcript path. Exactly one thing on the machine receives that, and it
is the statusline. So the statusline posts the part that matters here.

## What atrium owns and what it does not

Atrium owns the endpoint, what the board draws from it, and this document. The statusline script itself is not
in this repository and will not be. What follows is everything needed to write it.

## The wire

```
POST /telemetry
```

On the AGENT listener, the one hooks use, which defaults to `localhost:7777`. See "Finding the daemon" below.

Request body:

```json
{
  "session_id": "9a4f...",
  "context_used": 184000,
  "context_window": 200000,
  "model": "Opus 5",
  "five_hour": { "pct": 42, "resets_at": "2026-09-06T18:00:00Z" },
  "weekly":    { "pct": 71, "resets_at": "2026-09-11T00:00:00Z" }
}
```

Response, always:

```json
{ "ok": true }
```

### Fields

| field | type | required | meaning |
| --- | --- | --- | --- |
| `session_id` | string | one of three | The harness's own id for the conversation. Claude Code's `session_id`. |
| `task_id` | string | one of three | The card outright, for a caller that was told which one. Wins. |
| `agent` | string | one of three | The wire name every other atrium hook uses. The last fallback. |
| `context_used` | int | no | Tokens used. |
| `context_window` | int | no | Size of the window those tokens are counted against. |
| `context_pct` | int | no | Used as a percentage, for a caller that only knows the fraction. |
| `model` | string | no | What the session is running, as the harness displays it. Truncated at 60 characters. |
| `five_hour` | object | no | `{ "pct": int, "resets_at": RFC3339 string }`. |
| `weekly` | object | no | The same shape. |

At least one of `session_id`, `task_id` and `agent` must be present. `session_id` is the one a statusline
definitely has, and it is the path this was built for.

**`context_used` and `context_window` beat `context_pct`.** Send the pair when you have it. Atrium derives the
percentage from it, so the tooltip's token figures and the chip's percentage cannot disagree. A caller that
sends all three has its `context_pct` ignored.

### What is deliberately not on the wire

**The transcript path.** Atrium finds a card's transcripts itself, from the session id, in
`internal/api/sessions.go`. Accepting one here would be a second source of truth for the same fact, and the one
on the wire is the one nobody would notice going wrong.

**Cost, duration, lines changed.** Real numbers, and none of them decides which agent to interrupt. A field
added to a contract is a field that has to keep working.

**Claude Code's statusline payload, passed through whole.** That payload belongs to the harness, changes on its
release schedule, and carries a great deal atrium has no business holding. The caller extracts, atrium receives
named fields.

## How a post finds its card

In order. The first one that matches wins, and a post that matches nothing is dropped.

1. **`task_id`**, looked up directly.
2. **`session_id`**, matched against the card's `resume_id`. Atrium already records the harness's session id
   there, so this is an ordinary lookup with nothing new stored to support it.
3. **`agent`**, matched against the card's wire name, qualified the same way every hook is.

**When none of them matches a card, nothing is recorded and the endpoint answers `ok`.** A statusline renders in
every session on the machine, including the ones atrium has never heard of and the ones that never joined the
board. There is nothing wrong with those, so there is nothing for the caller to handle. No error, no log line,
no retry.

An empty `session_id` matches nothing rather than matching the first card with a blank `resume_id`. Plenty of
cards have one, because not every runner reports an id.

## The posture: this endpoint can never fail a caller

The same rules `/activity` follows, in `docs/activity-design.md`, and they matter more here because a statusline
renders many times a second while a hook runs once per tool call.

On atrium's side:

- **The response is written before the body is read.** Nothing about a telemetry post can produce a non-2xx,
  including a malformed one, because a caller that treats non-2xx as a failure would surface it once per render.
- **An unknown session is accepted and dropped**, as above.
- **All the work happens after the reply**, on a goroutine the caller does not wait on.
- **A reset time that will not parse costs the timestamp and not the limit.** The percentage is what anybody
  acts on.
- **Everything is clamped.** Percentages land in 0..100 whatever arrives, because a card reading `ctx 340%` is a
  board nobody believes again.
- **An empty post is not recorded.** A body with no percentage, no tokens and no limits leaves the previous
  figure alone rather than blanking it.

On the caller's side, and this is the part that has to be written into the statusline:

- **One second timeout on the whole request.** A statusline that stalls is a terminal that stops drawing.
- **Every failure ignored.** Connection refused, timeout, 5xx, unreadable body, no daemon at all. None of it
  reported, retried, or printed. The statusline renders exactly what it would have rendered anyway.
- **One attempt.** No retry, ever.
- **Nothing downstream reads the result.** The statusline's output must not depend on this post having worked.

## The throttle, which the caller is expected to apply

**Post at most once every ten to fifteen seconds per session, not once per render.**

A statusline is invoked every time the terminal redraws, which is many times a second while somebody is typing.
Context does not move at that rate: it changes when a turn completes. A post per render would be several hundred
times the useful rate, for a number that would be identical every time.

Implement it in the statusline by remembering the time of the last post per session and skipping the post
otherwise. The rest of the statusline still renders on every call: only the post is skipped.

**Atrium enforces a floor of two seconds per caller.** A post inside that window is dropped before the store is
touched, so a caller that ignores the throttle costs one map lookup rather than one database read. Two seconds
is well under the requested cadence on purpose: a caller that obeys this document is never dropped by it, so the
floor is not a rate anybody has to tune against.

## Finding the daemon

The agent listener defaults to `localhost:7777` and a machine running a second daemon, or one started with
`--agent-addr`, is not on that port. The address is written down:

- `%LocalAppData%\atrium\daemon.json` on Windows, `~/Library/Caches/atrium/daemon.json` on macOS,
  `$XDG_RUNTIME_DIR/atrium/daemon.json` on Linux. The `agent` field is the address to post to.
- `$ATRIUM_HUB_URL`, when it is set, names it outright.
- `localhost:7777` when neither says anything.

A stale file is not a problem worth guarding against. The daemon is gone, the connection is refused in
milliseconds, and the caller ignores failures anyway. See `internal/daemon/whereami.go`, which says so at
length.

`ATRIUM_PERM_GATE=off` turns every atrium hook off in a session. A statusline should honour it the same way and
post nothing.

## What the board draws

On the card, beside the activity badge:

- **`ctx 92%`**, from the percentage. Quiet under sixty, warned above sixty, and in the danger colour above
  eighty five. Most of a session's life is spent under half a window, and a chip that is lit the whole time is a
  chip you stop reading, so the colour only arrives when the number starts deciding something.
- **`5h 91%`** and **`week 94%`**, only when a limit is at eighty percent or more. Below that it is a fact about
  billing and not about this card.
- The tooltip carries the tokens, the model, both limits with the time each resets, and how long ago the figure
  arrived.

Drawn while the card is WAITING, unlike the activity badge, which is hidden then. That is exactly the moment the
number decides something: whether to answer this one now or let it sit.

## Never stored

Held in the daemon's memory, beside the activity map, and gone when the process ends. The argument is the one in
`docs/activity-design.md` and it applies here without modification: a context figure is a fact about a process
that is running right now, and written down it survives the restart that killed the session it described. A card
saying "92% context" about a conversation that no longer exists is worse than a card saying nothing, because it
is the number that decides where you look.

The figure is dropped when a card's session ends, along with its activity, by the same `forget`.

### It expires more slowly than an activity does

Thirty minutes, against fifteen for the activity badge, and the difference is deliberate.

An activity goes wrong quickly. "Running Bash" is false the moment the tool ends, and a session killed mid-tool
pins it until the daemon restarts.

A context figure goes wrong slowly, because context only grows. A number from ten minutes ago is a floor on the
number now, which is what the decision to interrupt actually wants. And a session sitting idle stops redrawing
its terminal, so it stops posting, and that idle card is exactly the one whose context decides whether you
resume it or start again.

The two therefore live in the same map and expire on different clocks. A card twenty minutes idle shows no
activity badge and still shows its context.

## A statusline post is not activity

It does not move the card's last-activity time and it does not set the activity badge.

A statusline redraws when the TERMINAL does, which includes a human typing in it, a window resize, and a timer
firing with nothing happening at all. Counting that as the agent doing something would make the board's
"quietest card" sort mean "the card nobody has looked at" rather than "the card that stopped", and that sort is
what the dispatch decision runs on.

## Testing it by hand

With a daemon running and a card whose `resume_id` you know:

```bash
curl -s -X POST localhost:7777/telemetry -H 'content-type: application/json' -d '{
  "session_id": "PUT-A-REAL-RESUME-ID-HERE",
  "context_used": 184000, "context_window": 200000, "model": "Opus 5",
  "five_hour": {"pct": 91, "resets_at": "2030-01-01T00:00:00Z"}
}'
```

The card grows a red `ctx 92%` chip and an amber `5h 91%` beside it within five seconds, which is the board's
poll interval. A session id nobody has answers `{"ok":true}` and changes nothing, which is the behaviour, not a
failure.
