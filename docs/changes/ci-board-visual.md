# CI: board-visual group

The board's pixel and timing units run in a `board-visual` CI group that cannot redden the run.
See `changelog/ops/2026-10-16-ci-board-visual.md`.

## Test plan @LETTER@

1. `bash scripts/ci.sh --list-groups` names `board` and `board-visual`.
2. `bash scripts/ci.sh --group board-visual` runs only the units on `scripts/board-visual-units.txt` and writes
   `build.claude/ci/board-visual-env.txt` and `board-visual.log`.
3. Add a made-up name to `scripts/board-visual-units.txt`. `ci.sh` stops and names it.
4. On a run, a failing `check (<os>, board-visual)` job shows green with its step failed, and `board` stays gating.
