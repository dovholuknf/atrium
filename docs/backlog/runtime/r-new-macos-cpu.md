# r-new-macos-cpu: room stats report CPU on macOS

Asked by clint 2026-10-01, after the m1mini move could not be judged on CPU.

## What is wrong

`GET /v1/room/stats` for m1mini has `mem_series_pct` and no `cpu_pct` or `cpu_series_pct`.
`internal/roomstats/plat_machine_darwin.go` says why: per-CPU ticks come from `host_statistics`, which it calls cgo,
and macOS has no `kern.cp_time`. So the board shows no CPU for a Mac room, and nobody can say whether moving work
to m1mini is loading it.

## What is wanted

CPU percent on darwin, in the same series the other platforms fill, with no cgo (the stack rule is pure Go).

Options to check, cheapest first:

- `host_processor_info` or `host_statistics64` through `golang.org/x/sys/unix` or a raw Mach trap, if reachable
  without cgo.
- `sysctl kern.cp_time` style counters under another name on arm64 macOS.
- Shelling out to `top -l 1` or `iostat` on each tick. Last resort: it costs a process per sample.

## Done means

m1mini's board shows a CPU series. The existing test that CPU is absent where the platform has none still holds
for platforms that really have none.
