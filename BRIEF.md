# Worker brief: u-new-details-debug-section

Launched by @ui director (director-ui on claude-sg4) for the orchestrator, night of 2026-10-04. You implement one
backlog item. Your worktree is on branch `claude/u-new-details-debug-section`, cut from
`claude/u-new-gate-readout-only-when-blocking` at 78b106e6 (not yet merged), so `typingGateText(s, held)` in
`js/typing.js` is there for the gate's words. Reuse it. Windows, PowerShell.

## Read first
- `docs/backlog/ui/u-new-details-debug-section.md`: the item. It is the spec, including the click-outside and Escape
  bug. Write choices (does the settings dialog keep its checkboxes or point at the drawer) under "## Design note" in
  the item file and in REPORT.md.
- `docs/backlog/ui/u-new-gate-readout-only-when-blocking.md` (with its design note) and
  `docs/backlog/ui/u-new-browser-prefs-every-window.md` (another worker is building that now on its own branch: do
  not build a cross-window mechanism here, just read and write the same localStorage keys as today).
- `internal/api/web/AGENTS.md`: how the web board is built and tested.

## Rules
- Before and after PNGs of the real details popover opened from a terminal (headless board, real code, no mockups)
  under `docs/screens/u-new-details-debug-section/`: the debug section with the gate open, and closed with the
  countdown.
- Headless tests: the section shows only when opened from the terminal strip, the live gate state and countdown,
  click outside closes it, Escape closes it, a click in the terminal closes it and focuses the terminal.
- Changelog: `changelog/ui/2026-10-04-u-new-details-debug-section.md`.
- Commit subjects: one line, no body, no trailers. Commit on this branch only. Never on main or claude/main.
- Go builds go to `build.claude/`. Clear `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` before `go test`.
- Do not merge, deploy, or restart atrium, the hub or a room. Do not edit CLAUDE.md files.
- Prose: no em-dashes, no semicolons, wrap at 120.

## Finish
Write REPORT.md in the worktree root (what changed, design notes, test results with the command, PNG paths), commit
it, then send ONE atrium_report: `done <sha>, REPORT.md` or `incomplete <sha>, state in REPORT.md`. Nothing else: no
start, progress or status messages.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.
