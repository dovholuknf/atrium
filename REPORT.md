# r-new-macos-cpu

Not previously landed (checked claude/main).

## Changed
- `internal/roomstats/cpu_darwin.go`: one long-lived `top -l 0 -n 0 -s 5` child, started on first read. Each "CPU usage" line (first one skipped) adds 1000 synthetic ticks, idle share of them from the idle percent. Restarts after 30s if top exits.
- `plat_machine_darwin.go`: ReadMachine sets HasCPU and the counters once a sample exists, so the existing series, `cpu_pct` and IdleMeter work unchanged.
- Comment fixes in machine.go and idle.go. Changelog added.

## Source chosen and why
Options 1 and 2 are out: no cgo-free path to host_statistics (x/sys has no Mach calls, no purego in go.mod), and no CPU tick sysctl on macOS. top streaming is the last-resort option but costs one process total, not one per tick. A per-tick `top -l 1` blocks about 1s each, so it was rejected.

## Tests
`go test ./internal/roomstats` passes (new parseTopIdle test). Live on m1mini: after about 17s ReadMachine returned HasCPU true with advancing counters. Windows and linux cross-build OK, vet clean. Platforms with no CPU (plat_machine_other) are untouched.

## Left
Not deployed, so the live board is unchecked. Sample interval is 5s, constant `topSampleSecs`.
