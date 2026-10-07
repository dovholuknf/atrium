# Doc overhaul: audit and plan

Status: plan by m-doc-overhaul, 2026-10-07, off `claude/main` 3be95cab. The interview is below, then the batches run
in order on `claude/m-doc-overhaul`.

## What is wrong, in one paragraph

The docs describe three different atriums. The root `AGENTS.md`, `README.md`'s quick start, `website/docs/install.md`
and `quick-start.md` describe `atrium daemon`, one process on :7777 and :7778, with Mode A and Mode B beside it.
`docs/how-atrium-works.md` and `docs/atrium-for-agents.md` describe `atrium2 hub` and `atrium2 room` on :7800 and
:7801, a second binary that no longer exists. The code is a third thing: one `atrium` binary, `atrium run` serving the
hub (board on 127.0.0.1:7778, rooms dial in on 127.0.0.1:7779), and `atrium room` on each machine (agents on
127.0.0.1:7777, its own fallback board on 127.0.0.1:7781), with `atrium rooms add|ls|token|mark|rm|log|legacy|git` on
the hub's side and cross-room `name@room` addressing. Nobody reading only the docs can find that out. The rest of the
damage is ordinary: 100 or so design docs with no line saying whether they were built, five docs explaining the same
startup five ways, and dated status snapshots that read as current.

## Ground truth used for the audit

| Fact | Where the code says it |
| --- | --- |
| Hub board 7778, link 7779, room agents 7777, room board 7781 | `internal/cli/atrium_defaults.go:19` to `:22` |
| `atrium run` starts the room detached when none answers, `--no-room` serves the hub alone | `internal/cli/atrium_runroom.go:37` |
| `atrium room` is the database, terminals and agents, plus its own loopback board as the escape hatch | `internal/cli/roomrun.go:223` |
| `atrium room join <string>` joins once, `atrium rooms ...` manages rooms from the hub | `internal/cli/roomrun.go:49`, `internal/cli/atrium_rooms.go:35` |
| `atrium daemon` still exists: one process, :7777 and :7778, no hub | `internal/cli/cli.go:148` |
| The hub holds no work, only rooms, names, join strings and a cache | `internal/cli/atrium_run.go:88` |
| Room state lives in `<state dir>/room`, hub state in `<state dir>/hub` | `internal/cli/atrium_defaults.go:28` |
| A terminal can outlive the room through the pty host, opt-in by the `pty_host` setting | `internal/daemon/hostterm.go:17` |
| Mode A (`hub`, `agent`, TUI) and Mode B (`serve`, `status`, `watch`) are gone, so are `internal/hub`, `agent`, `tui`, `server`, `state` | `internal/cli/cli.go:110`, `ls internal/` |
| `docs/backlog/<dept>/*.md` is still read by code, for item gates and review verdicts | `internal/link/deps.go:735`, `internal/deployready/deployready.go:181` |
| Nothing folds `changelog/<dept>/` into `CHANGELOG.md` | no reference in `scripts/*release*` |

## Audit

### The bulk collections

| Collection | Files | What it is | Verdict |
| --- | --- | --- | --- |
| `docs/backlog/<dept>/` | 546 | one file per backlog item, plus `README.md` and `HISTORY.md` | KEEP. Code reads it (`deps.go`, `deployready`), and items are still written daily. FIX `README.md` to say the live backlog is on the hub (`atrium backlog`, `atrium_backlog`) and these files are what `atrium backlog import` reads. Status lines are free text: 30 `DONE`, 8 `done`, 30 `not started`, 23 `HELD`, many one-offs |
| `docs/changes/` | 77 | pending test-plan sections, folded by `scripts/fold-changes.ps1` | KEEP. Its README is correct. 76 are waiting for a fold, which is a merge-process gap and not a doc fix |
| `changelog/<dept>/` | 365 | one changelog entry per change since 2026-09-29 | KEEP. FIX `CHANGELOG.md`'s header so a reader knows the newest entries are not in it |
| `docs/blog/` | 49 | post stubs, 41 ideas and 6 outlines, nothing drafted, not published by `website/` | KEEP as is. Not reader-facing yet. `from-chat-window-to-kanban` and `inventory.md` mention Mode A as history, which is right |
| `docs/screens/`, `notes/` | images | screenshots for docs and reviews | KEEP |

### Every other doc

Verdicts: **KEEP** is current and correct. **FIX** is a current reference with named wrong lines. **REWRITE** is a
current reference that is mostly wrong. **STATUS** is a design, spike or plan that stays as the record of what was
decided, and gets one `Status:` line at the top, checked against the code, and nothing else. **(check)** means the
build status is not settled yet and the batch settles it from the code before writing the line. **MERGE** and
**MOVE** name the target. **DELETE?** waits for the interview. **THIRD PARTY** is a read of another tool, which the
`*-comparison*.md` rule in `.gitignore` says stays local.

"Before 09-29 (reorg)" means the last commit was the docs reorg `340c2f9c`, which moved files without changing them.

| Path | Lines | Last real change | What it is | Verdict |
| --- | --- | --- | --- | --- |
| `CHANGELOG.md` | 3077 | 2026-09-29 | Changelog | FIX header only: frozen 09-29, nothing folds `changelog/` back in, so a reader sees no change after 09-29. Say where newer entries are. History untouched |
| `FEATURES.md` | 712 | before 09-29 (reorg) | Features | FIX: "Rooms and the hub" still calls `cmd/atrium2` a shim and uses `atrium2 hub token`. Otherwise the best capability index |
| `README.md` | 307 | 2026-10-02 | Atrium | REWRITE: quick start runs `atrium daemon` on :7777/:7778, "one machine, for one person", "runner dies with the daemon" (pty_host exists), docs list points at a design-not-built doc and the old backlog. Feature prose duplicates FEATURES.md |
| `docs/README.md` | 23 | before 09-29 (reorg) | docs | FIX: becomes the doc map and reading order. Says rnd holds "designs not yet built" while half of it is built |
| `docs/architecture-v2.md` | 657 | before 09-29 (reorg) | Architecture v2: the daemon, the API, and the clients | STATUS historical: first line says "design, not yet built", body is the v2 daemon design of 09-01, built and since split into hub and room |
| `docs/atrium-for-agents.md` | 110 | before 09-29 (reorg) | What atrium is, for an agent about to be wired into it | REWRITE: `.atrium2\bin\atrium2.exe`, "the hub binary" vs "the hook binary", which are one binary now |
| `docs/backlog-2.md` | 1 | before 09-29 (reorg) |  | OFF LIMITS (one-line pointer) |
| `docs/backlog.md` | 306 | before 09-29 (reorg) | What is left | OFF LIMITS. 16 dead links into `docs/backlog/backlog-*.md`, gitignored files. Listed as a gap only |
| `docs/context-cycle-design.md` | 167 | 2026-10-05 | Context cycle, design | MOVE to `docs/runtime/`, STATUS line. Top level is for entry points |
| `docs/context-cycle-plan.md` | 63 | 2026-10-05 | Context cycling plan, interview with clint, 2026-10-05 | MERGE into context-cycle-design.md as its "decisions" section |
| `docs/decisions-log.md` | 89 | before 09-29 (reorg) | Decisions log | KEEP, FIX pointer to gitignored `docs/interview-log.md` |
| `docs/decisions.md` | 827 | before 09-29 (reorg) | Decisions | STATUS: the hub/room decisions 1 to 19, still named `atrium2` throughout. Rename to `docs/fabric/hub-decisions.md`, status line naming `atrium run` / `atrium rooms` |
| `docs/fabric/audit-design.md` | 122 | before 09-29 (reorg) | An audit log for the hub and the rooms | STATUS (check built) |
| `docs/fabric/card-room-routing.md` | 97 | before 09-29 (reorg) | A card id carries its room end to end | STATUS (check built) |
| `docs/fabric/cross-room-say-design.md` | 299 | 2026-10-02 | Cross-room `atrium_say` (backlog-2 item 58) | STATUS built |
| `docs/fabric/f-003-resources-design.md` | 67 | 2026-09-30 | An inventory of what an agent may use (f-003) | STATUS built (`atrium resources`) |
| `docs/fabric/f-004-two-rooms-design.md` | 111 | 2026-09-30 | Two rooms on one machine, and moving a card to another machine (f-004) | STATUS (check built) |
| `docs/fabric/hub-restart-gate.md` | 189 | 2026-10-05 | The hub restart gate | KEEP, reference |
| `docs/fabric/hub-room-plan.md` | 280 | before 09-29 (reorg) | A hub that serves the board, and rooms that run the agents | MERGE into new `docs/fabric/hub-and-rooms.md`, then STATUS historical. `atrium2`, :7800/:7801 |
| `docs/fabric/hub-room-requirements.md` | 211 | before 09-29 (reorg) | The hub is a multi-tenant view of rooms | MERGE into `hub-and-rooms.md`, then STATUS historical |
| `docs/fabric/one-atrium-cutover.md` | 324 | before 09-29 (reorg) | The one-atrium cutover on this machine | STATUS historical: the cutover is done, scripts/live runs `atrium run --no-room` |
| `docs/fabric/one-atrium-plan.md` | 632 | before 09-29 (reorg) | One atrium: one binary, no Mode A, and the hub renamed | STATUS historical: one binary shipped, Mode A removed |
| `docs/fabric/overlays.md` | 463 | before 09-29 (reorg) | Reaching the board from elsewhere | FIX: still says board loopback is the only shape. Check against `atrium rooms` transports |
| `docs/fabric/pin-order-rooms-design.md` | 94 | before 09-29 (reorg) | A pinned strip across rooms (backlog-2 item 52) | STATUS (check built) |
| `docs/fabric/remote-launch.md` | 265 | before 09-29 (reorg) | Sending work to another machine | KEEP, says shipped |
| `docs/fabric/room-requirements-design.md` | 270 | before 09-29 (reorg) | What a room needs to take a project's work: `atrium.requirements.yaml` | STATUS built (`atrium.requirements.yaml`, `atrium requirements`) |
| `docs/fabric/runner-scoping-design.md` | 76 | before 09-29 (reorg) | Room-scoped runners | STATUS (no trace in code, likely proposed) |
| `docs/fabric/ziti-zrok-flow-design.md` | 221 | before 09-29 (reorg) | The OpenZiti and zrok flow for atrium | STATUS, `atrium2` refs |
| `docs/fabric/ziti-zrok-flow-design.mercurius-synopsis.md` | 53 | before 09-29 (reorg) | Session synopsis | DELETE? a review-round synopsis, its decisions are in the design |
| `docs/fabric/zrok-share-500.md` | 92 | before 09-29 (reorg) | Every share on api-v2.zrok.io returns 500 with an empty body | DELETE? or KEEP as incident note. Blog post `the-zrok-outage-that-was-not-ours` covers it |
| `docs/how-atrium-works.md` | 175 | before 09-29 (reorg) | How atrium works | REWRITE: the one architecture page. Diagram says `atrium2 hub` on :7800/:7801, `cmd/atrium2`, room board on :7778. Truth: hub board :7778, link :7779, room agents :7777, room board :7781, one binary |
| `docs/orchestrator/cold-start.md` | 113 | before 09-29 (reorg) | cold-start.md, orchestrator boot sequence | STATUS or DELETE? orchestrator boot from 09-06, names Mode A |
| `docs/orchestrator/dispatch-queue.md` | 1664 | before 09-29 (reorg) | Queued work, not yet dispatched | DELETE? 1664 lines of a 09-06 dispatch queue, superseded by the hub backlog |
| `docs/orchestrator/overnight-brief.md` | 335 | before 09-29 (reorg) | Overnight brief, 2026-09-06 | DELETE? a dated 09-06 brief |
| `docs/orchestrator/status.md` | 83 | before 09-29 (reorg) | Where everything stands, 2026-09-06 | DELETE? "where everything stands, 2026-09-06" |
| `docs/orchestrator/wrapup.md` | 87 | before 09-29 (reorg) | wrapup.md, orchestrator wave-boundary checklist | STATUS or DELETE? names Mode A |
| `docs/release/merge-hygiene.md` | 42 | before 09-29 (reorg) | Merge hygiene | KEEP |
| `docs/release/packaging.md` | 924 | 2026-10-04 | Packaging: installing atrium, running it as a service, and what pub... | FIX: launchd line runs `atrium daemon`. Check service scripts for the command they run |
| `docs/release/release-0-0-1-plan.md` | 108 | 2026-10-05 | Release 0.0.1: the plan | STATUS: says READY, check whether 0.0.1 was cut |
| `docs/release/test-plan-z-providers.md` | 234 | before 09-29 (reorg) | Z. Providers: telling atrium where your repositories live | MERGE into `docs/test-plan.md` section Z, or KEEP with a pointer |
| `docs/review/cr2-ada747b-2ce6443.md` | 170 | 2026-10-02 | cr2: correctness review of claude/main ada747b..2ce6443 | DELETE? a one-off correctness review, findings filed as backlog items |
| `docs/review/cr48-2d0ac78-ada747b.md` | 177 | 2026-09-29 | cr48: correctness review of claude/main 2d0ac78..ada747b | DELETE? same |
| `docs/review/item16-notes.md` | 193 | before 09-29 (reorg) | Item 16 reading notes: reviews that remember | DELETE? reading notes for review-memory-design |
| `docs/review/peer-review-brief.md` | 64 | before 09-29 (reorg) | brief: atrium peer review using the mercurius review protocol | DELETE? a one-off agent brief |
| `docs/review/review-memory-design.md` | 670 | 2026-10-01 | Reviews that remember (backlog-2 item 16) | STATUS (check built) |
| `docs/review/review-pr-start-design.md` | 145 | before 09-29 (reorg) | A PR card that registers itself with @review | STATUS proposed |
| `docs/review/security-audit-2026-09-30.md` | 320 | 2026-09-30 | Security audit, 2026-09-30 | KEEP as a dated record |
| `docs/rnd/agent-lineage-design.md` | 208 | before 09-29 (reorg) | Agent lineage: who spawned whom, and showing it | STATUS (check) |
| `docs/rnd/ai-platform-fit.md` | 342 | before 09-29 (reorg) | Does atrium fit the rest of the stack | STATUS research |
| `docs/rnd/background-hold-design.md` | 40 | before 09-29 (reorg) | 31 design: hold STUCK while background work runs | STATUS (check) |
| `docs/rnd/bb.md` | 356 | before 09-29 (reorg) | bb: what it is, and what atrium should do about it | THIRD PARTY: a read of another tool. .gitignore policy says local only. DELETE? or keep |
| `docs/rnd/card-urls-design.md` | 215 | 2026-09-30 | Card URLs: a card has an address made of names, and every share kee... | STATUS built (/alias/) |
| `docs/rnd/change-lifecycle-design.md` | 280 | 2026-10-02 | Change lifecycle: the stages a change goes through, each with its g... | STATUS (check) |
| `docs/rnd/change-record-design.md` | 188 | 2026-10-02 | Change record: atrium keeps "where are we" for every branch and PR,... | STATUS proposed |
| `docs/rnd/changes-view-design.md` | 95 | 2026-09-29 | A Changes view on every card (design) | STATUS built? (`/changes` route exists) |
| `docs/rnd/charon.md` | 786 | before 09-29 (reorg) | Charon: what it is, and what atrium should do about it | THIRD PARTY, as bb.md |
| `docs/rnd/competitor-features.md` | 342 | 2026-09-29 | Competitor features: what atrium should build next | THIRD PARTY, as bb.md |
| `docs/rnd/competitors.md` | 755 | before 09-29 (reorg) | Competitors: the field around atrium, and whether atrium can be ext... | THIRD PARTY, as bb.md |
| `docs/rnd/config-alignment-design.md` | 347 | 2026-10-02 | Config alignment: one config for every session atrium starts, owned... | STATUS proposed |
| `docs/rnd/context-limit-ownership-design.md` | 348 | 2026-10-02 | Who owns the context limit: atrium's new context or the runner's co... | MERGE into runtime/context-cycle-design.md, or STATUS superseded by it |
| `docs/rnd/defender-advice-design.md` | 147 | 2026-09-30 | Defender advice (r-new-defender-advice) | STATUS built (`scripts/room-defender.ps1`) |
| `docs/rnd/event-sink-stage3-design.md` | 176 | before 09-29 (reorg) | Event sink, stage 3: the permission table | STATUS (check) |
| `docs/rnd/everywhere-card-design.md` | 231 | before 09-29 (reorg) | A card on every room (backlog-2 item 49) | STATUS: says "designed, not built", check |
| `docs/rnd/f-011-stage0-spike.md` | 234 | 2026-09-29 | f-011 stage 0: the pty host spike | MERGE into rolling-restart-design as an appendix, or STATUS |
| `docs/rnd/factory-landscape.md` | 151 | 2026-10-01 | Software factories: the landscape, and what atrium takes from it (S... | THIRD PARTY (23 surveyed tools) |
| `docs/rnd/factory-shape.md` | 271 | 2026-09-30 | The factory: how it is shaped, what it costs, and whether to change it | STATUS research |
| `docs/rnd/federation-design-v2.md` | 667 | before 09-29 (reorg) | Federation v2: a forum, and the atria that report to it | STATUS superseded by the hub/room split (`hub-and-rooms.md`). README and AGENTS still point at it as current |
| `docs/rnd/federation-design.md` | 447 | before 09-29 (reorg) | Federation: one board, many machines | STATUS superseded, as v2 says |
| `docs/rnd/forum-implementation.md` | 1047 | before 09-29 (reorg) | The forum: implementation plan | STATUS superseded: the forum became the hub |
| `docs/rnd/freeze-budget-design.md` | 238 | 2026-09-30 | A board freeze, and a token and tool-call budget that trips it (des... | STATUS (check) |
| `docs/rnd/gateway-fit.md` | 263 | 2026-09-29 | mcp-gateway, llm-gateway and sterling: what atrium should take, lea... | STATUS research, done |
| `docs/rnd/git-sync-design.md` | 341 | 2026-09-30 | f-019: git sync over the hub and rooms (design) | STATUS built (`internal/gitsync`), MOVE to fabric |
| `docs/rnd/handle-addressed-http-design.md` | 210 | 2026-09-30 | Handle-addressed HTTP: a script names a card the way an agent does,... | STATUS (check) |
| `docs/rnd/held-message-escalation-design.md` | 160 | 2026-09-30 | Held-message escalation: what has waited too long says so once, and... | STATUS (check) |
| `docs/rnd/hook-coverage-spike.md` | 510 | before 09-29 (reorg) | Hook coverage | STATUS spike |
| `docs/rnd/hub-documents-design.md` | 247 | 2026-09-30 | Hub documents: a place to share what agents and clint write | STATUS built (`internal/link/docs_api.go`) |
| `docs/rnd/hub-forge-answers.md` | 47 | 2026-10-04 | Hub forge interview answers (clint) | MERGE into hub-forge-design.md |
| `docs/rnd/hub-forge-design.md` | 315 | 2026-10-02 | The hub as a forge: rooms push finished work to the hub, and the hu... | STATUS built (forge), MOVE to fabric |
| `docs/rnd/interviewer-brief.md` | 63 | 2026-10-02 | Interviewer brief: the template for a card that interviews clint on... | KEEP, a standing template |
| `docs/rnd/keepalive-marked-spec.md` | 59 | before 09-29 (reorg) | Keep-alive warms the cards you mark, not every idle card (backlog-2... | MERGE into keepalive-policy-design.md |
| `docs/rnd/keepalive-policy-design.md` | 703 | before 09-29 (reorg) | Keep-alive and restart policy: only what a human is using (backlog-... | STATUS: says "nothing here is built", check against `runtime/cache-keepalive-design.md` (built) |
| `docs/rnd/langchain-openwiki-spike.md` | 236 | 2026-10-02 | LangChain and OpenWiki: does either fit atrium today? (SPIKE) | THIRD PARTY spike |
| `docs/rnd/lean-context-cycle-design.md` | 154 | 2026-09-30 | Lean context cycle (r-new-lean-context-cycle) | MERGE into runtime/context-cycle-design.md, or STATUS superseded |
| `docs/rnd/local-proxy-trust-design.md` | 108 | 2026-09-30 | Loopback is not the operator behind a local proxy | STATUS (check) |
| `docs/rnd/long-turn-checkin-design.md` | 377 | 2026-10-02 | The wtf-o-meter: atrium asks a long turn what it is doing, and its ... | STATUS (check) |
| `docs/rnd/machine-health-design.md` | 142 | 2026-09-30 | Machine health in the gear, and "profile this machine" (u-new-machi... | STATUS (check) |
| `docs/rnd/merged-cull-design.md` | 125 | before 09-29 (reorg) | A merged worker is culled without anybody remembering to | STATUS built (`atrium merged`) |
| `docs/rnd/mobile-research.md` | 138 | before 09-29 (reorg) | Atrium on a phone: research for the mobile workshop | STATUS research. `/m` shipped |
| `docs/rnd/multi-room-design.md` | 272 | before 09-29 (reorg) | More than one room per machine, and a room that drains | STATUS (check), feeds `hub-and-rooms.md` |
| `docs/rnd/multi-tenant-decision.md` | 345 | before 09-29 (reorg) | Multi-tenant atrium, and the decision not to build it yet | KEEP, a decision record |
| `docs/rnd/open-with-system-design.md` | 130 | 2026-09-30 | Open with the system (u-new-open-with-system) | STATUS (check) |
| `docs/rnd/opencode-token-routing.md` | 242 | 2026-10-02 | OpenCode token routing: using clint's Kimi and OpenCode Go models w... | STATUS (check) |
| `docs/rnd/operator-focus.md` | 412 | 2026-10-02 | Operator focus: one numbered list of decisions, shorter reports, an... | STATUS (check) |
| `docs/rnd/orca.md` | 388 | before 09-29 (reorg) | Orca: what it is, and what atrium should do about it | THIRD PARTY, as bb.md |
| `docs/rnd/otel-export-design.md` | 269 | 2026-10-02 | OpenTelemetry export: atrium sends traces, metrics and logs to a co... | STATUS (check) |
| `docs/rnd/overlay-room-identity.md` | 156 | 2026-09-29 | Decision 18: what proves a room's name over an overlay (rd-003) | STATUS built (rooms legacy allow/refuse), decision 18 |
| `docs/rnd/owed-report-design.md` | 123 | before 09-29 (reorg) | A card owes its launcher a report only for a prompt its launcher sent | STATUS built |
| `docs/rnd/persistent-growler-design.md` | 333 | 2026-09-30 | The persistent growler: an alert that stays until you act on it or ... | STATUS built (growl) |
| `docs/rnd/postgres-probe.md` | 418 | before 09-29 (reorg) | Postgres: what actually broke | STATUS research |
| `docs/rnd/pr-ci-state-design.md` | 122 | 2026-09-29 | PR and CI state on the card, and feedback back to the card that own... | STATUS (check) |
| `docs/rnd/pr-review-workflow.md` | 199 | 2026-10-05 | The PR review workflow: paste, review, walk every finding (rnd, 202... | KEEP, current (10-05) |
| `docs/rnd/process-registry-design.md` | 711 | before 09-29 (reorg) | Processes: a long-running background process an agent starts, owned... | STATUS proposed, says nothing built |
| `docs/rnd/pulls-api.md` | 320 | 2026-10-04 | The pulls API: routes and JSON (r-pr-store, part 0) | MOVE to review/, reference for built routes |
| `docs/rnd/pulls-view-design.md` | 503 | 2026-10-01 | The pulls view: pull request review that atrium runs itself (rnd-ne... | STATUS built |
| `docs/rnd/reply-suggestions-design.md` | 139 | 2026-10-04 | Reply suggestions (rd-new-reply-suggestions) | STATUS built (RS1, RS4) |
| `docs/rnd/reports-channel-design.md` | 55 | 2026-10-05 | Reports and backlog that every room can reach | STATUS built (`atrium reports`, `atrium backlog`) |
| `docs/rnd/restart-idle-spec.md` | 70 | before 09-29 (reorg) | A restart resumes only the cards that were working (backlog-2 item ... | MERGE into keepalive-policy-design.md |
| `docs/rnd/resume-says-continue-design.md` | 85 | 2026-09-30 | Resume says continue: a card the restart interrupted is told to car... | STATUS (check) |
| `docs/rnd/review-on-atrium-design.md` | 490 | 2026-10-02 | Review on atrium: one call runs the reviewer panel as quiet child c... | STATUS (check) |
| `docs/rnd/review-quality-design.md` | 228 | 2026-10-04 | Review quality: fewer, better findings, and a measure of whether th... | STATUS (check) |
| `docs/rnd/review-tab-design.md` | 840 | before 09-29 (reorg) | The review tab: walking a pull request review on the board (u-005, ... | STATUS (check) |
| `docs/rnd/rolling-restart-design.md` | 410 | 2026-09-29 | Upgrading atrium without stopping your agents (f-011) | STATUS partly built: pty_host is an opt-in setting |
| `docs/rnd/room-autostart-design.md` | 355 | before 09-29 (reorg) | A room that starts again by itself: after a logoff, and after a reboot | STATUS shelved |
| `docs/rnd/room-deploy-hold-design.md` | 208 | 2026-09-30 | Room deploy hold: one call asks for a deploy, every agent holds, th... | STATUS (check) |
| `docs/rnd/room-handoff-design.md` | 329 | 2026-10-01 | Room handoff: `atrium move`, a card and its work from one room to a... | STATUS proposed (`atrium move` does not exist) |
| `docs/rnd/room-machine-2500-design.md` | 136 | 2026-10-01 | The best atrium room machine for $2,500 | STATUS research |
| `docs/rnd/room-to-room-access-spike.md` | 145 | 2026-10-01 | Room-to-room access: a room reaching another room's machine (SPIKE) | STATUS spike |
| `docs/rnd/runner-switch-design.md` | 218 | 2026-10-02 | Runner switch: moving a card to another runner, for example when a ... | STATUS proposed (no trace) |
| `docs/rnd/scheduled-launches-design.md` | 184 | 2026-09-30 | Scheduled launches: a row with a schedule and a launch request, and... | STATUS (check) |
| `docs/rnd/scm-forge-design.md` | 424 | 2026-10-04 | The forge: a provider that knows its forge, so a pasted PR is read ... | STATUS built (`scripts/recognisers`, forge) |
| `docs/rnd/security-design.md` | 443 | 2026-09-30 | Security design: close the browser edge first, then tell the board ... | STATUS (check) |
| `docs/rnd/spike-mcp-gateway.md` | 480 | before 09-29 (reorg) | Atrium and mcp-gateway: absorb, supervise, separate, or rooms | THIRD PARTY spike |
| `docs/rnd/spike-nested-subagents.md` | 210 | before 09-29 (reorg) | Spike: child runners, spawned by an agent, grouped under it | STATUS spike |
| `docs/rnd/state-of-the-art.md` | 195 | before 09-29 (reorg) | State of the art (atrium) | THIRD PARTY, names Mode A |
| `docs/rnd/telegram-notify-design.md` | 119 | 2026-10-01 | A Telegram bot for notifications | STATUS proposed (no trace) |
| `docs/rnd/transparent-rooms.md` | 77 | before 09-29 (reorg) | Rooms you cannot see | STATUS: "a decision being made", check what was decided |
| `docs/rnd/turn-end-spike.md` | 266 | before 09-29 (reorg) | Spike: a launched agent never ends a turn with nobody told | STATUS spike |
| `docs/rnd/usage-tab-design.md` | 304 | 2026-09-29 | The usage tab: cache reads out of the way, and what else it should ... | STATUS built (`atrium usage`, usage tab) |
| `docs/rnd/web-push-design.md` | 296 | 2026-09-30 | Web Push for the phone | STATUS built (`internal/link/push.go`) |
| `docs/rnd/what-am-i-working-on-design.md` | 172 | 2026-10-01 | What am I working on: finding the sessions clint is using right now | KEEP, says closed |
| `docs/room-accounts.md` | 273 | 2026-10-05 | Run a room as its own account | KEEP, MOVE to `docs/fabric/`? recent and correct |
| `docs/runtime/a2a-reliability-design.md` | 557 | before 09-29 (reorg) | Agent-to-agent reliability: no silent stall, no silent loss | STATUS, `atrium2` ref |
| `docs/runtime/activity-design.md` | 121 | before 09-29 (reorg) | Live activity on a card | KEEP reference |
| `docs/runtime/agent-messaging.md` | 215 | before 09-29 (reorg) | How sessions talk to each other | FIX: check against cross-room say and name@room |
| `docs/runtime/auto-mode.md` | 139 | before 09-29 (reorg) | Auto mode, and reading the record afterwards | KEEP |
| `docs/runtime/auto-new-context-design.md` | 463 | 2026-09-30 | Automatic new context at a context threshold (r-029) | MERGE into context-cycle-design.md, or STATUS superseded |
| `docs/runtime/cache-keepalive-design.md` | 513 | before 09-29 (reorg) | Cache keep-alive for idle sessions | KEEP, says built |
| `docs/runtime/codex-update-design.md` | 193 | before 09-29 (reorg) | Keeping codex up to date (backlog-2 item 12) | STATUS (check) |
| `docs/runtime/file-transfer-design.md` | 275 | before 09-29 (reorg) | Moving files, and pasting into a session | KEEP |
| `docs/runtime/hooks.md` | 171 | before 09-29 (reorg) | Wiring the hooks | KEEP, check the location file path |
| `docs/runtime/intake-design.md` | 550 | before 09-29 (reorg) | Intake: starting a card from something that is not a directory | FIX one Mode B line |
| `docs/runtime/item-dependencies-design.md` | 280 | 2026-09-30 | Item dependencies: work that waits on other work, with a gate only ... | STATUS built (itemgate) |
| `docs/runtime/keepalive-fork-args-design.md` | 87 | before 09-29 (reorg) | A keep-alive fork carries the card's launch args | STATUS (check) |
| `docs/runtime/launch-options-design.md` | 161 | 2026-10-02 | Launch options: model, effort, extra args and env (backlog-2 item 48) | KEEP, says built |
| `docs/runtime/lean-workers-design.md` | 167 | 2026-09-29 | Lean workers | KEEP |
| `docs/runtime/mcp-rules-design.md` | 297 | 2026-09-30 | Standing rules that name MCP tools (r-034) | KEEP, says built |
| `docs/runtime/other-runners.md` | 191 | before 09-29 (reorg) | What the other runners will tell you | KEEP |
| `docs/runtime/providers-design.md` | 272 | before 09-29 (reorg) | Providers: where a repository lives on this machine | STATUS (check) |
| `docs/runtime/reload-design.md` | 153 | before 09-29 (reorg) | Reloading atrium from inside atrium | FIX: restarter starts `atrium daemon --db`. Check what control.go restarts now |
| `docs/runtime/restart-wake.md` | 78 | before 09-29 (reorg) | The after-restart wake | KEEP |
| `docs/runtime/runner-setup-design.md` | 261 | before 09-29 (reorg) | Runner setup: making a runner work where atrium launches it | STATUS (check) |
| `docs/runtime/say-lifecycle-design.md` | 284 | before 09-29 (reorg) | A say leaves a record, and a reply asked for stays owed | STATUS (check) |
| `docs/runtime/scm-design.md` | 297 | 2026-10-04 | Source control and ticketing: work that arrives from somewhere else | STATUS: AGENTS.md says "nothing here is built", recognisers shipped |
| `docs/runtime/seen-design.md` | 274 | before 09-29 (reorg) | Seen tracking | STATUS (check) |
| `docs/runtime/statusline-telemetry.md` | 226 | before 09-29 (reorg) | Statusline telemetry | KEEP |
| `docs/runtime/unexpected-exit-wake.md` | 79 | before 09-29 (reorg) | The unexpected-exit notice | KEEP |
| `docs/runtime/work-ledger-design.md` | 558 | before 09-29 (reorg) | The work ledger: who did what, where it is, and who said it was done | FIX `atrium2 ledger` to `atrium ledger` |
| `docs/runtime/work-ledger-plan.md` | 70 | before 09-29 (reorg) | The work ledger, the plan | MERGE into work-ledger-design.md |
| `docs/runtime/worker-gateway-design.md` | 53 | 2026-09-30 | Worker gateway (r-035) | KEEP, built |
| `docs/runtime/worktree-gone-design.md` | 83 | before 09-29 (reorg) | A runner whose worktree is gone is asked to leave | STATUS (check) |
| `docs/terminal/input-lag-logging.md` | 135 | 2026-10-05 | Input-lag logging | KEEP |
| `docs/terminal/multi-pane-input-design.md` | 69 | before 09-29 (reorg) | Shared multi-pane input: keystroke fan-out | STATUS (check) |
| `docs/terminal/ptyhost-protocol.md` | 78 | 2026-10-01 | The pty host protocol, as built | KEEP, "as built" |
| `docs/terminal/supervision-design.md` | 220 | before 09-29 (reorg) | Supervision: owning the runner | FIX: no reattach claim vs pty_host |
| `docs/terminal/terminal-resize-decoupling-design.md` | 114 | before 09-29 (reorg) | Decoupling viewer size from the shared PTY | STATUS (check) |
| `docs/terminal/typing-race.md` | 118 | before 09-29 (reorg) | A typed peer message merges with what the human is typing | KEEP |
| `docs/test-plan.md` | 9032 | 2026-10-06 | Atrium test plan | FIX header: section letters A (Mode A) and E (Mode B) test surfaces that are gone. Mark them removed, keep the letters |
| `docs/ui/board-perf-findings.md` | 105 | 2026-10-05 | Board performance findings, 2026-10-05 | KEEP, dated findings |
| `docs/ui/board-repaint.md` | 113 | before 09-29 (reorg) | Repainting the board without throwing away where you were | KEEP |
| `docs/ui/css-nits.md` | 140 | before 09-29 (reorg) | CSS nits | STATUS or DELETE? a nit list |
| `docs/ui/preview-design.md` | 167 | before 09-29 (reorg) | A second board, on a copy, that acts on nothing | KEEP |
| `docs/ui/switcher-design.md` | 131 | before 09-29 (reorg) | The switcher | KEEP |
| `docs/user-guide.md` | 232 | 2026-10-02 | Atrium user guide | REWRITE: pattern 0 is the daemon logon task, "No multi-host" in limits, reference table has gwt Mode B paths. Unique patterns (ask, finish, defender, preview) MERGE into website/docs, or keep here, see interview |
| `internal/api/web/vendor/VERSIONS.md` | 38 | 2026-10-05 | Vendored, not fetched | KEEP |
| `internal/daemon/testdata/scrollback/README.md` | 44 | 2026-09-29 | Frozen scrollback fixtures | KEEP |
| `scripts/live/README.md` | 78 | 2026-10-04 | scripts/live: running atrium on sg4 | KEEP, operator notes for sg4, correct ports |
| `scripts/notify/README.md` | 17 | 2026-10-04 | Notify command examples | KEEP |
| `scripts/recognisers/README.md` | 118 | 2026-10-07 | Recognisers | KEEP |
| `scripts/sgg/bringup-sgg.md` | 75 | 2026-09-26 | Bringing a second room up on sgg | STATUS historical: `atrium2` bring-up of 09-26 |
| `scripts/sources/README.md` | 84 | before 09-29 (reorg) | Sources | KEEP |
| `website/DECISIONS.md` | 118 | 2026-09-24 | Docs site decisions | FIX: "the gate hook, and `atrium2` for rooms" |
| `website/docs/board.md` | 98 | 2026-10-05 | The board | CHECK at batch time |
| `website/docs/cards.md` | 78 | 2026-09-24 | Cards | CHECK |
| `website/docs/cli.md` | 100 | 2026-10-05 | The CLI | REWRITE: leads with `atrium daemon`, says `atrium2` is a shim |
| `website/docs/control-mcp.md` | 119 | 2026-10-04 | The control MCP server | FIX: MCP url `localhost:7800/_hub/mcp`, the hub is :7778 |
| `website/docs/files.md` | 31 | 2026-09-24 | Files | CHECK |
| `website/docs/history.md` | 43 | 2026-09-25 | History and notifications | CHECK |
| `website/docs/hooks.md` | 168 | 2026-09-24 | Hooks | CHECK, port for the gate hook |
| `website/docs/install.md` | 173 | 2026-09-24 | Install | REWRITE: "a second binary, `atrium2`", start with `atrium daemon` |
| `website/docs/intake.md` | 43 | 2026-09-24 | Intake and sources | CHECK |
| `website/docs/intro.md` | 72 | 2026-09-25 | What atrium is | CHECK |
| `website/docs/messages.md` | 73 | 2026-09-24 | Messages and peers | CHECK |
| `website/docs/modes.md` | 70 | 2026-09-24 | Two ways to run it | FIX: "a room restart ends it" ignores pty_host |
| `website/docs/overlays.md` | 66 | 2026-09-24 | Overlays and sharing | CHECK |
| `website/docs/permissions.md` | 102 | 2026-09-24 | Permissions and auto mode | CHECK |
| `website/docs/quick-start.md` | 77 | 2026-09-24 | Quick start | REWRITE: "it is `atrium daemon`", the two-port paragraph |
| `website/docs/rooms.md` | 200 | 2026-10-05 | Rooms and the hub | FIX: opens by describing `atrium daemon` as the base case |
| `website/docs/runners.md` | 67 | 2026-09-24 | Runners | CHECK |
| `website/docs/settings.md` | 55 | 2026-09-24 | Settings and operations | CHECK |
| `website/docs/story.md` | 166 | 2026-09-24 | Why atrium exists | KEEP, history by design |
| `website/docs/terminals.md` | 84 | 2026-09-24 | Supervised terminals | CHECK |

### Dead links

Only one is real. `docs/backlog.md` links 16 files under `docs/backlog/backlog-*.md`, which `.gitignore` excludes, and
`docs/backlog.md` is off limits here. `website/docs/install.md` and `control-mcp.md` link `rooms.md#start-a-hub`, which
resolves through an explicit `{#start-a-hub}` heading id.

Code comments name docs that do not exist. These are gaps for whoever owns the code, not doc edits:

| Path named | Named from |
| --- | --- |
| `docs/rnd/card-lifecycle-design.md` | `internal/api/open.go`, `internal/api/resources.go`, `internal/cli/open.go`, `internal/link/openroute.go` |
| `docs/interview-log.md` (gitignored) | `internal/api/web/index.html`, `internal/api/web/js/expose2.js` |
| `docs/backlog/backlog-2026-09-1*.md` (gitignored) | `internal/api/web/index.html` |
| `docs/backlog/runtime/r-new-report-no-launcher.md` | `internal/store/ledger.go:1053` |
| `docs/backlog/fabric/f-new-allowed-folders.md` | `scripts/provision-room.ps1` |
| `docs/changes/fabric-1-toolchain.md` | `scripts/room-toolchain.ps1` |

### Duplicated explanations

| Topic | Explained in | After |
| --- | --- | --- |
| How to start atrium | `README.md`, `docs/how-atrium-works.md`, `docs/user-guide.md` pattern 0, `website/docs/install.md`, `quick-start.md`, `rooms.md`, `cli.md`, root `AGENTS.md`, `docs/release/packaging.md` | `website/docs/install.md` and `rooms.md` own it. The others say one line and link |
| The hub and room split | `docs/how-atrium-works.md`, `docs/fabric/hub-room-plan.md`, `hub-room-requirements.md`, `one-atrium-plan.md`, `docs/decisions.md`, `docs/rnd/federation-design-v2.md`, `multi-room-design.md`, `website/docs/rooms.md` | new `docs/fabric/hub-and-rooms.md` is the one current reference. The plans become history |
| The permission chain | root `AGENTS.md`, `internal/daemon/AGENTS.md`, `docs/how-atrium-works.md` | `how-atrium-works.md`, with the package AGENTS.md keeping the "do not reorder" rule |
| Feature descriptions | `README.md` "What it gives you", `FEATURES.md`, `website/docs/*` | `FEATURES.md` is the index, `README.md` keeps a short list |
| Context cycling | `docs/context-cycle-design.md`, `context-cycle-plan.md`, `docs/runtime/auto-new-context-design.md`, `docs/rnd/lean-context-cycle-design.md`, `context-limit-ownership-design.md` | `docs/runtime/context-cycle-design.md`, the others marked superseded |
| Keep-alive | `docs/runtime/cache-keepalive-design.md`, `keepalive-fork-args-design.md`, `docs/rnd/keepalive-policy-design.md`, `keepalive-marked-spec.md`, `restart-idle-spec.md` | the two specs fold into the policy design, each with a status |
| PR review | `docs/review/review-memory-design.md`, `review-pr-start-design.md`, `docs/rnd/review-on-atrium-design.md`, `review-quality-design.md`, `review-tab-design.md`, `pulls-view-design.md`, `pulls-api.md`, `pr-review-workflow.md` | status lines, and `pr-review-workflow.md` named in the map as the current one |
| Decisions | `docs/decisions.md` (hub decisions 1 to 19), `docs/decisions-log.md` (clint's decisions) | both kept. `decisions.md` is renamed in the map to what it is, the hub and room decisions |

## Target doc map

What exists after, and who it is for.

| Doc | For | What it answers |
| --- | --- | --- |
| `README.md` | a human arriving at the repo | what atrium is, the hall metaphor, a ten-line start with `atrium run`, what it gives you in short, where to read next |
| `website/docs/*`, published at `dovholuknf.github.io/atrium` | a human using atrium | the manual and the place a new user reads: install, quick start, the hub and rooms, how it works, the board, the CLI, hooks, permissions |
| `FEATURES.md` | anyone | one entry per capability, current |
| `docs/README.md` | anyone inside `docs/` | the map below, where a new doc goes, and the status-line rule |
| `docs/how-atrium-works.md` | a contributor or an agent | the one architecture page: processes, ports, cards, hooks, the permission chain, messaging, state |
| `docs/fabric/hub-and-rooms.md` (new) | a contributor or an agent | the hub, rooms, link, join strings, overlays, cross-room addressing, as built |
| `docs/atrium-for-agents.md` | an agent wiring up a new runner | the hook contract, measured |
| `docs/<area>/*` | the area's owner | references for what is built, and design records with a status line |
| `docs/rnd/*` | @rnd and reviewers | designs, spikes and research, each with a status line |
| `docs/decisions-log.md`, `docs/fabric/hub-decisions.md` | anyone about to re-argue something | what was decided and why |
| `docs/test-plan.md`, `docs/changes/`, `changelog/` | the merger | unchanged process |
| `CHANGELOG.md` | anyone | frozen history, with a header that says where newer entries are |

Reading order for a new human: `README.md`, then the site: intro, install, quick start, the hub and rooms, how it
works. `FEATURES.md` for what else is there.

Reading order for a new agent: the root `AGENTS.md`, `docs/how-atrium-works.md`, `docs/fabric/hub-and-rooms.md`,
`docs/runtime/agent-messaging.md`, `docs/README.md` for where things go, then the `AGENTS.md` of the package being
changed.

### The status-line rule

Every design, spike, plan and research doc starts with one line after its title, in one of these forms, so a reader
knows in five seconds whether to trust the body:

- `Status: built. <where in the code>. <what differs from this design, if anything>.`
- `Status: partly built. <what is, what is not>.`
- `Status: proposed, not built.`
- `Status: superseded by <doc>. Kept as the record of <what>.`
- `Status: research, <date>. Its conclusion: <one line>.`

## Batches, smallest risk first

Each batch is one commit on `claude/m-doc-overhaul`, reported to the orchestrator by sha (the orchestrator merges
from the shared repository, so nothing is pushed). The doc-humanizer agent runs over any prose that was rewritten
rather than patched. A doc named from code moves only with those comment paths fixed in the same commit.

1. **Status lines.** One `Status:` line on every doc marked STATUS, each checked against the code first. No body
   edits.
2. **Small fixes.** Every FIX in the table: `atrium2 ledger` to `atrium ledger`, the reload restarter, the launchd
   line, `FEATURES.md`'s rooms section, the `CHANGELOG.md` and `docs/backlog/README.md` headers, the test plan's
   removed sections (marked removed, letters kept), `website/DECISIONS.md`, `control-mcp.md`'s port.
3. **Deletions** (Q2): the orchestrator snapshots, the one-off reviews and briefs, the mercurius synopsis.
4. **Archive and merges** (Q5): superseded designs to `docs/archive/` with a status line, the MERGE rows folded,
   comment paths in code fixed, every doc reference fixed with `git grep`.
5. **The architecture.** Write `docs/fabric/hub-and-rooms.md`, rewrite `docs/how-atrium-works.md`, rewrite
   `docs/README.md` as the map.
6. **For agents** (Q2). Write `docs/agents.md`, the curated bring-up. Move `docs/atrium-for-agents.md` to
   `docs/runtime/wiring-a-runner.md` and fix its paths. Then send the NOTES below to @dotfiles.
7. **The front door** (Q1). Rewrite `README.md`: a pitch that makes a stranger want to try it, "if you are an agent,
   read `docs/agents.md` first" at the top, then the working reference for `atrium run` and `atrium room`.
8. **The story** (Q5). Write `docs/story.md`, how atrium went from the v1 broker to the daemon to the hub and rooms,
   each step with what broke and its archived doc. File one hub backlog item per blog stub that tells part of it.
9. **The website, stale pages.** `install.md`, `quick-start.md`, `cli.md`, `control-mcp.md`, `rooms.md`, `modes.md`,
   and every CHECK page read against the code. `website/docs/hooks.md` holds the starter gate script that
   `website/scripts/test-gate-hook.js` runs, so its script block changes only with that test passing.
10. **The website, new pages.** How atrium works and the hub and rooms, carried from batch 5. The user guide's
    patterns, after which `docs/user-guide.md` is a pointer. The agent-focused page (Q3), "an agnostic harness for
    the agents you already run". The comparison (Q4), named and complimentary. The story, carried from batch 8.
11. **The comparison's tone** (Q4). Drop the `*-comparison*.md` rule from `.gitignore`, and reword anything in the
    rnd reads of other tools that runs a tool down.
12. **The site deploys itself.** `.github/workflows/docs.yml` runs on push to `main` when `website/**`, `docs/**`, the
    workflow or `scripts/build-docs.ps1` change, and on `workflow_dispatch`. The release trigger goes. The workflow
    stays logic-free: checkout, node, `configure-pages`, `scripts/build-docs.ps1`, upload, deploy. `website/DECISIONS.md`
    "Refreshed at release" and "Hosting" and the script's own header say the same. Every website batch is proved with
    `pwsh scripts/build-docs.ps1` before it is committed.

The site at `https://dovholuknf.github.io/atrium/` answers 404 today because nothing has deployed it. After batch 12
lands on `main`, clint sets the repository's Pages source to **GitHub Actions** (Settings, Pages, Build and
deployment, Source), once. Nothing in the repository can do that step.

## NOTES: changes the agent files need

The `AGENTS.md` files are not in this repository. They live in the dotagents repository at
`github/dovholuknf/atrium/`, and `CLAUDE.md` links to them. Nothing here edits them. What each one needs:

### Root `AGENTS.md`

- The opening, "`atrium daemon` is the thing that gets used", and the daemon diagram: replace with `atrium run` (hub)
  and `atrium room`, the four ports, and one line that `atrium daemon` still runs the old single process.
- Delete "Two older modes", "The wire protocol (Mode A)", "Agent-facing formatting affordances", "Resilience
  guarantees (Mode A)", "Behavior the LLM agent must NOT do" and the Mode A and Mode B rows of the subcommand table.
  All of it describes code that was removed.
- Repo layout: drop `internal/hub/`, `internal/agent/`, `internal/tui/`, `internal/server/`, `internal/state/`. Add
  `internal/link` (the hub and the link), `hubstore`, `ptyhost`, `gitsync`, `forge`, `prreview`, `edge`,
  `deployready`, `itemgate`, `requirements`, `resources`, `runnerprofile`, `runnersetup`, `roomstats`, `mcprule`,
  `cardurl`, `cardcolors`, `detach`, `shellpick`, `inputlag`, `testguard`, `webasset`. `internal/cli/` is no longer
  "daemon, stop, join, leave, hub, agent, serve, status, watch". `internal/api/web/index.html` is no longer the whole
  board: there is `css/`, `js/`, `m/`, `read.html`, `sw.js`.
- Documentation list: every path predates the 09-29 reorg (`docs/activity-design.md` is
  `docs/runtime/activity-design.md` and so on). Point at the reading order above instead of listing files.
  `docs/backlog.md` "keep this current" is wrong now that the backlog is on the hub.
- Test-plan letters "A=Mode A ... E=Mode B": those sections are gone.
- Out of scope: "Cross-machine aggregation" and "Persistence in the hub" are both built now (rooms, the hub store).
  "Single-machine localhost only" needs the overlay and room link wording.
- Conventions: "The hub is reader-only of agent identity ... no auth" is the Mode A hub. The rooms link is mTLS with a
  hub-signed certificate.

### `internal/daemon/AGENTS.md`

- "Two listeners: `:7778` for the human, `:7777` for agents": as `atrium room` the human board is :7781.
- `docs/activity-design.md` is `docs/runtime/activity-design.md`.
- "A kill is not a stop": true without `pty_host`. With it, the terminals live in the pty host.

### `internal/agent/AGENTS.md`

- The package does not exist. Delete the file.

### `internal/api/web/AGENTS.md`

- "`index.html` is the whole board ... in one file": the board is `index.html` plus `css/`, `js/`, `m/` and `sw.js`.
- `docs/backlog.md` as the record of the CodeMirror refusal: check it is still there.

### `internal/store`, `internal/api`, `internal/claudeconf`, `internal/safepath`

Checked at batch 2. Each names a doc by its path before the 09-29 reorg:

- `internal/api/AGENTS.md` and `internal/safepath/AGENTS.md`: `docs/file-transfer-design.md` is
  `docs/runtime/file-transfer-design.md`.
- `internal/store/AGENTS.md`: `docs/activity-design.md` is `docs/runtime/activity-design.md`, and
  `docs/intake-design.md` is `docs/runtime/intake-design.md`.
- `internal/claudeconf/AGENTS.md`: nothing stale found.

## Gaps found, not fixed here

- Nothing folds `changelog/<dept>/` into `CHANGELOG.md`, so the frozen file stops at 09-29 and nothing reads the
  fragments in order. A fold at release time is a script change.
- 76 files wait in `docs/changes/` for `fold-changes.ps1`.
- The code comments in the dead-link table above.
- `docs/backlog.md` links 16 gitignored files.
- The service installers still default to the old single process: `packaging/atrium.service`,
  `packaging/postinstall.sh` and `scripts/atrium-service.sh` run `atrium daemon` unless told `room`
  (`ATRIUM_SERVICE_VERB=room`, `atrium-service.ps1 -Verb room`). `docs/release/packaging.md` says so correctly.
- `internal/cli/backlog_import.go` says the files in `docs/backlog/` are the source of truth and the hub's backlog a
  mirror, "until the orchestrator says otherwise". The brief says the live backlog is the hub's. One of the two
  needs updating.

## Interview

Answers recorded here as they come.

- **Scope, from clint by way of the orchestrator, 2026-10-07.** The website is in scope as the published docs and
  the place a new user reads. It deploys on push to `main`. Batches 9, 10 and 12.
- **Q1, README.md's reader.** (b): the front door AND a working reference for clint and his agents. The pitch comes
  first and has to make a stranger want to try it ("people excited to use it"), then the CLI table, config and
  endpoint notes, rewritten for `atrium run` and `atrium room`.
- **Q2, dead operational docs.** (a), delete: `docs/orchestrator/dispatch-queue.md`, `status.md`,
  `overnight-brief.md`, `docs/review/cr2-*`, `cr48-*`, `item16-notes.md`, `peer-review-brief.md`, the mercurius
  synopsis. Git keeps them. AND a new curated bring-up doc for agents, `docs/agents.md`, with "if you are an agent,
  read this first" at the top of `README.md` and `docs/README.md` pointing at it. It is the in-repo half of what the
  root `AGENTS.md` should be: what atrium is now, the processes and ports, where things are, how to build and test,
  the house rules, and which docs to trust. The runner-wiring brief that holds a similar name today,
  `docs/atrium-for-agents.md`, becomes `docs/runtime/wiring-a-runner.md` so the two cannot be confused.
  Also asked for: an agent-focused marketing doc. Its reader is settled in Q3.
- **Q3, the agent-focused marketing doc.** Its reader is (a), a person who runs coding agents, and it lives on the
  site. An agent reading it must come away knowing atrium is an agnostic agent harness: claude, codex, gemini,
  opencode, ollama or a shell are rows in a runner table, not built-in assumptions. It links the competitive
  differences page, settled in Q4.
- **Q4, the comparison.** (a), open and named. A site page, "how atrium differs", compares atrium with the tools
  clint has named before (the ones in `docs/rnd/competitors.md`, `competitor-features.md`, `bb.md`, `charon.md`,
  `orca.md`, `factory-landscape.md`, `gateway-fit.md`, `spike-mcp-gateway.md`, `langchain-openwiki-spike.md`,
  `state-of-the-art.md`). It is complimentary to every one of them and never negative: each is a good tool and a real
  alternative, and the page says what each is best at and where atrium takes a different path. The `*-comparison*.md`
  rule in `.gitignore` and its comment go. The rnd reads stay in the repo, and since they are public they get the
  same tone pass: anything that runs another tool down is reworded to what it does well and how atrium differs.
- **Q5, superseded designs.** Kept, in `docs/archive/`, each with a `Status: superseded by <doc>` line. That covers
  the eight hub-history docs and every other design a later doc replaced. Beside them, a maintained story of how
  atrium changed, `docs/story.md`: it started as X, went to Y, then Z, each step with what broke and the archived
  doc that holds the detail. The site's `story.md` carries the published version. The blog stubs in `docs/blog/`
  that tell parts of that story become hub backlog items to write them, one item per post, filed with
  `atrium backlog file`. Moving a doc that Go comments name means updating those comment paths, which is the only
  edit this overhaul makes to a `.go` file, comments only. clint does not mind either way, so the paths are fixed
  in the same batch as the move, including the ones already dead.
- **Q6, the AGENTS.md files in dotagents.** Not edited here. Once `docs/agents.md` exists, the NOTES section goes to
  the @dotfiles card, which owns that repository, and it decides what to do with it.

Interview complete, 2026-10-07. The rest of the open choices (how the test plan marks its removed sections, where
the user guide's patterns land) follow from these answers and do not change what a reader is told, so they were not
asked.
