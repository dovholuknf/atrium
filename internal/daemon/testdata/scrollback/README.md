# Frozen scrollback fixtures

Read by `screen_real_test.go`. The loader takes `*.scrollback` and `*.scrollback.gz`. Nothing here is copied from a
live room's `~/.atrium/scrollback`: a live scrollback is a transcript of somebody's work, and this repository is
public. Every file was captured on a throwaway room from scripted work on atrium's own public source.

## How they were made

1. Build a binary into `build.claude/`.
2. Extract `git archive HEAD` to a neutral directory (`D:\cap\atrium`), so no home directory appears in the
   terminal's title or header. Add `.claude/settings.local.json` with a `spinnerVerbs` list of `Kneading`,
   `Forging` and `Simmering`, so the spinner words are ones the spinner test looks for.
3. Start a throwaway daemon on its own ports, from an empty database:
   `ATRIUM_LOCATION=<private>/daemon.json ATRIUM_SHARED=- ATRIUM_SHARED_LOCATION=-`,
   `atrium daemon --addr 127.0.0.1:17777 --http 127.0.0.1:17778 --db <private>/state/atrium.db
   --location-file <private>/daemon.json`. Never a copy of a live database, and never the default ports, which would
   take over every live session's hooks.
4. `atrium launch --cwd D:\cap\atrium --prompt ...` for each file, with `ATRIUM_BOARD_URL=http://127.0.0.1:17778`.
5. `atrium stop` against that instance. The daemon writes `<private>/state/scrollback/<card>.scrollback` on the
   way down, and that file is the capture.
6. Replace the OSC window title `C:\Users\<user>\.local\bin\claude.exe` with `claude.exe`, the one place a home
   directory got in. Grep the result for `Users`, `@`, `token`, host names and the operator's name.
7. Zero the account usage in the statusline, digit for digit so no byte moves: the `5h` percentage and its
   `(1h27m)` reset time, and the `wk` percentage, wherever they are written whole (escapes between the words are
   allowed, and the digits inside escapes are left alone). Then render every synchronized frame and check that
   neither percentage is ever anything but `00%`. Single-digit repaints of the reset countdown's minutes survive
   that (`0h02m`), which is a countdown and not usage. `ctx` and `tx` are this session's own counts and stay.

| File | Prompt |
| --- | --- |
| `session-tools.scrollback` | Read two source files, run `go vet ./internal/daemon/`, read two docs, `Start-Sleep 25`, then explain the screen model in 40 lines. Tool calls, hooks, a long spinner. |
| `session-reply.scrollback` | Read `docs/architecture-v2.md`, no tools, then write a review of at least 120 lines with headings, bullets and a table. A reply longer than one screen. |
| `lostlines.scrollback` | Attach over the websocket, ask for `LOSTLINES-BEGIN`, then `L0001 lostlines-tail` to `L0300 lostlines-tail`, while sending 16 resizes between 206x50 and 206x40. Then two more, then ask for `LOSTLINES-AFTER-REPAINT`. |

The pseudo terminal is 206x50, and the tests replay at 120 columns, the width the board uses most.

## Recapturing

Redo the steps for whichever file, then re-pin its floor in `pinnedFloors` in `screen_real_test.go`: the measured
percentage less 5, never below 40. A capture under 80% is read by hand before it is pinned. `lostlines` has no floor.
Its two tests count the numbered lines directly, and all 300 must survive. It was captured on a build with the
height hold (`387ccd5`) and does not reproduce item 74's loss.

Recapture when claude-code's output changes shape.
