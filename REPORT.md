# m-new-release-0-0-1 report

Incomplete: the gate is red on this machine and three answers are still needed from clint.

## Done
- Merged `claude/main` (c9d65765). The plan now reads the deploy queue as met and names `/_hub/deploy-queue` as the check.
- Docs site: added allowed folders, forge access, change requests and the deploy queue to `website/docs/rooms.md`, a
  pointer in `board.md`, and the missing CLI verbs in `cli.md` (checked against `atrium help`). Built to
  `build.claude/docs-site`, success, nothing published.
- Gate run: `bash scripts/ci.sh` with both env vars unset FAILED. Recorded in the plan under "Gate result". Log at
  `build.claude/ci.log`.
- Item file and changelog extended.

## Left
- Gate: this machine lacks `~/.ssh/id_ed25519_sign.pub` (commits fail, so `TestPRWorktree*` and all cut-release checks
  fail with 128), lacks symlink rights, and `internal/daemon`, `gitsync` and `link` hit the 600 s timeout. From
  `claude/main` and needing an owner: gofmt on `cmd/ptyhost-spike/pipe_windows.go`, `title=` tooltips not in
  `scripts/title-allowlist.txt` (index.html, changereq.js, hubrepos.js), skins (`--on-danger` set only by
  daylight, frost, linen, paper).
- Clint's three questions (version and tag, signing, channels) stay in the item file.
- OWED.md is not in this checkout. Line: `| R2 | Release 0.0.1 | docs/backlog/release/m-new-release-0-0-1.md |`.

## Commands left for clint
Plan section "Commands, in order": deploy-queue check, the gate, `cut-release.sh v0.0.1 --preflight`, the dry run,
`--execute`, the scoop bucket push, the scoop install check, the docs workflow. None was run.
