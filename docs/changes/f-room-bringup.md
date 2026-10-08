## Test plan

## @LETTER@. Room bring-up: work root and agent pack

### @LETTER@1. A work root on a second drive

1. On a Windows machine with a second drive and a standard account, make `V:\localai` and grant the account only
   `icacls V:\ /grant '<acct>:(RA,REA)'`, then run `provision-room.ps1 <acct>@<host> -WorkRoot V:\localai`.
2. Run it again, then `provision-room.ps1 ... -WorkRoot V:\localai -Check`.
3. Remove the drive grant and run it once more.

**Expected:** the first run makes `git`, `reviews`, `handoff` and `cache` under the root, sets `git_root`, `scm_root`,
`reviews_root` and `context_handoff_dir`, and points npm, go, pip and cargo at `cache`. The rerun and `-Check` say `ok`
on every step and change nothing. The last run fails `work-root` with exit 13, names the parent it cannot examine and
prints the `icacls` lines, and has changed nothing else.

### @LETTER@2. The agent pack

1. Provision a room without `-NoAgentPack`, then read `~/.claude/atrium-agent-pack.json` on it.
2. Run `room-check.ps1 <room>`, then provision again.

**Expected:** the room has the dotfiles agents and skills as real files, and the record names the mirror's commit. The
check shows `agent-pack ok`, and a second provision says `already at <sha>, nothing changed`.
