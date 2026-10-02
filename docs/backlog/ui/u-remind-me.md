# u-remind-me. Click "remind me about this", no reminder ladder

Status: board half built, pause exception approved by clint 2026-10-02. The hub half (question and ready raise once, a due reminder raises once) is owned by @fabric.

## What changed

- No new endpoint. "Remind me" is the existing snooze, `POST /_hub/growls/{id} {"do":"snooze","minutes":N}`, max 10080.
- Floating growler and phone page `/m`: the "snooze" button reads "remind me"; options 15 min, 1 h, tomorrow 9am (the next day's 9am local, clamped 1..10080 minutes).
- Bell: a log row whose growler is still open in `growlSet` gets a "remind me" menu with the same options, on the newest row for that growler only. A reminded growler shows "reminding in X". An ended growler's rows get nothing. This is what most browsers see, since the floating growler is off by default.
- `recordToLog` takes an optional growler id, kept on the log entry as `growl`, to map a row to its growler.
- `growlRemind(g, minutes)` is the one place the minutes are worked out; the growler button and the bell both call it.

## Tests

`growlRemind` in `scripts/test-board-headless.js`, run alone with `growlOff`, `growlActions`, `growlPhone`, `growlStack` and `bootClean`.
