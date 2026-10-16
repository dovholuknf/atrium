Branch status's commit facts and the carried decision (`readFacts`, `readMeta`, `hashBatch`, `carriedBy`) are now plain functions over a Runner and a directory in `internal/gitsync/landed.go`, so a later tool can ask the same landed question in a room. No behavior change, hub only, nothing to deploy. (f-worktrees-tool, phase 1)
Docs: none, covered by docs/changes/f-worktrees-tool-p1.md
