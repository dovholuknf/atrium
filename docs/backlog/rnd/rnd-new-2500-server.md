# rnd-new-2500-server: the best atrium room machine for $2,500

Filed by the orchestrator, 2026-10-01, from clint: "if you had 2500 dollars to make the most kickass atrium server,
how would you spend it".

## The workload, measured

- Many concurrent Claude Code / Codex CLI sessions (node processes, roughly 250-350 MB each, mostly idle waiting on
  the API). Today: up to ~15 claude processes on sg4 plus directors and workers across rooms.
- The sharded headless board suite: one Chromium per shard. Benchmark today (claude/main, default shards):
  sg4 (i9-13900H, 20 threads, 64 GB) 3m28s, 79% avg CPU, timer drift p99 104ms (lags), sg3 (12 threads) 3m10s,
  m1mini (M1, 8 cores, 16 GB?) 4m08s, drift p99 5ms. Wall time is bound by total work, not core count.
- Go builds and `go test ./internal/daemon` (296s on sg4), many git worktrees, sqlite, a few GB of disk per worktree.
- No local model inference today: the models are API-side. Say whether a local model would earn a share of the
  budget (embedding, a small reviewer, codex-style local runs) or not.
- Memory pressure is real: sg4 sat at 57/64 GB today, 12 GB of it a leak.

## Not necessarily one machine

clint: "it doesn't have to be ONE server... 5 @ 500 or 1 @ 2500, all viable." Compare the split explicitly: one big
box, two or three mid boxes, five small ones (mini PCs, used Mac minis). Atrium is already multi-room through the
hub, so weigh what more rooms cost (provisioning per room, git sync, the session cap per room, failure isolation,
power and noise) against one machine's limits (a single point of failure, memory ceiling).

## What the design must answer

1. Two or three concrete builds at current prices (parts list with links and prices, or a prebuilt / mini PC /
   Mac mini or Mac Studio), each with what it buys for this workload: concurrent sessions, suite wall time, build
   time.
2. OS: Linux vs Windows vs macOS for a room, given atrium's rooms today (Windows sg4/sg3, macOS m1mini) and what
   provisioning supports.
3. Single-thread speed vs cores vs RAM vs fast NVMe: which one actually moves the numbers above. Use today's
   benchmark to argue it.
4. Always-on concerns: idle power, noise, auto-restart after power loss (m1mini went down today after a power cut),
   remote management, UPS.
5. One recommendation, and what it replaces or frees (could sg4 stop hosting work entirely?).

## Output

A short design doc published on the hub, recommendation first. Research only, buy nothing.
