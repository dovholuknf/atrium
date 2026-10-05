# f-place-cpu

## Built
- `internal/roomstats/idle.go`: `IdleMeter`, idle CPU percent over the window between two asks, from the existing
  `ReadMachine` counters (Windows, Linux). Darwin has no counters without cgo, so `plat_idle_darwin.go` estimates idle
  from `vm.loadavg` over the CPU count. Other platforms give nothing.
- Piggybacks on the room beat: `note.IdleCPU` (a pointer, `idle_cpu`), set by `Room.IdleCPU` in `Room.beat`. No new
  connection or timer. The hub keeps it on `attached`, exposed as `Attached.IdleCPU` (nil when never sent).
- `placePRRoom` orders with `roomLoad.lessLoaded`: sessions, then a room with a figure before one without, then more
  idle CPU, then room name.
- Wired in `internal/cli/roomrun.go`.
- Tests: `TestRoomLoadOrdering`, `TestPlacementBreaksATieOnIdleCPU` (one room sends no figure), two `IdleMeter` tests.

## Decisions
- The window is the beat interval (5s), so the first beat after attach carries no figure.
- Out of range figures from a room are ignored. The last good figure is kept, it is not aged out.
- Ties on equal idle go to the name, no tolerance band.
- Darwin's figure is a load estimate, good for ranking only.

## Not done
- No figure shown on the board (off limits). `Attached.IdleCPU` is there for it.
- Many other `internal/link` tests fail here on `unable to access 'NUL'` from git on Windows. They are not touched by
  this work. The placement, claim and roomstats tests pass.
