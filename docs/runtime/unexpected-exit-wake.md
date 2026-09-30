# The unexpected-exit notice

When the room goes away while a runner is in the middle of a turn, the turn is lost. A crash, a kill, a planned stop
and a deploy all do this. The fixtures and the reopen list bring the session back with its conversation, and it then
sits idle at an empty prompt, because the turn that was running is not resumed.

The notice is one line that the room types into that session after it comes back:

```
[atrium] unexpected exit: atrium went away while you were working (crash) at 2026-09-25 14:02:11 EDT. Your session was resumed. Check where you were and carry on.
```

The reason in brackets is `crash` or `restart`. `restart` is a planned stop or a deploy, and `crash` is everything
else. The time is when the room went away: the moment of a planned stop, or the last time the card was heard from
before a crash.

## This is a forced turn, on purpose

"a2a stage 1: no forced turns" (`docs/decisions-log.md`) says that atrium does not start a turn that nobody asked
for. A forced turn spends tokens every time it fires. The restart wake (`docs/runtime/restart-wake.md`) stays inside that
rule, because a session queues its wake for itself.

The notice reverses that rule for one case. clint decided it on 2026-09-25. The room interrupted the turn, so the room
gives the turn back and the agent picks its work back up. Without the notice, the work stops until somebody sees the
idle card and types into it. The notice is typed only after the room's own exit, and only into a card that was
mid-turn. Everything else still follows "no forced turns".

## What counts as mid-turn

A card counts as mid-turn when its status was `running` or `needs-permission` when the room went away. `running`
means a prompt was sent and no Stop has come back. `needs-permission` means the turn is blocked on a dialog, and the
restart removes that dialog. A card in `needs-input` finished its turn, and it gets nothing.

The two kinds of exit leave different evidence behind:

- **A planned stop.** The wind-down reads the statuses before it stops any runner, because a runner that exits files
  its card `dead` or `done`. It queues a notice for each supervised card that is mid-turn. Then it writes the
  `room_stopped_at` setting.
- **A crash or a kill.** Nothing runs on the way down, so the card keeps the `running` status that the dead room left.
  At start, before the reaper or a reopen can move any status, the room reads `room_stopped_at` and clears it. If the
  setting is empty, the last room did not reach its wind-down. The room then queues a notice for each card that is
  still `running` or `needs-permission` and has a runner recorded. A card whose process is still alive is skipped,
  because that session outlived the room and so it was never the room's to lose.

## Delivery

The notice is a row in the `restart_wake` table with `queued_by` set to `unexpected-exit`. It uses the restart wake's
gate and typing path, unchanged: a runner that started after the row was queued, a settled SessionStart hook, no
dialog on screen, a turn that is over, an empty line and a quiet keyboard. It is typed behind a grey
`[atrium] unexpected exit:` label, in the same style as the wake and a peer's message. So the prompt that it starts
does not mark the card's turn seen and does not answer its questions.

The card's history records a `notified` event with `by: unexpected-exit` when the notice is queued, and a `prompted`
event with `from: unexpected-exit` when it is typed. While the notice waits, the card shows the `wake queued` chip,
and the tooltip shows the text.

## One line, once per exit

A card has one row, and the notice never replaces a row that is already there:

- A card that queued its own restart wake gets its wake and not the notice.
- A notice that is still waiting is not joined by a second one. A crash loop that kills the room three times before
  the runner comes back leaves one notice.
- A runner that comes back reports SessionStart, which moves the card to `needs-input`. A restart after that, with no
  turn in between, finds nothing mid-turn.

## Switching it off

The room cog has the setting `after atrium goes away mid-turn`. It is on by default. It is stored as
`unexpected_exit_wake` (`on` or `off`) in that room's settings, and the config export includes it. When it is off,
a stop or a start queues nothing, and a notice that is already waiting is dropped without being typed.

## Where it lives

- `internal/daemon/unexpectedexit.go`: the two exit halves, the text and the label.
- `internal/daemon/restartwake.go`: the delivery, shared with the wake. `tryWake` picks the label and drops a notice
  when the setting is off.
- `internal/store/unexpectedexit.go`: `QueueUnexpectedExit`, the planned-stop mark and the setting.
- `internal/api/settings.go`, `internal/api/web/index.html`, `internal/api/web/js/notes-files.js`: the setting.
