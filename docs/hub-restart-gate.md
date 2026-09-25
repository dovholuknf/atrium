# The hub restart gate

A hub-only deploy restarts the hub, and a hub restart drops every board pane. The deploy runs from a script that
cannot see the board, so it once bounced a board somebody was typing into. The gate makes the deploy ask first.
The hub answers once nobody is using a board. Every board shows a short countdown first, and a click on it holds
the restart until somebody resumes it.

```
  deploy script                 hub                                  every board window
  ┌───────────────────┐  POST   ┌──────────────────────────┐   SSE   ┌──────────────────────────┐
  │ hub-restart-gate  │ ──────> │ wait for idle            │ ──────> │ "atrium restarts in 5s"  │
  │ .ps1              │         │ count down               │         │  with a pause button     │
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

1. The script sends `POST /_hub/restart` with `countdown`, `idle`, `wait` and `hold` in seconds. The hub sends
   the response headers at once and holds the request for up to `hold` seconds. With no answer by then it
   answers `waiting`, with the ask's id and whether it is paused, and the script asks again with that id. See
   "Re-polling" below.
2. With no board open, the answer is `go` at once. There is nobody to warn.
3. Otherwise the hub waits until the boards have been idle for `idle` seconds.
4. The hub sends a `hub-restart` event, state `countdown`, to every board stream. A board scoped to one room
   hears it too: hub events skip the room filter.
5. Input during the countdown takes the countdown down (`cancelled`) and the hub waits for idle again. That is not
   a pause, because nobody asked for one.
6. A click on the countdown toast sends `POST /_hub/restart/pause`. The hub sends `paused` to every board. Each
   board then shows a "restart on hold" toast with a resume button.
7. At the end of an unpaused countdown, the hub sends `restarting` to every board and answers the script `go`.

The script restarts the hub only on `go`. The gate stops nothing itself.

A `go` says which one it is, in `why`, and the script prints it after `go:`. The audit line says it after
`restarting:`.

- `no board is open`: step 2. Nobody was warned, because nobody was there.
- `counted down on N board stream(s) and nobody paused`: step 7. Every one of those streams got the countdown and
  the restarting cover.

Treat the second as proof that a board was counted. The countdown lasts a few seconds and the board then reloads
onto the new build, so a board nobody was looking at keeps no trace on screen. Its toast log still holds the
"hub restart" entry.

| Answer | Exit | What it means |
| --- | --- | --- |
| `go` | 0 | Restart now. `why` says whether a board saw a countdown. |
| `busy` | 3 | A board stayed in use for the whole of `wait`. |
| 409 | 3 | Another deploy is already waiting for an answer. |
| hub gone | 4 | The hub stopped answering, or answered 410, while the ask was waiting. |
| 404 | 0 | The hub is older than the gate. It is restarted the old way, with no warning. |
| no answer | 0 | No hub is listening. There is nothing to warn and nothing to stop. |

## The pause

A PAUSE HOLDS UNTIL SOMEBODY RESUMES IT, with no timeout. `wait` is how long the boards may stay busy before the
deploy gives up. It does not run while the restart is paused, and a resume starts it over. The pause also holds
against later deploys: an ask made while paused waits, with no timeout, until the resume.

A pause made with no deploy waiting, because the script was already gone, holds for the next deploy the same way.

Both of the gate's toasts are sticky: the countdown and "restart on hold". Neither has a timer or a dismiss
button. The toast cap does not count them and never evicts them, so the stack holds up to three ordinary toasts
beside them, one on a phone. Anything else that takes one off the stack sees it put back. A toast comes down only
when the hub says what comes next: the countdown goes on `restarting`, `cancelled` or `paused`, and the paused
toast goes on `resumed` or `restarting`. A window that opens during a pause or a countdown reads
`GET /_hub/restart` when its stream opens and shows the toast without an event. A stream that reopens leaves a
countdown already on screen counting.

Resume is the button on the paused toast. It sends `POST /_hub/restart/resume`. Every board then takes its paused
toast down. The resume click counts as input, so a deploy still waiting gets the full idle window and a fresh
countdown. It does not restart the second the button is pressed.

## Re-polling

No request is held for the whole of a pause. The script sends `hold`, 25 seconds by default, and the hub holds
each request for at most that long. It then answers `waiting` with the ask's id, and the script asks again with
`{"ask": "<id>", "hold": 25}`. The ask lives in the hub between requests. It ends when the script collects its
answer. It also ends when no request has polled it for 30 seconds: the script is gone, so nobody would restart on
a `go`. Its countdown then comes off every board. A pause stays.

The script exits 4 when a poll finds no hub, or when the hub answers 410 because it has no such ask. A hub that
restarted has forgotten every ask, and in both cases nobody said go.

A request with no `hold` is the older script's. The hub holds that one request until it has an answer, and its
ask is not patient: a pause held past `wait` still answers `paused`, exit 3. Held for ever, that request would hit
the older script's own timeout, which it reads as no hub, which is a `go`.

The pause is held in memory. A hub that restarts for any other reason has lost what the pause guarded, so the
pause goes with it.

## Several windows

Every window holds its own event stream, and the hub says the same thing down each one. So every window shows
the countdown, a click in any one of them pauses all of them, and resume from any one resumes all of them. The
hub counts open streams to answer "is a board open". A window whose stream is reconnecting is not counted.

Every spelling of the stream counts, and every one hears the countdown. The hub never proxies the event stream
to a room: `/v1/events/hub`, `/v1/events/room/<name>`, and a bare `/v1/events` scoped by `X-Atrium-Room`, by
`?atrium_room=` or by a hub with one room attached all end in the same list of streams the gate counts and
speaks to. That holds for popped-out windows and the phone too. The board's `input`, `pause` and `resume` calls
sit under `/_hub/`, which the hub answers itself before it looks at a room.

## The restarting cover

On `restarting`, the board opens a modal dialog, "atrium is restarting". Everything under it is inert, so a
keystroke typed while the hub is away cannot land half way. Escape does not close it. Neither does a click
outside it, or anything that closes every open dialog. The board also arms the terminal's restart wait, so the
attached pane waits for its socket to come back rather than tearing down.

THE COVER COMES DOWN ONLY ONCE THE NEW HUB IS UP AND THE BOARD HAS CAUGHT UP. A stream reopening does not prove
that: the old hub's stream can blip and come back before the old hub goes. So `GET /_hub/restart` names the hub
process, in `boot`, a random id each hub makes when it starts. A board keeps the name it last heard. On every stream
open while the cover is up, it asks again, and only a different name is the new hub. The board build cannot stand in
for the name, because a deploy that changes only Go code keeps the same build. A hub too old to give a name is
taken as gone once the stream has dropped and reopened.

Once the new hub answers, the board reads `/v1/health`. A new board build reloads the page there (see
`docs/reload-design.md`), with the cover still up. Otherwise the board waits for one full refresh that began after
that point, and then the cover comes down.

A reload blanks the page whatever waits for what, so the cover outlives it instead. While the cover is up, the
board writes it to the tab's `sessionStorage`. A page that loads with it written down puts the cover straight back,
before the board under it draws, with its clock still counting from `restarting`. It then waits for the new hub in
the same way. This covers the new-build reload and a reload by hand.

Nothing else takes the cover down. Its `close` is refused while it is up, so a view switch, a toast click or
anything that closes every open dialog leaves it where it is, with no frame between.

A window whose stream missed `restarting` keeps the countdown at `0s` while the hub is away. When its stream reopens
onto a hub with a different name, the cover takes over from the countdown and comes down the same way.

A stop nobody announced is not this cover's. The board covers that with `atrium is not running` instead, and a
reload while atrium is down gets the service worker's `down.html`. Neither shows while the countdown or this cover
is up. See `js/down.js`.

If the stream is still live 90 seconds after `restarting` and the hub still gives the old name, the old hub never
went. The cover comes down and a toast says the hub did not restart.

## Loopback and auth

`POST /_hub/restart` is refused unless the caller is on loopback. It restarts nothing, but it is the deploy
script's door, and nothing that reaches the hub over an overlay is a deploy script. The board's `input`, `pause`
and `resume` calls are not guarded. They can only hold a restart back, and the phone board is where a pause is
most likely to be clicked. There is no auth, the same as the rest of the hub.

## The deploy script

`scripts/hub-restart-gate.ps1` asks and exits 0 for go, 3 for held, and 4 when the hub went away while the ask
was waiting. A deploy calls it before it stops the hub:

```powershell
& D:\worktrees\claude\atrium\orchestrator\scripts\hub-restart-gate.ps1
if ($LASTEXITCODE -ne 0) { Say 'restart held by the board, nothing changed'; exit 0 }
```

Its defaults are a 5 second countdown, a 10 second idle window, a 300 second wait and a 25 second hold. The hub
caps them at 60, 600, 3600 and 60. The first request times out at `wait` plus the countdown plus 30 seconds,
because a hub older than re-polling holds it for the whole wait. Each poll times out at `hold` plus the countdown
plus 30 seconds.

## What this does not do

- **No room restarts.** A room restart kills its runners. This gate is for the hub only.
- **No schedule.** The gate answers a deploy that somebody started. It never starts one.
- **No queue.** One deploy asks at a time, and a second is refused out loud.
