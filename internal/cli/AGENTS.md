# internal/cli

Every cobra subcommand. Most are thin faces over an HTTP route on a room or the hub, and the work is done there. The
parts that are NOT thin are the hooks, the processes that must outlive their parent, and the seams where packages that
must not import each other are wired together. Defaults (ports, directories) are in `atrium_defaults.go`.

## The hooks: a hook must never fail a session

`hook.go`, `session.go`, `turn.go`, `hook_permission.go`, `hook_nopoll.go`, `hook_leanagents.go`, and `merged.go`
for git. The rules are `docs/runtime/hooks.md`, "rules every atrium hook follows", and `hook.go` enforces them: exit 0
whatever happens, one attempt, a bound on everything, silence on success.

- **`hook` runs before every tool call in every session.** Its timeout is the operator's latency. Do not add a retry.
  Do not raise the timeout.
- **The permission hook fails OPEN.** Every failure returns nil, prints nothing and exits 0, and Claude Code runs its
  own permission flow. A daemon that is down lets work through rather than bricking every session.
- **`turn` is the one hook that writes to stdout**, because that is how a Stop hook says anything, and a broken one
  is a session that will not stop. Three things hold that line and none may be removed: every failure prints
  `{"continue":true}` and exits 0, `stop_hook_active` is honoured so it never blocks twice, and the daemon's answer
  passes through only when it parses as one of the two shapes a Stop hook may return.
- **The guard (`hook_guard.go`) is the exception that DENIES.** It is off unless `ATRIUM_GUARD` says on, and a broken
  override file or a check that panics denies. What fails open is what Claude Code decides: a timeout, a missing
  payload.
- **`merged` never fails a merge.** Every error is one line on stderr and the exit code is still 0.
- **`turnquestions.go` never sends what a session said.** It keeps only the numbered lines of the last
  `Open Questions:` block, reads a bounded tail of the transcript, and a missing or huge transcript is no questions.
- **`ready` is NOT silent and a refusal exits non-zero**, unlike the hooks. An agent that believed an ack landed would
  wait on a clear that never comes.

## Processes that must outlive their parent

- **`atrium run` is two processes on purpose** (`atrium_runroom.go`). The hub restarts freely and the room does not.
  When no room answers, it starts `atrium room` detached. Stopping or restarting `atrium run` never touches the room.
- **A restart from inside a session is scheduled, never immediate** (`control.go`). A tool call that kills its caller
  never returns, so the tool spawns a detached restarter, answers "scheduled", and the turn ends first.
- **A restart parks other agents first** (`control_park.go`). Only sessions atrium owns. One in a terminal somebody
  opened themselves survives a restart untouched.
- **A room restarting for its hub passes ALL of `roomLaunch` on** (`roomrestart.go`). Leaving one out starts a
  different room: without `--dir` it reads the default key directory, which may hold another room's name and hub.
- **Detaching lives in `internal/detach`.** Read its comment before changing the flags.
- **A binary swap is a rename-aside within the same directory** (`upgrade.go`, `control.go`). Windows will not delete
  an open executable but permits that rename.

## Seams, and why they are here

`internal/link` and `internal/daemon` do not import each other, and `internal/link` never learns the hub has a
database. This package is where they meet:

- `atrium_inventory.go` joins the hub's durable room record (`internal/hubstore`) with the live connection list
  (`internal/link`). Here and nowhere else.
- `atrium_audit.go`, `atrium_notify.go`, `atrium_growl.go` adapt the hub's store to the interfaces `internal/link` asks
  for.
- `roomrelay.go` is the daemon's relay over the room's link.
- `transport.go` is the only place that knows there is more than one transport.

## Landmines

- **`boardAddress` (`stop.go`): the recorded address beats the default port.** A hardcoded 7778 was wrong the moment a
  daemon ran anywhere else, in a way that read as the daemon being down.
- **A second daemon writing the same address file steals every hook on the machine** (`preview.go`). `atrium preview`
  exists so nobody has to remember `--location-file`. A preview copies the database with its `-wal` and `-shm`, never
  shares it.
- **`atrium rooms ...` opens the hub's store directly**, even while a hub runs on it (SQLite WAL). That is on purpose,
  so adding a room works with the hub down. `rooms git sync|collect|init` are the exceptions and go to the running hub.
- **No `rooms git` verb takes a branch, a URL or a refspec**, except `init`, which takes a forge URL on the hub's
  machine.
- **There is no `--zrok-public` on `room join`.** The room link is never fronted by a public URL.
- **`errAlreadySaid` marks a failure the command already explained.** Wrap it only on paths that printed a human
  sentence, so cobra does not bury it under usage. A command that fails silently must still be reported.
- **A bare alias reaches whoever holds it NOW** (`cardverbs.go`). An alias moves to a new card once the old one is done.
- **`dispatch` validates nothing against this machine.** The runner and directory belong to the target room, which
  refuses in its own words.
- **Process ancestry is one snapshot on Windows** (`ancestry_windows.go`), not WMI, which cost seconds per session
  start. The native claude install is named for its version, so the walk also checks `argv[0]`.
- **The hub and room raise themselves to above normal on Windows** unless `ATRIUM_PRIORITY=normal`. Not high, and the
  runners they start are not raised here.
- **`atrium control` must be registered against an INSTALLED binary**, never `build.claude/`, which a running MCP
  server holds open. See the root `AGENTS.md`.

## The files

| | |
| --- | --- |
| `cli.go` | the cobra root and `errAlreadySaid` |
| `atrium_defaults.go` | ports and directories |
| `atrium_run.go` / `atrium_runroom.go` / `roomrun.go` | the hub, the hub plus a room, and the room |
| `roomjoinflags.go` / `transport.go` | how a room reaches its hub, and choosing a transport |
| `roomrestart.go` / `roomdetach.go` / `upgrade.go` | a room restarting itself, starting over ssh, installing a new build |
| `atrium_rooms*.go` / `atrium_inventory.go` | the hub's side of rooms, from a terminal |
| `atrium_backups.go` / `atrium_builds.go` / `atrium_share.go` | restoring the hub's store, builds to offer rooms, the board on zrok |
| `hook*.go` / `session.go` / `turn.go` / `turnquestions.go` | the hooks |
| `adapter_test_cmd.go` / `adapter_validate.go` | `atrium adapter test`, the runner contract conformance kit and its schema check |
| `merged.go` | git's `post-merge`, which never fails a merge |
| `control*.go` | the stdio `atrium control` MCP server and its tools. Many tools are the hub's, reached through the room |
| `peer.go` / `cardverbs.go` | `peers`, `tell`, and naming a card by handle from a shell |
| `finish.go` / `help.go` (`atrium ask`) / `ready.go` | an agent saying it is done, stuck, or that its handoff is written |
| `wait.go` | `atrium wait`: block until the board, the room link and named rooms answer |
| `launch.go` / `dispatch.go` / `open.go` / `close.go` | starting work here, queueing it for a room, opening a link, closing a card |
| `stop.go` / `preview.go` / `tenant.go` | stopping without killing, a throwaway board, naming an atrium |
| `ancestry*.go` | finding the runner a hook is under |
| `ptyhost.go` | `atrium ptyhost`. See `internal/ptyhost` |
| `replay.go` / `termattach*.go` | rendering a captured stream, and a card's terminal in this terminal |
| `roomdb.go` / `ledger.go` / `usage.go` | offline database compaction, the work ledger, usage backfill |
| `landcheck.go` | `atrium land-check`: a hub branch against claude/main, in one call |
| `merge.go` | `atrium merge`: asking the hub's merge queue for this worktree's branch, the list, show, cancel, the merge room |
| `version.go` | the version, stamped by the linker |
