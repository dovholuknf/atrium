# docs

One folder per owning area. The entry points everybody reads stay here at the top.

| Where | What | Owner |
|---|---|---|
| `docs/` | `architecture-v2.md`, `how-atrium-works.md`, `user-guide.md`, `atrium-for-agents.md`, `backlog.md`, `decisions.md`, `decisions-log.md`, `test-plan.md`, `changes/` | shared |
| `backlog/` | one file per item, `backlog/<dept>/<id>.md`. `backlog-2.md` is a pointer here | each director its own folder |
| `rnd/` | designs not yet built, research, spikes, reads of other tools | @rnd |
| `runtime/` | the daemon, the store, hooks, messaging, launching, keep-alive, intake | @runtime |
| `terminal/` | the pseudo terminal, attach, input, resize | @terminal |
| `ui/` | the board | @ui |
| `fabric/` | the hub, rooms, overlays, reaching atrium from elsewhere | @fabric |
| `review/` | code review, its director and its memory | @review |
| `release/` | packaging, landing work, the test plan that is its own file | @merge |
| `orchestrator/` | the orchestrator's boot, wrap-up and standing notes | @orchestrator |

**Where a new doc goes.** A design that is not built yet goes in `rnd/`, whoever writes it. When it is built, the
director of the area that owns the code moves it into that area's folder with `git mv`, and fixes every path that
names it (`git grep docs/rnd/<name>`). A reference for code that exists goes straight into its area.

`test-plan.md` and `changes/` stay at the top because every brief, `DIRECTOR.md` and `CLAUDE.md` name them there,
and `scripts/fold-changes.ps1` reads them there.
