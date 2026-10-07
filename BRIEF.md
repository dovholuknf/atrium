# H7: nothing gets lost (r-nothing-gets-lost)
Trace every message path (say same room, cross room via hub, parked/done/dead/restarting card, wake, reports, open
questions, launcher notices). Fix every drop point: accepted messages stored durably first, visible until delivered or
dismissed; a say to a card with no session is KEPT and delivered when it next runs; wake works on a done card with a
conversation; nothing expires silently; board shows undelivered count; tests per drop point (go test -run).
Changelog: changelog/runtime/2026-10-07-r-nothing-gets-lost.md and docs/changes/r-nothing-gets-lost.md. Migrations at
END of schema.go. One-line commits, no trailers. git push hub claude/r-nothing-gets-lost. Then atrium_say
orchestrator-sg4-control@sg4-control "done <sha>: ..." or "blocked: ...". No atrium_report, no deploy, no push origin.
