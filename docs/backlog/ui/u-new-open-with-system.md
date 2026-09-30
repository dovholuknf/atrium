# u-new-open-with-system: open a folder in the file manager, a file in the system editor, or in atrium

Status: parked (clint, 2026-09-30). @ui the menu, @runtime the endpoint. Design review by @rnd first: this starts
processes on the room's machine, and `internal/api/fileopen.go` explains why that is fenced.

## Why

clint wants, on folders: **open in Explorer** (Finder on macOS, the file manager on Linux). On files: **open with
the system editor** as well as **open in atrium**. Only where it makes sense: when the room is on the machine the
board is being used from, as it is for clint on sg4.

## What is already there

- `internal/api/fileopen.go`: open a file in an editor on the room's machine. Off until the operator configures
  `editor_command`, never a shell, the path resolved through `internal/safepath` against the card's worktree.
- `internal/api/filetext.go`: the board's own text editor behind a content-hash precondition. That is "open in
  atrium".

## Wanted

- **Open folder:** `explorer.exe <dir>` on Windows, `open <dir>` on macOS, `xdg-open <dir>` on Linux. A known
  program per platform, so no configuration. Argument only, never a shell, the directory through `safepath`.
- **Open with system editor:** the platform's default handler for the file (`open`, `xdg-open`, and on Windows
  the file association without `cmd /c start`). REFUSE anything the default handler would EXECUTE: `.exe .bat
  .cmd .ps1 .vbs .js .msi .lnk .scr .com .hta .sh`, anything with the executable bit on macOS and Linux, and any
  extension with no association. Those go to the configured editor, or the menu item is not shown. "Open" on a
  script is running it.
- **Open in atrium:** the existing text editor, always available.
- **Only when local.** Show the two system items only when the room is on the hub's own machine AND the request
  came over the hub's loopback listener, not over a share or an overlay. Every request over a share presents as
  loopback (`internal/api/CLAUDE.md`), so the check is the listener, not the address. A room on sg3 opened from
  sg4's board would put a window on sg3's desktop, where nobody is looking.

## Open for the design

- **Which desktop the window appears on.** On sg4 the room runs as `claude` and clint is logged in as `clint`. A
  process the claude-user daemon starts may land in claude's session and never show on clint's screen, or
  Explorer may hand off to clint's already running Explorer. Prove it by hand before building anything, and if it
  does not show, say what would (a helper in clint's session that the daemon asks).
- Whether the folder item applies to the card's worktree only, or to any folder in the file browser.
