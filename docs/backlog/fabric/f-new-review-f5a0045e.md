# Review: provision follow-up 9bdbf6c3 and board suite on sg3 f5a0045e (@fabric)

Range b7c13a9e..f5a0045e, two commits on claude/fabric. Unsigned.

- 9bdbf6c3 closes the two lows on the provision macOS start (1e6d18e7). The warn now carries the detached start's last
  line, and the done line says that a later desktop login loads the LaunchAgent, whose room then finds the port taken
  and is restarted every 10 s.
- f5a0045e is clint's order: `node scripts/test-board-sharded.js` hands the run to sg3 by default, through
  scripts/board-suite-remote.ps1 (snapshot, push, ssh) and scripts/board-suite-run.ps1 (the room's half, 5.1).

## 9bdbf6c3

I checked one risk: `last=$(... 2>&1 | tail -n 1)` waits for EOF on the pipe, so a detached child that kept the pipe
would hang provision. It does not keep it. `detach.Start` gives the child the room log for stdout and stderr and nil
for stdin, and `exec` replaces the zsh, so the pipe closes when `room --detach` returns. On a refused start, the last
line is cobra's `Error: ...`, which is the useful one. OK.

## f5a0045e

### The question asked: the empty-args path

It works. `$SuiteArgs = ''` is sent as `A`, `Substring(1)` is `''`, and `FromBase64String('')` is an empty array, so
`$suiteArgs` is `''` and the command is `node scripts/test-board-sharded.js --local `. Base64 never carries a space, so
it survives the ssh command line as one argument.

### MEDIUM: a failed `git add -A` is ignored, and the suite runs HEAD without the changes it was asked to test (proven)

`$ErrorActionPreference = 'Stop'` does not cover a native command's exit code in pwsh 7, and nothing checks
`$LASTEXITCODE` after `read-tree`, `add`, `write-tree` or `commit-tree`. When `git add -A` fails, the temporary index
is never written, `write-tree` writes HEAD's tree, `$dirty` is false, and the line says it runs `<head>` with no
mention of the uncommitted changes. The suite then reports on a tree that lacks every uncommitted edit. A board check
done this way says green for a change it never ran.

Proof: D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-f5a0045e.ps1 runs the script's snapshot steps in
D:/worktrees/claude/atrium/review, which has an untracked file named `NUL`, as many Windows worktrees here do because a
`> NUL` from bash makes one. Result: `error: short read while indexing NUL`, `fatal: adding files failed`,
`add exit: 128`, then `write-tree exit: 0` with HEAD's tree. The real index is untouched.

Fix: check `$LASTEXITCODE` after each git step and exit 3 with git's message. Say in it that an untracked file git
cannot read (such as `NUL`) is the usual cause, and that `--local` runs the suite here. Snapshotting only tracked
changes (`git add -u`) plus untracked files under scripts/ and internal/ would make the stray-file case rarer, but the
exit check is what is needed.

### LOW: `Invoke-Expression` on sg3 runs the suite arguments as PowerShell

board-suite-run.ps1 builds `"node ... --local $suiteArgs"` and runs it with `Invoke-Expression`. A unit list with a
`;` or a `$(...)` in it would run as code on sg3. Only the operator types those, as the same user, so this is not a
boundary. It is the fragile way to do it, and a quote in an argument breaks the run. Send the arguments as base64 JSON
of an array and run `& node scripts/test-board-sharded.js --local @argv`.

### LOW: an interrupted run leaves a worktree and a ref on sg3

Ctrl-C ends the local ssh. Whether the remote powershell and its node children die with the channel depends on
OpenSSH for Windows, and if they do, the `finally` that removes `suite-<id>` and `refs/suite/<id>` does not run. A
sweep at the start of board-suite-run.ps1 that removes `suite-*` worktrees and `refs/suite/*` older than a day would
bound it.

### NITS

- The ssh command line quotes nothing, so a clone path with a space breaks it. The clone paths in use have none.
- `opt.argv` is set in parseArgs and never read.

## Verdict

9bdbf6c3 alone would be OK. HOLD b7c13a9e..f5a0045e for the medium in f5a0045e. Re-read b7c13a9e..tip. Scripts only,
nothing served, so the verdict will be hub-ok and room-ok as for test tooling.

Quality: after the Sonnet switch, the design is careful (temporary index, nothing staged, refs and worktree removed,
exit codes kept). The miss is the classic pwsh one: native failures do not throw, and nothing checked them.
