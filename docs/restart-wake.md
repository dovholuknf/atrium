# The after-restart wake

A room restart ends every terminal the room owns. The fixtures and the reopen list bring the sessions back with
their conversations, and each one then sits at an empty prompt until somebody types into it. The orchestrator is
one of those sessions, so every deploy it runs ends with it idle until clint types "we up".

A wake is that line, queued before the restart and typed by the room after it.

## Queuing one

- **From the session itself:** `atrium_wake_after_restart` on atrium-control, with `text`. It queues on the
  caller's own card, named by `X-Atrium-Agent`, and on no other card. `clear` cancels it.
- **From a script:** `POST /v1/tasks/{id}/restart-wake` with `{"text": "...", "by": "deploy.ps1"}` on the room's
  board, or on the hub with `X-Atrium-Room`. `{id}` is a card id or a wire name. `GET` reads it and `DELETE`
  clears it.

One wake per card. A newer one replaces the older one, and the answer names the text it replaced. The text is at
most 2000 characters. A wake is a prompt to pick work back up, and a longer briefing belongs in a file the prompt
names.

## Delivery

The room looks at every waiting wake every two seconds. It types a wake when all of these hold at once:

1. The card has a supervised runner that started after the wake was queued. The runner up at queue time is the
   one the restart is about to end, and nothing is typed into it.
2. That runner's SessionStart hook has fired and five seconds have passed, so the input box is drawn. A runner
   that never posts SessionStart gets a minute instead.
3. No dialog is on screen, and the runner is not mid-turn (`thinking`, `tool` or `compacting`).
4. The input line is empty and the keyboard has been quiet for two seconds. This is `injectPeer`'s gate, checked
   again under the input lock, the same rule as `docs/typing-race.md`.

If any check fails, the room asks again on the next tick. Once all four hold, it types the text with no banner,
presses Enter, deletes the row and records a `prompted` event with `from: restart-wake`. A later tick finds no row
and types nothing. A crash between the Enter and the delete would type the wake again after the next restart.

## Expiry

A wake waits 30 minutes. After that the restart it was queued for is not happening, and a prompt typed hours later
comes out of nowhere. The row is marked expired and kept. The card shows a `wake expired` chip with the text, and
a `notified` event with `what: expired` is recorded. An expired wake is never typed. It ages off after a day, or
goes when it is cleared or replaced.

A card whose runner atrium does not supervise has no terminal to type into, so its wake always expires.

## Why this is not a forced turn

"a2a stage 1: no forced turns" means one session does not start a turn in another. `internal/daemon/peers.go`
explains why a peer's words are gated on another session's terminal: the terminal is shared with a human, and a
peer is not that human.

A wake is different. The session queues it for itself, or a human runs the script that queues it. It only resumes
work the restart interrupted, and it lands in the session that asked for it, not in a peer's. It still goes through
the same gate as every automated write, so it never merges into a half-typed line or lands mid-turn.

## Where it lives

- `internal/store/restartwake.go`: the `restart_wake` table (migration `0059_restart_wake`) and its events.
- `internal/daemon/restartwake.go`: the in-memory mirror, the tick, the readiness rule and the endpoint.
- `internal/link/control_mcp.go`: `atrium_wake_after_restart`.
- `internal/api/web/js/board.js`: the `wake queued` and `wake expired` chips.
