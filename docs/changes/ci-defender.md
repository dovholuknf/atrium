Docs: none, a CI script and a test log line. Nothing a reader names changed.

## What and why

`scripts/test-time-guard.sh` flagged `internal/ptyhost.TestStartRunsADetachedHostFromACopy` on the windows-latest
runner at 11.73 s (run 39000703278), 12.42 s (39003297482) and 7.87 s (39000519389), against a 5 s limit doubled on a
hosted runner. `internal/detach.TestStartOutsideJobAnswersTheProgramNotCmd` sat at 13.94, 9.92 and 6.66 s in the same
runs. Both start an executable that did not exist a moment before.

The trace, on a Windows 11 box with Defender on: `Start` on a fresh copy of the 7 MB test binary takes 2.2 to 3.4 s
inside `cmd.Start`, with the breakaway succeeding, so not the WMI path. The same copy started again takes 20 ms. A
second fresh copy pays again. A hard link pays too. The dial lands 50 ms after the start returns and the host exits
3 s after its own "up". So the test is the scan plus the 3 s idle floor, and the scan is the part that grows on a
loaded runner. `scripts/ci-defender.ps1` measures the same thing with go.exe on this machine: first start 4276 ms,
second 30 ms.

Whether GitHub's runner scans the same way was not known, so the fix prints the evidence rather than assuming it:

- `scripts/ci-defender.ps1`, run by the machine step of `scripts/ci.sh` on Windows under `GITHUB_ACTIONS` only,
  prints real-time protection on or off, the exclusions already there, and the first and second start of a fresh copy
  of go.exe. When protection is on and the shell is elevated it excludes TEMP, RUNNER_TEMP, GOCACHE, GOTMPDIR and the
  checkout's build.claude, and measures a fresh copy again. It never fails the step and never changes a machine that
  is not GitHub's.
- The ptyhost test logs "started after", "dialed after" and "probed after, in a job", so the next guard report says
  which part was slow. Its idle floor stays 3 s. The test was not widened and no allowlist line was added.

One thing learned on the way: Defender's behaviour signatures flag a shell command line that carries the exclusion
cmdlet (Trojan:Win32/PowExcEnv.H!MTB), and blocked the bash that was writing the script with a heredoc. The script
is written as a file. The same text on disk is not flagged, as `scripts/room-defender.ps1` has shown for weeks.

## Test plan

## @LETTER@. Defender is measured and excluded on the Windows runner

### @LETTER@1. The machine step says what Defender does

1. Open the windows-latest job of any group on a run after this change, and its "the machine" step.

**Expected:** lines prefixed `ci-defender:` say whether real-time protection is on, what a fresh copy of go.exe paid
on its first and second start, which folders were excluded, and the two times again after. No line fails the step.

### @LETTER@2. The ptyhost start test is under the guard

1. In the same run's go-test job, find `TestStartRunsADetachedHostFromACopy` in `timings.sorted.tsv` and its log.

**Expected:** the test logs "started after", "dialed after" and "probed after", the started time is well under the
2 to 3 s a scan costs, and the test is under the guard's 10 s.

### @LETTER@3. A developer's machine is only measured

1. On a Windows box with Defender on, run `pwsh -NoProfile -File scripts\ci-defender.ps1` without `GITHUB_ACTIONS`.

**Expected:** it prints the status and the two start times, says "not on GitHub Actions, so nothing is excluded",
and exits 0. `Get-MpPreference` shows no new exclusion.
