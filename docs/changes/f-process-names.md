## Test plan

## @LETTER@. The pty host runs as atrium-pty

### @LETTER@1. A new host is named for its role

1. Turn on the `pty_host` setting and start a room from a build with no `atrium-pty` beside it.
2. Open a terminal. Confirm `atrium-pty` (`.exe` on Windows) now sits beside `atrium` and the host process carries that
   name in Task Manager or `ps`.
3. Confirm `atrium.ptyhost` is gone when no old host is running, and that hooks in `settings.json` still name `atrium`.

### @LETTER@2. A stale copy is refreshed, a held one is left

1. Replace `atrium` with a different build of the same size and stop every pty host.
2. Start a room. Confirm `atrium-pty` now matches the new `atrium` byte for byte.
3. With a host running from `atrium-pty` on Windows, start another room for a different state dir. Confirm the start
   succeeds and the running host's file is untouched.
