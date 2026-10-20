# internal/ptyhost

The pty host: a small long-lived process that owns the pseudo terminals and the runners under them, so a room can
restart without ending any of them. The design is `docs/rnd/rolling-restart-design.md` and the wire protocol is
`docs/terminal/ptyhost-protocol.md`. The daemon's side of it is `internal/daemon/hostterm.go`, used only when the
`pty_host` setting is on.

## What the host knows, and what it must not

The host knows a card only as the opaque `id` it was handed. Nothing that is policy lives here. Kind (`runner` or
`shell`) is stored and reported, never acted on. The screen model, the typing gate and the ring the board reads stay
in the daemon. `ring.go` is deliberately NOT the daemon's `ringBuffer`: it keeps bytes, absolute offsets and size cuts,
and none of the width bookkeeping of a screen model.

## Invariants

1. **The drain never stops and never waits on a client.** A pty nobody reads fills its pipe and the runner blocks on
   a write, which must never happen during a daemon gap. Output goes through each client's bounded queue
   (`DefaultClientQueue`, 4 MB). A client that falls further behind is closed, and it reattaches from its offset.
   A reply is never counted against the bound, since an attach replay is legitimately bigger than it.
2. **One daemon at a time.** Only a connection that said `hello` may use the verbs after it. A second `hello` without
   `takeover` is refused with "a daemon is already connected". A takeover closes the old daemon and then takes
   `verbMu` for an instant, so the old daemon's in-flight verb finishes before the new one's first is answered.
3. **`probe` is read-only.** It needs no hello, evicts nobody, and changes nothing. Call it to look at a host.
4. **Every verb after spawn names the pty by `run_id` alone.** An unknown or empty `run_id` is refused. A `run_id` is
   a random 128-bit ULID-style id, never a counter a restarted host could repeat.
5. **An exit is kept until it is collected.** `collect` is refused while the runner lives, and only then does the host
   forget the pty and its ring. The idle exit (`DefaultIdleExit`, ten minutes) needs no pty AND no daemon, so a host
   holding an uncollected exit never goes idle.
6. **Exit never reaches a client before the last output bytes.** `await` waits for the read to end, or, since a ConPTY
   stays open until it is closed, for a 200ms quiet spell bounded at two seconds.
7. **Attach is snapshot, subscribe and reply under one lock.** That is what keeps a byte from being missed or seen
   twice between the replay and the live stream. Do not split it.
8. **A resize records its cut BEFORE it is applied**, so a byte produced at the new size never sits ahead of its cut.
   A failed resize puts the old size back.
9. **`Host.Close` KILLS the runners, it does not only close the ptys.** Closing a ConPTY does not end the process
   under it, and a test run once left twenty runners behind.

## Landmines

- **Close every handle through `closeOnce`.** Closing a Windows handle twice is not a Go panic. The number may already
  belong to something else, and the spike took the whole host and every runner down with "netpoll failed".
- **`argv[0]` is resolved on the host's PATH before the working directory is set.** go-pty resolves relative to
  `Dir`, so a bare `claude` would otherwise be looked for inside the repository being worked in.
- **The host runs from a COPY**, `atrium-pty[.exe]` next to the installed binary, so swapping in a new atrium never
  touches the image the host runs from. A copy that is current, or that cannot be replaced, is used as it is.
- **`Start` does not check for a running host.** Probe first. A second host for one state dir exits at once because
  the listener refuses it, since two hosts would split the runners between them.
- **`signal term` is a kill on Windows.** `os.Process.Signal` supports only Kill there. A polite stop types the
  runner's exit keys through `write` and uses `signal` only when that has failed. Elsewhere it is SIGTERM.
- **The address is a hash of the state dir and the user**, so two atriums or two users on one machine never meet and
  the name reveals no path. On Windows it is a named pipe whose DACL grants only the current user's SID, not
  Administrators, not SYSTEM. go-winio's default descriptor would grant both and is never used. Elsewhere it is a
  unix socket at mode 0600, and when the state dir path is over the 103-byte `sun_path` limit it goes to a private
  `/tmp/atrium-<uid>/` instead.
- **`in_job` on a probe means ending the starter's job ends the host and every runner.** The host is started with
  `CREATE_BREAKAWAY_FROM_JOB` and is still inside a job only when that job refused breakaway. A caller already in a
  job goes straight to WMI (`internal/detach`), since breakaway escapes only the innermost job and a terminal's job
  can sit under another that keeps the host.
- **Integration tests carry `//go:build integration`** and do not compile under a plain `go test`. See
  `scripts/test.sh`.

## The files

| | |
| --- | --- |
| `proto.go` | the frame shapes, the verbs, and `Proto`. Exited and exit code are never omitted on purpose |
| `host.go` | the host: clients and their bounded queues, the drain, the exit, and every verb |
| `client.go` | THE CLIENT API the daemon is written against. Its header is the contract. Keep it small |
| `ring.go` | bytes, absolute offsets, size cuts and clear marks. Not the daemon's ring |
| `channel*.go` | the address, the named pipe or unix socket, and who may connect to it |
| `close.go` | `closeOnce`, the one way anything here closes |
| `run.go` | `Run` for `atrium ptyhost`, and `Listen` for a test that wants a host in process |
| `start.go` | launching a host detached, from a copy of the binary, logging to `ptyhost.log` in the state dir |
| `jobinfo_*.go` | whether the host sits inside a job object that refused breakaway |
| `raise_*.go` | Windows only: above normal priority for a runner and its console host, unless `ATRIUM_PRIORITY=normal` |
| `signal_*.go` | what `signal term` means on each platform |
