# The hub restart gate

A hub-only deploy restarts the hub, and a hub restart drops every board pane. The deploy runs from a script that
cannot see the board, so it once bounced a board somebody was typing into. The gate makes the deploy ask first.
The hub answers once nobody is using a board. Every board shows a short countdown first, and a click on it holds
the restart until somebody resumes it.

```
  deploy script                 hub                                  every board window
  ┌───────────────────┐  POST   ┌──────────────────────────┐   SSE   ┌──────────────────────────┐
  │ hub-restart-gate  │ ──────> │ wait for idle            │ ──────> │ "restarts in 5s unless   │
  │ .ps1              │         │ count down               │         │  you click this"         │
  │                   │ <────── │ answer go/paused/busy    │ <────── │ click: pause             │
  │ go: swap binary   │  answer └──────────────────────────┘  POST   │ input: keystroke, click  │
  └───────────────────┘                                              └──────────────────────────┘
```

## What idle means

A board is in use when somebody presses a key, clicks, scrolls, or pastes in it. Every board window reports that
to the hub with `POST /_hub/restart/input`, the popped-out terminals included. The listeners sit on the document
in the capture phase, so a keystroke into an xterm counts even when the terminal stops it. Pointer movement does
not count: reading is not using.

A window reports at most once every three seconds. The hub treats the latest report from any window as the last
input. Idle means no report for the script's idle window, ten seconds by default. The throttle is well under that
window, so a steady typist never looks idle between two reports.

Clicks on the gate's own toasts and on the restarting cover are not input. A click on the countdown is a pause,
and reporting it as input as well would take the countdown down under the pointer.

## Who holds the countdown

The hub holds it. The script only learns the answer, and a board only shows what the hub says.

1. The script sends `POST /_hub/restart` with `countdown`, `idle` and `wait` in seconds. The hub sends the
   response headers at once and holds the request open until it has an answer.
2. With no board open, the answer is `go` at once. There is nobody to warn.
3. Otherwise the hub waits until the boards have been idle for `idle` seconds.
4. The hub sends a `hub-restart` event, state `countdown`, to every board stream. A board scoped to one room
   hears it too: hub events skip the room filter.
5. Input during the countdown takes the countdown down (`cancelled`) and the hub waits for idle again. That is not
   a pause, because nobody asked for one.
6. A click on the countdown toast sends `POST /_hub/restart/pause`. The hub sends `paused` to every board. Each
   board then shows a "hub restart paused" toast with a resume button.
7. At the end of an unpaused countdown, the hub sends `restarting` to every board and answers the script `go`.

The script restarts the hub only on `go`. The gate stops nothing itself.

| Answer | Exit | What it means |
| --- | --- | --- |
| `go` | 0 | Restart now. |
| `paused` | 3 | Somebody paused it, and the pause still held when `wait` ran out. |
| `busy` | 3 | A board stayed in use for the whole of `wait`. |
| 409 | 3 | Another deploy is already waiting for an answer. |
| 404 | 0 | The hub is older than the gate. It is restarted the old way, with no warning. |
| no answer | 0 | No hub is listening. There is nothing to warn and nothing to stop. |

## The pause

A pause holds until somebody resumes it. It also holds against later deploys: an ask made while paused waits
for its `wait`, then answers `paused`.

The paused toast is sticky. It has no timer and no dismiss button, and it goes back on the stack if the toast cap
pushes it off, because it is the way out of the pause. A window that opens during a pause reads
`GET /_hub/restart` when its stream opens and shows the toast without an event.

Resume is the button on that toast. It sends `POST /_hub/restart/resume`. Every board then takes its paused toast
down. The resume click counts as input, so a deploy still waiting gets the full idle window and a fresh countdown.
It does not restart the second the button is pressed.

The pause is held in memory. A hub that restarts for any other reason has lost what the pause guarded, so the
pause goes with it.

## Several windows

Every window holds its own event stream, and the hub says the same thing down each one. So every window shows
the countdown, a click in any one of them pauses all of them, and resume from any one resumes all of them. The
hub counts open streams to answer "is a board open". A window whose stream is reconnecting is not counted.

## The restarting cover

On `restarting`, the board opens a modal dialog, "the hub is restarting". Everything under it is inert, so a
keystroke typed while the hub is away cannot land half way. Escape does not close it. Neither does a click
outside it, or anything that closes every open dialog. The board also arms the terminal's restart wait, so the
attached pane waits for its socket to come back rather than tearing down.

The cover comes down when the event stream opens again. A stream only reopens after it drops, so a reopen means a
new hub answered. A new board build reloads the page anyway (see `docs/reload-design.md`). If the stream is still
live 90 seconds after `restarting`, the old hub never went. The cover comes down and a toast says the hub did not
restart.

## Loopback and auth

`POST /_hub/restart` is refused unless the caller is on loopback. It restarts nothing, but it is the deploy
script's door, and nothing that reaches the hub over an overlay is a deploy script. The board's `input`, `pause`
and `resume` calls are not guarded. They can only hold a restart back, and the phone board is where a pause is
most likely to be clicked. There is no auth, the same as the rest of the hub.

## The deploy script

`scripts/hub-restart-gate.ps1` asks and exits 0 for go, 3 for held. A deploy calls it before it stops the hub:

```powershell
& D:\worktrees\claude\atrium\orchestrator\scripts\hub-restart-gate.ps1
if ($LASTEXITCODE -ne 0) { Say 'restart held by the board, nothing changed'; exit 0 }
```

Its defaults are a 5 second countdown, a 10 second idle window, and a 300 second wait. The hub caps them at 60,
600 and 3600. The request times out at `wait` plus the countdown plus 30 seconds.

## What this does not do

- **No room restarts.** A room restart kills its runners. This gate is for the hub only.
- **No schedule.** The gate answers a deploy that somebody started. It never starts one.
- **No queue.** One deploy asks at a time, and a second is refused out loud.
