- The new-context cycle no longer retypes the limit prompt into a turn that begins while it decides. It reads the turn
  count before the busy state, so a turn starting between the two reads cannot pair a new count with a stale "idle"
  (ci-red-fixes).
- The board's switcher check lifts `forgeHostLabel` with the code that needs it, the two units that clicked the rooms tab
  open it with `switchView` since the tab can sit in the `...` menu, and two native `title` tooltips became `data-tip`
  and `aria-label`.
- The Windows PowerShell tests read the curly quotes in their hostile folders as UTF-8, and `test-room-account.ps1` keeps
  its mac checks inside the unix-only block that defines `Say`.
- `internal/store/forgecfg_test.go` is gofmt clean, and the time guard allows the api package's parallel git rigs 33s a test.
