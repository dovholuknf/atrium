# m-new-release-0-0-1 report

READY: prepared by agents, the publish is clint's by hand. The gate is not green on this machine and cannot be here.

## Done
- Rebased onto `claude/main` (bcdbb267).
- The three questions are written into the item file under "Decided by @fabric, 2026-10-05" and the plan matches:
  `0.0.1` and `v0.0.1`, unsigned with `checksums.txt` published, GitHub Release plus scoop plus the docs site.
- Filed `docs/backlog/release/m-new-release-signing.md`.
- Fixed the gofmt failure in `cmd/ptyhost-spike/pipe_windows.go`.
- Filed for @ui, not fixed: `docs/backlog/ui/u-new-gate-title-tooltips.md` and `u-new-gate-on-danger.md`.
- Item status is READY. Changelog extended.

## Gate parts run here, 2026-10-05
- Pass: gofmt, go vet, go build, go test of every package except `daemon`, `gitsync` and `link`, bar the failures below.
- Fail, this machine: no `~/.ssh/id_ed25519_sign.pub` (`TestPRWorktree*` in `internal/api`, and the cut-release checks),
  no symlink rights (`TestInstallWritesThroughASymlink`). `internal/daemon`, `gitsync`, `link` hit the 600 s timeout
  and were skipped this time.
- Fail, @ui's: `check-titles.sh` and `check-skins.sh`.

## Left, commands for clint
Plan section "Commands, in order". First, the full gate on a machine with a signing key and no symlink limit, once
@ui has fixed the two filed items. Then `cut-release.sh v0.0.1 --preflight`, the dry run, `--execute`, the scoop
bucket push, the scoop install check and the docs workflow. None was run.
