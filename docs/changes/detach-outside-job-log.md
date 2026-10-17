Docs: none, no verb, flag, setting or file changed. `internal/detach.Start` takes the log as a path instead of a file.

## What and why

`internal/ptyhost.TestStartRunsADetachedHostFromACopy` sat at 10.35 and 10.85 s on the windows-latest runner (runs
39032382613 and 39034662485), 7.2 to 7.8 s of it inside `Start`, and the probe said the host was in a job. The runner's
test process is in a job object with no breakaway flag, so `detach.Start` took the WMI path. Reproduced locally by
running the test inside a fresh job object: 20 to 54 s inside `Start`, the script's poll running its whole course,
then "OpenProcess: The parameter is incorrect" and the fallback start inside the job.

The script's own trace said why: cmd.exe was created (ReturnValue 0) and was already gone at the first poll with no
child and nothing in the log. Five WMI command lines by hand narrowed it to one condition: the redirect target being a
file the starting process holds open for writing. cmd's `>>` asks for a share mode that excludes other writers, fails,
and prints its error on a console nobody sees. Every caller of `detach.Start` (ptyhost.Start, startRoom,
spawnRoomRestart) opened the log and held it across the call.

What changed:

- `detach.Start` takes the log as a path. It opens the file only around the direct start and has it closed before the
  WMI retry. `detach.PathOf` gives spawnRoomRestart the path of a stderr that is a file.
- The script passes `Win32_ProcessStartup` with `ShowWindow` 0, so the cmd console WMI creates no longer flashes on
  the desktop. It polls on a 5 s clock instead of 50 iterations (each CIM query is 100 to 600 ms, so the old count was
  7 s on the runner and 20 s here), and exits 4 as soon as cmd is gone, which `startOutsideJob` names in its error.
- Inside a refusing job the ptyhost start test went from 54 s to 5.3 s here. internal/detach gained a test that
  ping's output reaches the log, and one that a held log is named as the cause within a poll or two.

The earlier Defender theory (`docs/changes/ci-defender.md`) was the local cause, not the runner's: the runner printed
real-time protection off, and the phase lines on the runner pointed at Start with the host in a job.

## Test plan

## @LETTER@. A detached start leaves a job that refuses breakaway

### @LETTER@1. The runner's ptyhost start test

1. On a run after this change, find `TestStartRunsADetachedHostFromACopy` in the windows-latest go-test job's log.

**Expected:** "started after" is under 2 s and the test is under the 10 s guard.

### @LETTER@2. The detach tests

1. Run `go test -tags integration -count=1 -v ./internal/detach` on Windows.

**Expected:** `TestStartOutsideJobAnswersTheProgramAndWritesItsLog` passes with "started after" around 1 s, and
`TestStartOutsideJobSaysWhenCmdCannotOpenAHeldLog` names the cause in under 4 s.

### @LETTER@3. No window

1. On a Windows desktop, run the same tests and watch the taskbar.

**Expected:** no cmd or ping console appears.
