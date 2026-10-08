## Test plan

## @LETTER@. Provisioning drives `atrium room setup`

### @LETTER@1. A work root from empty

1. On a machine with an account the room will run as, run
   `pwsh -File scripts/provision-room.ps1 <account>@<host> -Name <room> -WorkRoot <folder on a drive>`.
2. Run it again, then `pwsh -File scripts/provision-room.ps1 <account>@<host> -Name <room> -Check`.

**Expected:** the first run prints `work-dirs`, `work-cache`, `git-root`, `scm-root`, `reviews-root`, `handoff-dir` and
`agent-pack` lines from the room's own `atrium room setup --apply`, with the work root made under the folder. The second
run says `ok` on every one and changes nothing. `-Check` writes nothing and prints the same rows as `ok`. The old
`scripts/room-work.ps1` is gone and nothing names it.

### @LETTER@2. A parent the account cannot examine

1. Use a `-WorkRoot` whose drive or parent the account cannot examine, on a machine that already has an atrium.

**Expected:** exit 13 before anything is changed, a `work-root fail` line and the lines an administrator runs under it.
`room-check.ps1 <room>` shows the same as a `work-root human` row. After the administrator ran the lines, a rerun goes on.

### @LETTER@3. A machine with no atrium yet, and one with an old one

1. Provision a machine that has no `~/.atrium/bin/atrium`, with `-WorkRoot`, and `-Check` another that has an atrium
   without `room setup`.

**Expected:** a `work-root warn` saying the first run checks it, the run installs atrium, joins, and then applies. The
old atrium is a `warn` under `-Check` and a `skip` row in `room-check.ps1`.

### @LETTER@4. A work root that is not safe

1. Run with `-WorkRoot V:\`, `-WorkRoot work\localai` or `-WorkRoot \\h\s\x` against a machine that has an atrium.

**Expected:** exit 1 and `work-root fail atrium room setup refused: ...` naming the field. The room is unchanged.
