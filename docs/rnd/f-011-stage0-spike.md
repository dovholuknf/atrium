# f-011 stage 0: the pty host spike

Run by @terminal's worker `f-011a` on sg4 (Windows 11), 2026-09-29. Tests `docs/rnd/rolling-restart-design.md` section 3.
This is a throwaway. The code is `cmd/ptyhost-spike/`, a standalone command (not a subcommand of `atrium`, and nothing
in the supervisor or the cobra root knows it exists). It builds and vets on `windows` and on `linux`. It is a reference,
not a start on stage 1.

**Verdict: the design stands, with changes.** Every claim about the host that could be run here held. Six changes are
needed and two of them (the gaps in 3.2 and in 7) would have shipped a bug. Two checks could not be run as written
(6 as a real Task Scheduler task, 8 in sg4-wsl) and one is an open gate (launchd). Details follow.

## What was built

- One binary, `ptyhost-spike`, with modes `host`, `start`, `parent`, `client` and (Windows) `jobtest`.
- `host` owns ONE pty via `github.com/aymanbagabas/go-pty` (the supervisor's own package) running one command. A
  goroutine drains the output into a bounded ring ALWAYS, connected or not. The ring keeps the absolute offset of the
  next byte and the list of size cuts `(offset, cols, rows)`.
- Verbs: `hello`, `list`, `attach {from}`, `write`, `resize`, `signal`, `collect`, framed as one JSON object per line
  with bytes in base64. `attach` answers with the retained bytes from `from`, a `truncated` flag and the cuts in force
  from there, then streams `{out: offset, data}` frames. A `resize` records its cut at the current offset BEFORE it
  applies the pty resize. `collect` is refused until the runner has exited, and the host exits after it.
- Windows: a named pipe `\\.\pipe\atrium-spike-<name>`, created with the DACL `D:P(A;;GA;;;<current user SID>)`,
  `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, written by hand on `golang.org/x/sys/windows` so the
  spike adds no dependency. Linux: a unix socket, bound under `umask 0177` and chmod 0600.
- `start` launches the host with `internal/cli/spawn_windows.go`'s flags (`DETACHED_PROCESS |
  CREATE_NEW_PROCESS_GROUP | CREATE_BREAKAWAY_FROM_JOB`, retry without breakaway on access denied), `Setsid` on Linux.
- The client is one connection per invocation, and an `attach -script` mode that sends commands on the same connection
  it is streaming from, which is how a "daemon" both watches and types.

Isolation: pipe names `atrium-spike-*`, task names `atrium-spike-f011a-tmp-*`, files under the worker's scratchpad,
`ATRIUM_LOCATION` and `ATRIUM_SHARED_LOCATION` pointed at a file that does not exist (not cleared: cleared, the hooks
fall back to the machine's default location file, which is the live room). The claude that ran had
`ATRIUM_PERM_GATE=off` and `ATRIUM_HUB_URL=http://127.0.0.1:1`. It ran `--model claude-haiku-4-5-20251001` in an empty
scratch directory and was told it was a test. Nothing touched 7777, 7778, the live database, or a real task.

## The checks

### 1. A client attaches, types into claude, sees the reply. PASS

Host started detached running `claude` at 120x40. `attach from 0` replayed 172 bytes (the trust dialog), a Down and
Enter were written, claude drew its banner. A prompt asking for 80 numbered lines was typed and claude answered with
`Line 1` to `Line 80`, all of it arriving on the client.

One protocol fact this found, not a bug: the text and the Enter must be two `write` frames with a pause between. Sent as
one, claude's input box takes the Enter as part of a paste and does not submit. The design already says frames are
"never split or merged", which is what makes this hold, and the stage 1 supervisor must keep sending them apart as it
does today.

### 2. Kill the client mid-reply. PASS

Client A was killed after receiving 9234 bytes, with claude mid-reply. `list` one second later: `out_offset` 13165.
Ten seconds later with no client connected: 14814. So claude kept working, the ring kept growing, and the pty never
blocked. (The same holds for the pwsh runner: 2078 to 3013 to 3938 bytes across client kills.)

### 3. Reattach from the last offset, and from 0. PASS

- Client B `attach from 9234`: header `from 9234`, 5580 bytes replayed, next offset 14814.
- Client C `attach from 0`: 14814 bytes. `A + B` against C: 14814 bytes each, first difference none. The 80 `Line N`
  numbers in `A + B`: 80 found, each once, in order. No byte duplicated or skipped.
- Attaching from 0 with a ring that wrapped (pwsh runner, 4096 byte ring, 5320 bytes written): header
  `from 1224, truncated true, ring_start 1224`, 4096 bytes, and the cuts rebased to the retained window:
  `[{1224, 100x30}, {3638, 80x24}]`. The whole ring, the flag, and the size cuts.

### 4. Typing and resize after reattach. PASS

Client D attached from the current offset and typed `Now reply with exactly the single word PONG`. claude answered
`● PONG`. Then `resize 90 30`: the host answered `out_offset 22008` and `list` reported 90x30. Attaching from 22008
gave `cuts [{22008, 90x30}]` and the first bytes there are `ESC[?25l ESC[H` then a full repaint at the new width
(`Line 70`, `Line 71`, ...). So the cut precedes the first byte at the new size. See gap G3 for what this does not prove.
The pwsh runner agrees: `[Console]::WindowWidth` answered 80 after `resize 80 24`, and the cuts were
`[{0,100x30}, {3638,80x24}]`.

### 5. The runner exits with no client connected. PASS

Client E typed `/exit` and disconnected at 23:06:19.9. claude exited at 23:06:25.6 with nobody connected. A new client's
`list` showed `exited true` (in the pwsh run: `exit_code 7`, and 0 for claude). Its `attach from 25085` returned the
2812 bytes that were written after that offset, including claude's last screen, then an `EXIT code=0` frame. `collect`
answered ok, the host exited 200 ms later, and the pipe was gone (`dial: The system cannot find the file specified`).

A note for the wire: my `list` used `omitempty` on `exit_code`, so an exit code 0 was invisible. `exited` and
`exit_code` must always both be sent.

### 6. Job objects on sg4. PARTIAL: could not be run as a real Task Scheduler task

**A Task Scheduler task cannot run as the `claude` account on sg4.** `scripts/atrium-autostart.ps1` registers
`InteractiveToken`, and that registers here but never fires: `schtasks /Run` returns SUCCESS and the task stays `Ready`
with `Last Result 267011` (`SCHED_S_TASK_HAS_NOT_RUN`), because `quser` shows one session, `clint` on the console, and
none for `claude`. `atrium2-watchdog`, an existing task on this box, has the same never-run result. `S4U` (which needs
no session) is refused at registration: `ERROR: Access is denied`. So the real room here is not started by a task
running as this account, and its job policy cannot be read. I did not touch that task or the room.

Substitute, which answers the design's actual question: the harness `jobtest` creates a job object with
`KILL_ON_JOB_CLOSE`, starts `parent` in it, then closes the job, which is what ending a task does to its members. Three
policies for breakaway:

| Job allows | `CREATE_BREAKAWAY_FROM_JOB` | Host after the job closed | claude-shaped runner |
| --- | --- | --- | --- |
| nothing | refused, `Access is denied`, retried without | DEAD | dead (heartbeat stopped at 175 bytes) |
| `BREAKAWAY_OK` | honoured, host started outside | alive | alive (315 to 420 bytes) |
| `SILENT_BREAKAWAY_OK` | honoured | alive | alive (315 to 385 bytes) |

So `spawn_windows.go`'s flags do what the design says, and the retry path does what the design says ("dies with the
task, no worse"). Also read directly: the shell an agent runs in on sg4 is in a job with `LimitFlags 0x1800`
(`BREAKAWAY_OK` and `SILENT_BREAKAWAY_OK`, no kill on close), so a host started from an agent shell breaks away.

**What is still unknown:** whether Task Scheduler's own job allows breakaway. That needs a machine where a task really
runs, which is any Windows box with an interactive logon for the account. It is an open gate for stage 3, listed under
"Open gates". See gap G5, which says why it matters more than the design thinks.

### 7. ConPTY owned by a console-less detached process. PASS

Every host in these runs was started `DETACHED_PROCESS`, so it had no console. claude ran correctly under it: banner,
trust dialog, resize repaint, the reply and `/exit` all worked.

### 8. systemd. NOT RUN (recorded as option a)

Not runnable on sg4 as `claude`. `wsl -l -v` for this account lists one distro, `docker-desktop`, stopped, and I did not
start it. sg4-wsl is `clint`'s distro and a live room, so the design says never. @terminal ruled: record it as not
runnable here, write the steps down, and try a throwaway Linux box only if there was room. There was not, so it was
not tried. Probed only: `ssh cdwsl` is refused (localhost port 22), and `ssh cdzrok` (Ubuntu 22.04 EC2, user `ubuntu`)
answered `systemctl --user is-system-running` with `running` and has `systemd-run`, so it is a ready home for this test.
Nothing was created there.

What is done: the Linux build compiles and vets (`GOOS=linux go vet ./cmd/ptyhost-spike`). The unix socket path
(`pipe_unix.go`, `detach_unix.go`) has been compiled and never executed. What would run it, on any Linux with
`systemd --user` and `linger` on, taking about an hour:

1. `GOOS=linux go build -o build.claude/ptyhost-spike ./cmd/ptyhost-spike`, copy into the distro.
2. A unit `atrium-spike-f011a.service` with `ExecStart=ptyhost-spike parent -name x -- bash`, default `KillMode`. Start
   it, `systemctl --user stop` it, and check whether the host (and its `bash`) survived. Expected: dead, the section 7
   hazard.
3. Repeat with `KillMode=process`, and with the host started by `systemd-run --user --unit atrium-spike-f011a-host
   ptyhost-spike host ...`. Expected: both survive. Recommendation from reading only, not evidence: `systemd-run --user`
   from the daemon, because it needs no change to the room's unit and works for a room whose unit an operator wrote, but
   it makes the host depend on `systemd-run` being on PATH and a user manager running, and `KillMode=process` also leaves
   every other stray process of the room behind on stop. Pick after step 3.
4. Remove both units and `systemctl --user daemon-reload`.

If the distro has no user session: `[boot] systemd=true` in `/etc/wsl.conf`, `wsl --shutdown`, and
`loginctl enable-linger <user>`.

## Findings that are not in the design

Named plainly, by section.

**G1. Section 3.2, "a new `hello` closes the older connection", evicts the live daemon on any probe.** My first host
closed the older connection when ANY connection arrived, and my own test client's `list` killed the attached client.
The design's discovery says a name "only counts once `hello` answers on it", so a second daemon start, `atrium status`,
a stale-socket scan, or the daemon's own reconnect probe would send `hello` and evict the daemon that is running.
Change: two verbs. `probe` (read only, never evicts, answers `proto`, `build`, `owner pid`) for discovery, and
`hello {takeover: true}` as the only thing that closes an older connection, and only after the owner is shown to be
dead or the operator chose to.

**G2. Section 3.2, "one connection, a control stream and multiplexed pty streams", needs overlapped I/O on Windows.** A
named pipe handle opened without `FILE_FLAG_OVERLAPPED` serialises every call on it: a blocked `ReadFile` stops a
`WriteFile`. A daemon that is streaming output and typing on one connection would deadlock or stall. The spike used
overlapped I/O by hand, which works. Stage 1 should use `github.com/Microsoft/go-winio` (not in `go.mod` today) or the
same code, and must say so.

**G3. Section 3.2, "the cut and the first byte at the new size can never be out of order", is stronger than the host can
know.** The cut is recorded at the ring's write position when the `resize` verb runs, but the drain is asynchronous:
bytes the runner wrote at the old size may still be in the pty's pipe and land after the cut, and ConPTY applies the
resize asynchronously too. The evidence in check 4 (the repaint begins right after the cut) shows the common case. The
guarantee is really "the cut is never after bytes written at the new size", and a cut can be a few bytes early. That is
today's behaviour in the supervisor (same race), so it is not a regression, but the design should not promise more.

**G4. Section 3.3 and 3.2, the host never says what happens to a slow reader.** A subscriber that stops reading must
not block the drain. The spike gives each attached client a 4096-frame queue and, when it overflows, closes that client,
which reattaches from its last offset. Say so in the protocol. Related: the exit event must be published after the drain
has finished, or the tail is short. The spike waits for the drain to reach EOF or 1.5 s and files the exit after that.
Whether ConPTY's read side reaches EOF by itself after the child exits was not separately measured, and the 1.5 s cap is
what makes the tail complete either way. Stage 1 should measure it and drop the cap if it is unneeded.

**G5. Section 4.6 and 7, how a restart is done can end the host.** The design says a restart is `restart_atrium` or
`provision-room.ps1 -Restart`. If the room is a scheduled task, ending and re-running the task closes its job, and the
host is in it unless breakaway succeeded. The table under check 6 shows the failure exactly: with breakaway refused, the
host dies with the task and takes every runner, in the very operation the feature exists for. The design's answer, "no
worse than today", is true and beside the point: it is worse than the operator believes. Change: the daemon records
whether the host started outside the job (the spike's `jobReport` shows how: `IsProcessInJob` and
`QueryInformationJobObject` with a null job handle, and the retry path in `startDetached` already knows which happened),
puts that on the board next to the host build, and a restart that would end the job refuses to proceed when the host is
inside it. Otherwise every operator on a task that forbids breakaway is one restart from losing every agent.

**G6. Section 7, Windows pipe names.** The pipe namespace is flat and global, so "derived from the state dir" has to
mean a hash of its full path, and two users on one machine need the user's SID in the name. The pre-created instance
also matters: my Accept creates the next instance only after the previous connection returns, so a client that dials in
that gap gets `ERROR_FILE_NOT_FOUND` or `ERROR_PIPE_BUSY`. The client retries for 5 s and that hid it. The host should
always have one instance waiting.

**G7. Handle hygiene in the host, learned by crashing it.** `pipeConn.Close` was called from two paths and the second
`CloseHandle` hit a value the runtime had already reused: `GetQueuedCompletionStatusEx failed (errno=735)`, `fatal
error: netpoll failed`, the host gone. In a host that owns every runner that is the worst failure there is. It is the
same hazard `internal/daemon/CLAUDE.md` records for `closePTY`. Every handle in the host closes through one function with
a `sync.Once`, and the stage 1 tests should close a connection from two goroutines.

Smaller: the DACL was applied (the log line prints it) but I could not connect from a second account to test the
refusal. A `truncated` replay must also say the offset it starts from, which the spike does (`from`). Not measured:
memory or CPU of the host with many ptys, `write` throughput, and behaviour when the daemon's frame is larger than the
pipe buffer (writes loop, and the spike never sent more than a few KB).

## Open gates

- **Open, half finished: check 8 on cdzrok.** Steps 1 to 4 under check 8 can run there as user `ubuntu` with a uniquely
  named unit in a scratch directory. Not done, so the `KillMode` hazard is still from reading, not evidence.
- **The Task Scheduler path itself stays untested**, because only `clint` has a session on sg4. The
  `KILL_ON_JOB_CLOSE` job in check 6 is a fair substitute for the job semantics, not for the scheduler.
- **macOS launchd, m1mini: OPEN GATE.** Not run, m1mini was offline. Per @rnd this must pass on m1mini before the host is
  turned on for m1mini (it is opt-in per room anyway), and it does not hold stage 1. What to check: a LaunchAgent that
  runs `ptyhost-spike parent`, then `launchctl bootout` it, and see whether the `Setsid` host and its runner survive.
- **Task Scheduler's own job policy** (check 6 above), on a Windows machine with an interactive logon for the account.
  Does not hold stage 1. Holds stage 3.
- **systemd** (check 8 above). Does not hold stage 1. Holds stage 3.

## What the spike created and removed

Created: pipe names `atrium-spike-*` (gone with their hosts), scheduled tasks `atrium-spike-f011a-tmp-break` (twice,
InteractiveToken, then S4U which failed to register), both deleted (`schtasks /Query` finds no `spike`), copies
`spike-task-*.exe` and `spike-jt-*.exe` under the scratchpad, one throwaway claude (ended by `/exit`), several pwsh
runners. Processes after the last run: none (`Get-Process` for `spike-*` and `ptyhost-spike` is empty, and no heartbeat
pwsh is left). No systemd unit was created because none could be. The real room's task, the live daemon, its ports and
its database were not touched.

## Verdict

**The design stands, with changes.** The core claims hold on real hardware with a real claude: a detached, console-less
process can own a ConPTY, keep it drained while nobody is attached, and let a new client attach from an absolute offset
with no byte lost or repeated, type, resize and collect an exit. Required changes before stage 1 is built:

1. A non-evicting `probe` for discovery, and takeover only through an explicit verb (G1).
2. Overlapped I/O for the Windows pipe, stated in 3.2 (G2).
3. Soften the resize-cut guarantee to what a host can know (G3).
4. Specify the slow-reader policy and publish an exit only after the drain (G4).
5. Record whether the host got out of the job, show it, and refuse a restart that would end it (G5).
6. Pipe naming, always one instance waiting, and one idempotent close for every handle (G6, G7).
