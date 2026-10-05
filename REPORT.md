# f-003 stage 1: report

## Done
- `internal/resources`: `Path`, `Init` (O_EXCL, never overwrites), `Read` (64 KiB cap), the starter header.
- `atrium resources init` (`internal/cli/resources.go`) writes `<StateDir>/resources.md`.
- `atrium_resources` tool (`internal/link/resources_mcp.go`): returns the file plus the hub's rooms (name, online, OS/arch).
  In the worker set (`ctlclass.go`). Pinned lists in `ctlclass_test.go` updated (worker 10, full 20).
- Framing line, "`atrium_resources` lists the machines and environments you may use.", added to BRIEF.md
  (`writeBriefFile`) and, for a launch with no brief, to the prompt. Same mechanism as the git-url line.
- Tests: resources, link tool, cli init, daemon brief line. Changelog `changelog/fabric/2026-10-04-f-003.md`.

## Left
- The hub reads the file on ITS machine. A room on another machine has its own file the hub cannot see (stage 2 fan-out).
- The file is at `<StateDir>/resources.md` (`~/.atrium` by default, `$WORKTREE_ROOT/hub` when set), reading "next to the
  state dir" as the design's `~/.atrium/resources.md` example.
- The framing line needs a brief or a prompt, so a launch with neither (a bare resume or an empty launch) gets none.
- clint seeds the file for sg4 (m1mini, FIPS network).

## Verify
- `go test ./internal/resources ./internal/link ./internal/cli` pass.
- daemon `-run 'Brief|Resources|Launch|Lean'`: only `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP` fails, and it
  fails identically on clean c9d65765. The full daemon suite was not run.
