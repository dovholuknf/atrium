Docs: d-slim-orchestrator

## Test plan

## @LETTER@. A slim orchestrator can only dispatch

### @LETTER@1. Denied tools

1. In the board launch dialog pick runner `claude slim`, cwd the atrium checkout, and launch.
2. Ask it to run `git status`, then to edit any file.

**Expected:** both are refused as disallowed tools.

### @LETTER@2. Allowed tools

1. Ask it to read a file and grep the repo.
2. Ask it to list peers with the atrium tools.

**Expected:** Read, Grep, Glob and the atrium tools all work.
