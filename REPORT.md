# m-new-release-0-0-1 report

Incomplete: three answers are needed from clint, and the deploy queue is not merged.

## Done
- Plan: `docs/release/release-0-0-1-plan.md`, with the prerequisites, version and tag, who runs each step, and every
  outward command as exact text for clint. None was run.
- Docs site sources refreshed (`website/docs/board.md`, `control-mcp.md`, `rooms.md`). Built locally to
  `build.claude/docs-site` with docusaurus, no broken links, gate-hook script passes. Not published.
- Item file updated with status and the open questions. Changelog entry added.

## Not done
- OWED.md: `notes/OWED.md` is not in this checkout. Line to add:
  `| R2 | Release 0.0.1 | docs/backlog/release/m-new-release-0-0-1.md |` (match the table's column layout).
- Open questions for clint: version string, signing, channels. The plan assumes v0.0.1, unsigned, GitHub Release plus
  scoop plus docs site.
- Not covered by the site refresh: change requests, allowed folders, forge access.
- No Go code changed, so no Go tests were run. I did not run `scripts/ci.sh`.

## Commands left for clint
In the plan, section "Commands, in order": the gate, `cut-release.sh v0.0.1 --preflight`, the dry run, `--execute`,
the scoop bucket push, the scoop install check and the docs workflow.
