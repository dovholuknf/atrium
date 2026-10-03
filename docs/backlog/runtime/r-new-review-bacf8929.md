# Review: r-restart-loopback 5f2bcd58..bacf8929 (m1mini, 2026-10-02): OK

One commit on claude/r-restart-loopback, one of clint's pause exceptions. It closes most of the held
`r-new-sec-daemon-wide-bind`:

- `atrium daemon` now defaults `--addr` and `--http` to `127.0.0.1:7777` and `127.0.0.1:7778`.
- The daemon records the addresses it bound (`agent_listen`, `board_listen`) in its location file.
- `restart_atrium` passes those addresses back.
- A file written before this change, with no bind recorded, falls back to 127.0.0.1 on its old port. It never falls
  back to a wider address.

The commit is unsigned.

## How it was checked

- I read the diff, the restart path (`restartHandler`, then `runRestart`, `readLocation` and `ReadLocation`), and
  where each location file comes from.
- At the tip, `go test ./internal/cli/ -run 'TestRestartKeepsTheDaemonsBind|TestDaemonDefaultsAreLoopback'` passes.
  `go vet` is clean on `internal/cli` and `internal/daemon`. gofmt is clean apart from the known `fyi_test.go`.
- I ran four mutants in a scratch worktree:
  - The restart passes only `--db` again: `TestRestartKeepsTheDaemonsBind` fails.
  - The fallback for an old file becomes `":"+port`: the same test fails.
  - The `--http` default goes back to `:7778`: `TestDaemonDefaultsAreLoopback` fails.
  - The daemon no longer writes `agent_listen` and `board_listen`: nothing fails. That is L1.
- On landing the only conflict is in `docs/test-plan.md`. It lands as **IF**.

## What holds

- **No caller can widen the bind.** `restart_atrium` takes `why`, `wait_seconds` and `force`, and no address. The
  detached half takes `--db` and `--delay`. The bind comes from the location file alone:
  - A recorded bind is passed back exactly as the daemon was given it.
  - Without one, the fallback is always `127.0.0.1:<port>`, whatever the old reach URL's host was (`localhost`,
    `[::1]`, a LAN IP).
  - An empty host (`:7778`), `0.0.0.0`, `[::]` or `::ffff:0.0.0.0` reaches the new daemon only if the old daemon
    itself was started with it. That is the operator's choice, by design.
- **It reads the right file at the right moment.** `runRestart` reads the location before `stopDaemon`, and that
  matters because a clean wind-down deletes the file. A runner's session inherits `ATRIUM_LOCATION`, so a daemon
  started with `--location-file` is found through its own file.
- **The default change matches what rooms already do.** Rooms already pass explicit `127.0.0.1` addresses, and a room
  restarts through `roomrestart.go`, which carries its own. Only bare `atrium daemon` installs change: a no-flag start
  is now loopback. That covers the autostart script when `-Http` is not given, and the LaunchAgent.
- **Windows and macOS.** 127.0.0.1 binds the same way on both. A board at `http://localhost:7778` still opens: the
  browser falls back from ::1 to 127.0.0.1.

## Lows

- **L1: the write side is untested.** The new test builds a `daemon.Location` by hand. If `writeLocation` stopped
  recording `AgentListen` and `BoardListen`, every restart would quietly fall to loopback on the reach port, and
  nothing would fail. Add a test that starts a daemon on `127.0.0.1:<free>` with `LocationFile` set, reads the file
  back, and checks both fields.
- **L2: the shared location file can now set a bind.**
  - `ReadLocation` falls back to `%WORKTREE_ROOT%/atrium/daemon.json` when the per-user file is missing. That shared
    file sits on ground that other accounts can write.
  - Before this change it only steered which daemon got stopped. Now its `board_listen` becomes the new daemon's
    `--http`.
  - Fix: take a recorded bind only from the per-user file or `ATRIUM_LOCATION`. From the shared file, fall back to
    loopback on the port. A state file read back on a rerun is input.
- **L3: the restart still drops the daemon's other flags.** `--location-file`, `--shutdown-token`, `--board-dir` and
  `--started-by` are lost, and the restart always runs `daemon`, never `room`. This predates the change, but the held
  item asked for "its addresses and its mode", and the test plan's step 4 depends on it. Either record the mode and
  those flags too, or say in the item that only the bind is carried.
- **L4: a wide board bind with no login is neither warned about nor refused.** That was the held item's third
  "Wanted" bullet. Keeping the operator's choice is fine, but log one line at start, as `loopbackBoard` does. Leave
  `r-new-sec-daemon-wide-bind` open for this bullet and for L3's mode.

Atrium-Verdict: room-ok 5f2bcd58..bacf8929
Atrium-Verdict: hub-ok 5f2bcd58..bacf8929
Quality: a small, well-aimed fix. The bind is carried exactly as given and never rebuilt from the reach URL, the
fallback for an old file narrows rather than widens, and three of the four mutants are caught.
