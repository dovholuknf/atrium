# Input-lag logging

Typing into a supervised session goes through four places: the board's xterm, the hub on `:7778`, the link to the
room, and the room daemon that writes the pty. When keystrokes lag, this logging says which of the four added the
delay. All of it is off by default and costs one boolean test per keystroke when off.

## Turning it on

**One checkbox for all of it.** Open a terminal's **details** drawer and tick **log terminal input lag** under **debug**. It takes effect at once, with no
restart, in the browser, the hub and the room. In the all-rooms view the hub passes it to every attached room, and a
room that attaches later is told when it arrives. Scoped to one room, it switches that room and the hub. The hub and
the room keep it across a restart. Opening a terminal's details drawer (or the settings) in another browser shows the
box ticked and starts timing there too.

**Browser only.** Without the settings dialog, run this in the console:

```js
localStorage.setItem("atrium.debug.inputlag", "1")   // then reload
```

Output goes to the browser's console. Chrome hides `console.debug` lines by default, so set the level filter to
include **Verbose** to see every keystroke. Slow keys, stalls and the summary show without it.

**Hub and room, pinned at start.** An environment variable set before starting atrium overrides the checkbox for
the life of that process. Use it to pick a threshold other than 20ms, or to keep a machine timing whatever the
checkbox says:

```powershell
$env:ATRIUM_DEBUG_INPUTLAG = "1"     # log any hop over 20ms
$env:ATRIUM_DEBUG_INPUTLAG = "5"     # or pick the threshold in ms
```

The variable stays with the process it was set on. A runner the room launches, a keep-alive fork and a shell opened
on a card do not inherit it, or any other `ATRIUM_DEBUG_` variable, so a worker's `go test` and the atrium binaries
it runs are not timed. To time one runner, name the variable in its `environment` field.

While it is set, settings says so under the checkbox, and the box reaches only the browser. Only a hop over the
threshold is logged, so a healthy session stays quiet. Switching from the checkbox writes one `[inputlag] on from
settings` or `off from settings` line. Every timing line starts with `[inputlag]` and a clock to the millisecond:

```powershell
Select-String '\[inputlag\]' <daemon log>
```

## Reading the browser lines

One line per timed keystroke:

```text
[inputlag] 14:03:22.481 key 212.4ms (send 0.2, wire 205.1, parse 3.0, paint 4.1) | main thread blocked 0ms of it
```

- **send**: `onData` to the frame handed to the socket. Should be under a millisecond.
- **wire**: the frame sent to the first output frame back. This is the hub, the link, the room and the runner.
- **parse**: xterm parsing that output.
- **paint**: the next animation frame after the parse, which is when you see it.
- **main thread blocked Nms of it**: how much of the wait overlapped a long task or a frame gap. When this is
  most of the total, the board's own JavaScript is starving input and the network is not the cause.
- **N more keys while waiting**: only the first unanswered key is timed. The rest are counted.
- **socket had N bytes unsent before this key**: the browser's own send buffer was already backed up when the key
  arrived. Read before the key is sent, so the key's own frame is not counted, and left out when it is zero.

A key over 100ms prints as a warning and adds the fetch counts from `api()`, at send and now. A full
`6/6 in flight` with a queue behind it means a refresh storm coincided with the typing.

Separately:

- `main thread blocked 140ms (long task)` is any block of 50ms or more, whether or not you were typing.
- Every ten seconds, while keys are being timed: `last 300 keys: p50 18.2ms, p95 61.0ms, max 240.3ms`.

## Reading the hub and room lines

The hub cannot see websocket frames: after the upgrade it copies raw bytes. So it times the connection it holds
to the room.

| line | measures |
|---|---|
| `hub <room> echo: frame up -> first bytes back` | the hub's round trip to the room, runner included |
| `hub <room> up: write toward the room took` | the link refusing bytes, which is backpressure |
| `room <task> in: ws frame -> pty write` | frame read to bytes in the pty, split by lock wait and pty write |
| `room <task> echo: ws frame in -> first output out` | the room's round trip, split into `runner` and `atrium` |
| `room <task> out: pty read -> fanout` | lock contention inside the room between the pty and the attachers |
| `room <task> out: ws write` | the room's websocket send blocking |
| `room <task> out: attacher N chunks behind, N bytes dropped` | an attacher too slow to keep up, output lost |

## Finding the slow hop

Match lines by clock. For one slow keystroke:

- **browser wire** minus **hub echo** is the browser-to-hub leg, including the hub's own HTTP stack.
- **hub echo** minus **room echo** is the link between hub and room.
- **room echo** splits itself at the moment the pty handed output over. `runner` is frame in to pty read: the pty
  write, the runner's think and redraw, and ConPTY. `atrium` is pty read to websocket write: the ring, the fan-out,
  the attach queue and the send. A large `runner` with no **room in** line is the runner, or the machine not
  scheduling it. `split unknown` means a later chunk moved the stamp, so fall back to the other lines.
- A large **input lock** is a keystroke waiting behind a peer message being typed in.
- A large **main thread blocked** on the browser line means none of the above. Look at what the board was doing.

## When the numbers do not add up

The room starts its clock when its attach reader takes a frame off the socket. A frame that waits in the socket
before that is counted by the hub and missed by the room. So a slow **hub echo** with no **room echo** line does
not prove the link is slow. It can also mean the room process was not running at that moment.

On Windows that is the usual cause. A busy machine can leave a normal-priority process unscheduled for 15 to
200ms in bursts, and it does not matter what the process is doing. The hub and the room raise themselves to above
normal at start for this reason. `ATRIUM_PRIORITY=normal` turns that off, for comparing.

To tell a slow program from a machine that is not scheduling it, run the hiccup probe at two priorities at the
same time. It only sleeps 1ms in a loop and logs each sleep that overslept by 15ms or more:

```powershell
go test -c -o build.claude/link.probe.exe ./internal/link/
$env:HICCUP = '120'
$p = Start-Process build.claude/link.probe.exe '"-test.run=TestProbeSchedulingHiccups"', '"-test.v"' `
  -WindowStyle Hidden -RedirectStandardOutput hic-normal.out -PassThru
```

Start a second copy with `$p.PriorityClass = 'AboveNormal'` and a different output file. When the normal copy logs
hiccups and the raised one logs none, the stalls are the machine.

A run on 2026-09-29 logged 571 stalls of 15 to 149ms in 120s at Normal and none at AboveNormal. The hub and the room
were already raised, but every runner and the `conhost.exe` or `OpenConsole.exe` behind its pty was Normal, so a
keystroke waited at the runner's redraw. The room now raises each interactive runner, the shell beside a card, and
their console host at spawn. That is the `runner_priority` room setting: `above_normal` (the default) or `normal`, set
in the settings API and read at each spawn, so it applies to the next runner and never to one already running. Only
those processes are raised. Their children are not, and must not be: Windows passes a raised class down only for Idle
and BelowNormal, so the go test and chrome an agent starts stay Normal and keep the machine busy below the key path.
Keep-alive forks, sources and recognisers are not interactive ptys and stay Normal.

`TestProbeTwoPairsSideBySide` in `internal/link/latency_test.go` compares two running hub and room pairs. It
alternates keystrokes between them, so both see the same load at the same moment.
