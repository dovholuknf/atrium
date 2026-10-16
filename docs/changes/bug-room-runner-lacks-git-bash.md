# bug-room-runner-lacks-git-bash

A room started over ssh on Windows had no Git on PATH, so claude spawned without Git Bash and every hook
blocked. The card sat at needs-input looking alive. A launch that cannot run a tool must not make a card.

## Design

- **Where.** `launchLocked` in `internal/daemon/launch.go`, right after `env := childEnvFrom(...)` and before
  anything is spawned. The new code is `internal/daemon/gitbash.go`.
- **When.** Only on Windows, and only for the claude runner (harness id `claude`). Other runners and other
  OSes skip it untouched.
- **What is checked.** The environment the runner will actually get (`env`, so a harness-set value counts):
  1. `CLAUDE_CODE_GIT_BASH_PATH` naming an existing file.
  2. `git.exe` on that env's PATH, then `..\bin\bash.exe` beside it (the rule `claudeconf` already uses).
  3. `Program Files\Git\bin\bash.exe`.
  4. Known atrium installs: `<home>\.local\share\atrium-tools\git\bin\bash.exe` and
     `%LOCALAPPDATA%\atrium-tools\git\bin\bash.exe`.
  A bash.exe that only sits on PATH does not count (System32 has WSL's launcher).
  The rule is rewritten over plain strings in `gitbash.go`, because `claudeconf` uses `filepath` and would
  not test on a Mac.
- **Found by 1 to 3.** Nothing changes. **Found only by 4.** `CLAUDE_CODE_GIT_BASH_PATH` is set to it in the
  runner's env, so the room needs no restart and no PATH edit.
- **Not found.** The launch is refused before anything is spawned: `launchFailed` marks the card dead with the reason, and
  the error is returned to the caller (and so to the launcher): `claude needs Git Bash on Windows and this room cannot find one: set
  CLAUDE_CODE_GIT_BASH_PATH to a bash.exe, or put a full Git (not MinGit) on the room's PATH, then start the
  room again`.
- **Injected for tests.** `gitBashFor(goos, runnerID string, env []string, home string,
  exists func(string) bool, lookPath func(name, path string) (string, error))` returns the env entry to add (or empty) and an error. No
  disk, no OS probing, runs on any OS. The caller passes the real ones.
- **Not doing.** Reporting a card whose first tool call fails this way: the refusal makes it unreachable for
  this cause, and a hook-error detector is a separate item.

## Test plan

- Not Windows, or not claude: no check, no change.
- Env var naming an existing file passes unchanged; naming a missing file falls through.
- MinGit on PATH (no bin\bash.exe) and nothing else: refused with the reason.
- Full Git on PATH: passes unchanged.
- Only the atrium-tools Git present: passes and sets `CLAUDE_CODE_GIT_BASH_PATH`.
- A harness-set `CLAUDE_CODE_GIT_BASH_PATH` is honoured and never overwritten.
