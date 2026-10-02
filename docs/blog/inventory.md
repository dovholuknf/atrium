# atrium feature inventory

Status: written by @rnd for the blog expedition, 2026-10-02. It covers what atrium ships, what is designed, what is
wanted, and what was abandoned, with one line each and the doc each lives in. The posts in `docs/blog/` draw on it.

Method:
- Two read-only digs of `claude/main` and `claude/rnd`, run at 2026-10-02 late, cover README, FEATURES, CHANGELOG,
  `changelog/*/`, `docs/**`, `website/docs` and the git log.
- "Built" was checked against git. A backlog file's `Status:` line can be stale, and where the two disagree, git
  wins.
- Dates are when a thing first landed on `claude/main`. "claude/main only" in FEATURES.md means it has not yet
  reached the signed `origin/main`.

Security findings are summarised at a high level only, and the security follow-ups are not listed item by item.

## 1. Shipped

Commit volume is part of the story. In June there was 1 commit, and in early September about 70. Then the daily
count was:
- 257 on 09-28;
- 755 on 09-29;
- 542 on 09-30;
- 310 on 10-01;
- 208 on 10-02.

The jump came once the director agents started, on 09-28 at 22:00.

### Daemon and halt

- One `atrium` binary — `atrium run` serves the hub and starts this machine's room detached — `docs/fabric/one-atrium-plan.md`, FEATURES "Rooms and the hub" — atrium2 shim retired 2026-09-26 (`ce1a458f`)
- Durable SQLite state — every card has an append-only event log, pure-Go `modernc.org/sqlite` — `docs/architecture-v2.md` — 2026-09-01 (`fdc5c8e6`)
- Storage failure halts — agent listener closes and stays closed, board stays up to say what broke — `docs/how-atrium-works.md`, README — 2026-09-01 (`fdc5c8e6`)
- Constraint errors never halt — a SQLite constraint refusal is returned to the caller, only real storage failure halts — `changelog/runtime/2026-09-30-r-test-isolation.md` — 2026-09-30 (`8da27125`)
- Health event — room pushes `health` (halted, cause, settling) instead of being polled — `changelog/runtime/2026-09-29-r-017.md` — 2026-09-29
- `atrium stop` graceful wind-down — event streams close, runners get ten seconds — README — 2026-09-01 (`b054a573`)
- `restart_atrium` from inside a session — parks other agents, detached restarter, comes back as the same room, installs staged binary — `docs/runtime/reload-design.md` — 2026-09-04 (`3d89e69b`), same-room 2026-09-22
- A restart reopens what was open — resumes conversations, alerts quiet until every card arrives — `docs/runtime/reload-design.md` — 2026-09-08 (`45dffb30`)
- Asked-exit is sticky — only a person's exit (board, `atrium_exit`, cull, terminate, typed `/exit`) keeps a card down across restart; `task.exit_asked_at` migration 0076 — `changelog/runtime/2026-09-30-r-review-exit-asked.md` — 2026-09-30
- Database-changed warning — warns when it opens a different DB than last time — README — 2026-09-04
- `atrium db compact` / `atrium ledger` — offline VACUUM INTO copy, read the work ledger straight from the DB — README — 2026-09-24 (`1c263460`)
- Event sink and rolling window — `event_sink` db/file/both, per-card roll-off — FEATURES — 2026-09-18
- Hub snapshots and restore — every 10 minutes, tiered over a month, restore moves current store aside — `CHANGELOG.md`, `atrium backups` — 2026-09-17 (`4dc0e03`)
- Preview board — `atrium preview --from live`, second daemon on a copy of cards, takes no hooks — `docs/ui/preview-design.md` — 2026-09-06 (`2985923e`)
- Test guard — test binaries clear every `ATRIUM_*`, sealed home, agents never really started in tests — `internal/testguard`, `changelog/runtime/2026-09-30-r-test-guard.md` — 2026-09-30
- Room stats — `room-stats` every 10s (token burn, process, disk, CPU, memory) — `changelog/runtime/2026-09-29-r-010.md`, `r-031.md` — 2026-09-29/30
- Gzip + ETag board assets — first load 2.7MB to 0.86MB, reload is a 304 — `internal/webasset`, `changelog/runtime/2026-09-30-r-board-gzip.md` — 2026-09-30
- Pty host (off by default) — `atrium ptyhost` owns runners so a restarted daemon reattaches with ring replay — `docs/terminal/ptyhost-protocol.md`, `changelog/terminal/2026-09-30-f-011*.md` — 2026-09-30 (`cb6fb087`), setting `pty_host` off

### Permissions and auto mode

- Permission gate — PreToolUse hook blocks every tool call until approved/blocked, block reason goes back to the agent, fails open if atrium is down — README, `docs/runtime/hooks.md` — 2026-09-01 (`fdc5c8e6`)
- Gate built into the binary — `atrium hook --event permission` replaces the dotfiles pwsh script, registered with a 24h timeout — `changelog/runtime/2026-09-29-r-023.md`, `changelog/fabric/2026-09-30-f-006-replace.md` — 2026-09-29/30 (`b2f52fc4`)
- Real diffs in a pending edit — dimmed context, changed words highlighted — README — 2026-09-01 (`c51ae309`)
- Standing rules — always/never as prefix, glob, or folder; most specific wins; a tie now goes to block — README, `changelog/runtime/2026-09-30-r-034.md` — 2026-09-01, folders 09-02, tie rule 09-30
- MCP tool rules — rules can name `mcp__server__tool`, a glob or a whole server — `docs/runtime/mcp-rules-design.md` — 2026-09-30
- Import Claude Code's allow/deny lists — preview first, reports unmappable entries (134 rules on first run) — README, `docs/architecture-v2.md` — 2026-09-01
- Auto mode — per card, board-wide, or for the next hour; never overrides a never rule or a shelved card; `what did it do?` review — `docs/runtime/auto-mode.md` — 2026-09-02 (`a85e2a3b`), hour 09-03 (`e11e950d`)
- Hub-held board-wide auto — approves from the request's own event, recorded as `global-auto` not `you` — `changelog/fabric/2026-09-29-f-008.md`, `r-020.md` — 2026-09-19, attribution fix 09-29
- Shelving is a standing no — and answers what it had pending — `docs/how-atrium-works.md` — 2026-09-01
- Machine-wide gating — `ATRIUM_PERM_GATE=on`; runners tab writes missing hooks, preserving siblings/symlinks; codex is a second target — `docs/runtime/hooks.md` — 2026-09-02, codex 09-03 (`5469fa9d`)
- Dialog guard — atrium refuses to type into a permission dialog it did not raise — CHANGELOG — 2026-09-16 (`704b2e9a`)
- Paste-marker strip — typed text can no longer end its own bracketed paste (which could send Shift+Tab and cycle permission mode) — `changelog/runtime/2026-10-01-r-paste-strip.md` — 2026-10-01
- Decision log — every decision records who asked and who answered (you, a rule, auto) — `docs/runtime/auto-mode.md` — 2026-09-02
- Cross-origin and Host protection — browser-reachable listeners refuse cross-site writes and foreign websocket upgrades, loopback answers only a loopback Host — `changelog/runtime/2026-09-30-r-security-edge.md` — 2026-09-30
- Local operator check — loopback-only hub routes now also refuse forwarded/proxied requests (a zrok share terminates on 127.0.0.1) — `changelog/runtime/2026-09-30-r-local-operator.md` — 2026-09-30
- Hosts setting — `GET/PUT /_hub/hosts`, the DNS-rebinding allowlist, public-suffix wildcards ignored — `changelog/runtime/2026-09-30-r-hosts-setting.md` — 2026-09-30

### Cards (sessions) and the board

- One web board, every session a card — plain page over JSON+SSE (not the planned React SPA) — README, `docs/architecture-v2.md` — 2026-09-01
- Attention columns — needs permission, ready, running, finished, shelved, plus inbox; empty columns compress — README — 2026-09-01, compress 09-03
- Question vs finished turn — Notification hook distinguishes an ask from running out of work — `docs/runtime/hooks.md` — 2026-09-04 (`304fbe08`)
- Live activity badge — thinking / named tool / N subagents, never stored — `docs/runtime/activity-design.md` — 2026-09-02
- Liveness by syscall — pid check, no turn or token — README — 2026-09-01
- Context burn on a card — statusline posts to `/telemetry`, card draws context and account limits — `docs/runtime/statusline-telemetry.md` — 2026-09-07
- Tags, grouping, sort pill, custom groups, collapsing groups — FEATURES "The board" — 2026-09-03 to 09-22
- `ctrl-shift-k` switcher — MRU filter across title/dir/tags, works in pop-outs — `docs/ui/switcher-design.md` — 2026-09-07 (`3e6f425b`)
- Pins with stored rank — FEATURES — 2026-09-03
- Rename wins everywhere; inferred `display_repo` with override — FEATURES — 2026-09-14/15
- Archive instead of delete; resume falls back to fresh — FEATURES — 2026-09-03
- Priority and per-card icon on notifications — FEATURES — 2026-09-04/05
- Seen tracking and Open Questions — teal unread dot, `? N` chip, stored — `docs/runtime/seen-design.md` — 2026-09-23
- Card URLs made of names — `/alias/<alias>`, `/room/<room>/<name>`, `/m/...`; ambiguity chooser — `changelog/ui/2026-09-30-u-card-urls.md`, `docs/rnd/card-urls-design.md` — 2026-09-30
- Handle-addressed HTTP — `/v1/tasks/<alias|name@room|room~id>`; `atrium task|exit|new-context|launch --onto` — `changelog/runtime/2026-09-30-r-handle-http-*.md` — 2026-09-30
- Twenty skins, terminal theme import, per-scope skin — FEATURES "Settings and skins" — 2026-09-05 to 09-19
- Event-driven board — board keeps one copy updated from SSE, idle board 350 req/min to ~5 — `changelog/ui/2026-09-29-u-009.md`, `u-009b.md` — 2026-09-29
- Self-reload on new build — build id hashes `web/` — `docs/runtime/reload-design.md` — 2026-09-04 (`a874f918`)
- Phone page `/m` — "Needs you" list, card thread with replies and prompts, composer, approve/deny, file viewer, per-turn diffs, home-screen install — `changelog/ui/2026-09-29-u-024.md`, `2026-09-30-u-m-*.md`, `2026-10-01-u-m-changes.md` — 2026-09-29 onward
- Phone redirect — coarse pointer under 600px lands on `/m` — `changelog/ui/2026-09-30-u-m-redirect*.md` — 2026-09-30
- Replies API — `GET /v1/tasks/{id}/replies` with prompts, paging backwards — `changelog/runtime/2026-09-29-r-024.md`, `2026-09-30-r-replies-*.md` — 2026-09-29/30
- Changes API — `GET /v1/tasks/{id}/changes?against=head|base`, `?turn=` for one reply's edits — README, `changelog/runtime/2026-10-01-r-changes.md` — 2026-10-01
- Usage tab — tokens by kind/cause/card/department/director, limits with flameout projection, burn chart, cache reads hidden by default — `changelog/ui/2026-09-29-u-011..u-014.md`, `2026-10-01-burn-chart.md`, `docs/rnd/usage-tab-design.md` — 2026-09-28 onward
- `atrium usage backfill` — fills usage rows from transcripts — `changelog/runtime/2026-09-29-r-015-backfill.md` — 2026-09-29
- Rooms dashboard tiles — agents/tokens/machine bands per room — `changelog/ui/2026-09-29-u-010.md`, `2026-09-30-u-034.md` — 2026-09-29
- Persistent growlers — hub `growl` table for waiting permission/question/halt/deploy-hold, reminders 1m..120m, approve/reply/snooze from it — `changelog/runtime/2026-09-30-r-growler-*.md`, `changelog/ui/2026-09-30-u-growler.md`, `docs/rnd/persistent-growler-design.md` — 2026-09-30; made a per-browser setting OFF by default 2026-10-02 (`a8134b47`)
- Blocker state — a card that never got going is red, rings like a permission, pinned in the bell — commit `10a19a37` — 2026-10-02
- Desktop notifications with approve/block — one alert per event, per-card sounds — README — 2026-09-03, one-alert 09-16
- Bell / notification log — last 200, repeats folded — FEATURES — 2026-09-14
- Hub notify command — `PUT /_hub/notify`, runs your command (env + stdin JSON, never argv) when a card needs you and no tab is visible — `changelog/fabric/2026-09-29-f-017.md`, `f-023..f-025` — 2026-09-29
- History tab — every card ever, JSON/CSV export — README — 2026-09-03 (`db2cf2c2`)
- Audit tab — hub and room events, every mutating control call with claimed caller — `docs/fabric/audit-design.md`, `changelog/fabric/2026-09-30-f-020.md` — 2026-09-19, control calls 09-30
- Hub documents — `/_hub/docs` versioned uploads at `/d/<slug>`, `atrium_publish` with secret-shape refusal — `changelog/fabric/2026-09-30-hub-documents-d1.md`, `docs/rnd/hub-documents-design.md` — 2026-09-30
- Config export/import — dry run by default — FEATURES — 2026-09-06
- Files in and out — paste/drop into `.atrium/incoming`, browse + zip out, one containment check, in-browser editor, paths in output are links — `docs/runtime/file-transfer-design.md` — 2026-09-03 to 09-07

### Supervision and terminals

- Pty supervision with browser attach — atrium owns each runner's pty, websocket attach — `docs/terminal/supervision-design.md` — 2026-09-01 (`b054a573`)
- Watch-and-gate vs supervised modes — `atrium join` / `leave` for sessions in your own terminal — `website/docs/modes.md` — 2026-09-01
- Pop-out terminal windows — reconnect across hub restart, per-card bell — FEATURES — 2026-09-04, reconnect 09-19
- Scrollback through a screen model — grid replay, survives resizes/restarts, `replay_flat` escape hatch — CHANGELOG ~line 2440 — 2026-09-14 (`53b728cb`)
- Repaint loss report — `scrollback/text?repair=report` measures rows conhost overwrote — `changelog/terminal/2026-09-29-74b.md` — 2026-09-29
- Width follows widest viewer, height shortest, `terminal_min_cols` 120 floor for claude — `docs/terminal/terminal-resize-decoupling-design.md` — 2026-09-22
- Half-second height hold — a passing height flip cannot eat lines — CHANGELOG ~line 275 — 2026-09-28 (`ee9d0b54`)
- Open at the watched size — no banner repeat on a fresh card — `changelog/terminal/2026-09-29-t-002.md` — 2026-09-29
- Typing gate — every automated write waits for an empty line and 2s quiet; tracks line text, ignores focus/mouse reports — FEATURES, `docs/backlog/terminal/33.md` — 2026-09-20, text model 09-28
- In-done acks for pastes — room answers `in-done` per paste frame — `changelog/runtime/2026-09-30-r-paste-done.md` — 2026-09-30
- Shell beside a wedged agent — FEATURES — 2026-09-05
- Restart onto the same card — from the terminal cog — FEATURES — 2026-09-18
- New context cycle — capture a `HANDOFF.<alias>.md`, `/clear`, wake; manual, automatic at `auto_new_context_k`, and enforced `context_ceiling_k` for tagged cards — `docs/runtime/auto-new-context-design.md`, `changelog/runtime/2026-09-29-91.md`, `2026-09-30-r-029.md`, `2026-09-30-r-director-ceiling.md` — 2026-09-28 (`3d0edb0c`) onward
- Idle parking — a card idle 2h is parked (no process, resume id kept), woken by key/say/report — `changelog/runtime/2026-09-29-r-007-*.md` — 2026-09-29
- Cache keep-alive — refreshes idle Claude cards' prompt caches with a guarded forked resume, stops at break-even — `docs/runtime/cache-keepalive-design.md`, `changelog/ui/2026-09-30-u-032.md` — 2026-09-27 (`81492df1`)
- `--autocompact` at the atrium limit plus 10% — commit `16488230` — 2026-10-02
- Live model switch — `POST /v1/tasks/{id}/model`, `atrium_model`, survives restart — README, `changelog/runtime/2026-09-30-r-card-model.md` — 2026-09-30
- Held-message escalation — a `when: done` message older than 15m rides the next tool call; turns over 45m flagged to the launcher — `changelog/runtime/2026-09-30-r-escalation-*.md` — 2026-09-30
- Above-normal priority on Windows — hub, room, runners and conhost — `changelog/terminal/2026-09-29-t-004.md` — 2026-09-22/29
- Phone terminal — no-resize watching, pinch/pan, key bar, message box for swipe/dictation — `changelog/terminal/2026-09-29-t-003b.md`, `changelog/ui/2026-09-29-u-016..u-026.md` — 2026-09-29
- Diagnostics — terminal trace from the cog, `atrium replay`, `ATRIUM_TAP_DIR`, one checkbox for input-lag timing — FEATURES "Diagnostics" — 2026-09-14 to 09-22
- Erase-display keeps the page — a cleared terminal pushes visible rows into history — commit `d6e70c94` — 2026-10-02

### Runners (claude, codex, opencode, others)

- Runners are configuration — command, args, dir, env, resume args, exit keys — `docs/runtime/other-runners.md`, `docs/atrium-for-agents.md` — 2026-09-01, non-claude 09-07 (`6634742b`)
- Codex — hooks as a second target, smoke per runner, full `codex-package` install — `docs/runtime/other-runners.md`, `changelog/fabric/2026-09-29-f-007.md` — 2026-09-03 onward
- Gemini setup checks — folder trust and sign-in, a fix button — `docs/runtime/runner-setup-design.md` — 2026-09-23
- OpenCode — atrium plugin for opencode, counts as a runner when a hook finds its pid, edited approvals apply whole or refuse — commits `4ea3dc66`, `6bce194f` — 2026-10-01
- Ollama, shell, aider rows — `docs/runtime/other-runners.md` — 2026-09-07
- Room-scoped runners — launch dialog offers only runners the room has, per-room `bin_path` — `docs/fabric/runner-scoping-design.md` — 2026-09-21
- Lean workers — atrium-control only by default, keep named agents/skills via a session plugin (`lean_agents`, `lean_skills`), narrower gateway — `docs/runtime/lean-workers-design.md`, `changelog/runtime/2026-09-29-r-005.md`, `r-035.md` — 2026-09-28 (`6d08a63f`)
- `--strict-mcp-config` on launched claude — no MCP prompt stalls — FEATURES — 2026-09-12
- Pick a conversation to resume, refuse a second runner on one — FEATURES — 2026-09-04; resume-holder checks 09-30
- Fixtures — sessions atrium keeps running, on/off — FEATURES — 2026-09-03, toggle 09-18
- Throwaway sessions with `keep this work` — FEATURES — 2026-09-14
- In-process runner update check — reads installed `package.json`, asks registry once — FEATURES — 2026-09-15
- Native-claude pid discovery — macOS via sysctl, Linux via argv[0] because the process is named for its version — `changelog/fabric/2026-09-29-f-016.md`, `2026-09-30-f-018.md` — 2026-09-29/30

### Rooms, hub, federation, link

- Hub/room split — hub serves board and holds no card state, room owns DB/ptys/agents, room dials hub over mTLS after a one-string join — `docs/fabric/hub-room-plan.md` — 2026-09-17 (`7dde248e`)
- One board over every room — merged lists, room picker with state dots — `docs/fabric/hub-room-requirements.md` — 2026-09-17, picker 09-19
- Hub names its rooms — `atrium rooms add|ls|token|mark|rm|log`, name from hub-signed cert — `docs/decisions.md` 7 and 18 — 2026-09-17 (`d49d69d`, `153078c`)
- Offline-room cache — cards from an unanswering room drawn from last report, nothing opens — `docs/decisions.md` 12-16 — 2026-09-17 (`a59d4d2`)
- Delete a room: mark, clear, confirm, stop, remove — `docs/decisions.md` 9 — 2026-09-17
- Transports: direct mTLS, private zrok, OpenZiti — `docs/fabric/ziti-zrok-flow-design.md` — 2026-09-17 (`97d41101`)
- Overlay rooms proven by hub-signed cert inside the overlay; legacy rooms marked unproven, `atrium rooms legacy refuse` — `changelog/fabric/2026-09-30-f-022.md`, `f-026.md`, `docs/rnd/overlay-room-identity.md` — 2026-09-30
- Hub offers builds, room opts in with `--accept-upgrades` — FEATURES — 2026-09-17
- `--isolated` second room on one machine — FEATURES — 2026-09-18
- Cross-room say/task/exit/launch — relay ops through the hub, `name@room`, `room~id` — commits `58579f40`, `df42c25c`, `bf3630f0` — 2026-09-28 onward
- `atrium:everywhere` cards — reachable by bare name from every room — `changelog/fabric/2026-10-01-49.md` — 2026-10-01
- Hub events instead of polls — rooms attach/leave/mark pushed — `changelog/fabric/2026-09-29-f-008.md` — 2026-09-29
- `atrium dispatch to <room>` — queue work a room claims on check-in — `docs/fabric/remote-launch.md` — 2026-09-07
- Per-room launch caps — `PUT /_hub/launch-caps` — `changelog/fabric/2026-09-30-room-launch-cap.md` — 2026-09-30
- Hub git sync — hub mirrors `claude/main` into a bare repo, serves it upload-pack only over a `git` link kind, collects rooms' `claude/*` into `refs/remotes/<room>/`, never pushes — `changelog/fabric/2026-09-30-f-019b.md`, `docs/rnd/git-sync-design.md`, `docs/decisions.md` 19 — 2026-09-30, live on sg3 and m1mini
- Hub git store / repos view — `git.store`, `rooms git init`, board repos view with clone URLs — commits `06ea9026`, `d3004c30` — 2026-10-02
- Unscoped launch routing — goes to the room on the caller's machine that has the directory — commit `0ce86ade` — 2026-10-02
- Room provisioning over ssh — `scripts/provision-room.ps1` (install, join, hooks, gate, statusline, Defender, autostart, smoke per runner, `-Remove`) — `changelog/fabric/*`, `docs/rnd/room-autostart-design.md` — 2026-09-28 (`c5054059`) onward
- Room requirements — `atrium.requirements.yaml`, `atrium requirements`, `POST /v1/preflight`, `scripts/room-check.ps1` — `changelog/fabric/2026-09-29-f-013.md`, `f-014.md`, `changelog/runtime/2026-09-29-r-018.md` — 2026-09-29
- `room-defender.ps1` — Defender exclusions read as the agent account, admin line printed not scripted — `docs/user-guide.md` pattern 13 — 2026-09-30

### Overlays (OpenZiti, zrok)

- Board served on a zrok share or OpenZiti service natively — SDK listener, nothing proxied, atrium holds no identity — `docs/fabric/overlays.md` — 2026-09-03 (`b7163560`), hub share 09-18, `--board-transport ziti` 09-19
- Lend one session — allowlist handler, read-only enforced on the socket, readable `/room/<room>/<handle>` address — `docs/fabric/overlays.md`, `changelog/runtime/2026-09-30-r-card-urls-r4.md` — 2026-09-04 (`5af35c25`)
- Reserved zrok names, atrium's own zrok environment, shares survive restart — FEATURES — 2026-09-03/06
- Revoke a public share from the board — FEATURES — 2026-09-11
- Published-board login — basic and/or OIDC; public zrok board share refused without a login — `docs/fabric/overlays.md`, decisions-log "Board-share auth" — OIDC 09-06, password 09-11, enforced 09-19/20
- Ziti JWT enrol at join — commit `fa887821` — 2026-09-20
- Three trial "expose the board" surfaces side by side — `docs/fabric/ziti-zrok-flow-design.md` — 2026-09-20 (`e27ca9f0`)
- ziti-sdk-c build profile for Windows rooms — `room-toolchain.ps1 -Profile c` — commit `8b82e12a` — 2026-10-02

### Intake (PR review, etc.)

- Inbox — cards with no runner, `start` prefills launch — `docs/runtime/intake-design.md` — 2026-09-03
- Sources — a command on a timer posting items, holds no credential, off after 3 failures — `docs/runtime/intake-design.md`, `scripts/sources/` — 2026-09-03 (`9028a32f`)
- Recognisers / `atrium open <url>` — a URL fills the launch dialog — `docs/runtime/scm-design.md` — 2026-09-07
- Providers — name + root + layout adopts every checkout, `git worktree add` with no shell — `docs/runtime/providers-design.md` — 2026-09-17, projects dropped 09-22
- Named actions — stored prompts on any card (`write it up and finish`) — `docs/user-guide.md` pattern 9 — 2026-09-03
- Pulls tab and PR review runner — `POST /v1/prs`, run folder per PR head, `claude -p` primed once then reviewer/verifier/critic forks, renderer writes `findings/` and `walk.txt`, budget checks, never posts to GitHub — `changelog/runtime/2026-10-01-r-pr-*.md`, `changelog/ui/2026-10-01-pulls-p2.md`, `docs/rnd/pulls-view-design.md`, `docs/rnd/pulls-api.md` — 2026-10-01
- Walk drawer — walk a review's findings beside the terminal, mark posted/skipped, edits refused on a changed file — `changelog/ui/2026-09-29-u-005.md` — 2026-09-29
- Pulls through the hub — merged `/v1/prs` in the ALL view — `changelog/fabric/2026-10-01-f-pulls-hub.md` — 2026-10-01

### Git and landing (deploy-ready, verdicts)

- Deploy-ready — `internal/deployready` reads `Atrium-Verdict: hub-ok|room-ok|hold <base>..<tip>` trailers on review commits, matched by `git patch-id --stable`; header pill and one-click deploy from the hub's machine — `changelog/runtime/2026-10-01-r-deploy-ready.md`, `changelog/ui/2026-10-01-u-deploy-ready.md` — 2026-10-01
- Deploy-ready bounded at 5s — after it hung the live hub — `changelog/runtime/2026-10-01-r-deploy-ready-bound.md` — 2026-10-01
- Deploy guards — deploy binary only from clean `claude/main`, refuse `(modified)`, pass only if `claude-sg4` stays attached 30s — `changelog/merge/2026-09-29-deploy-guards.md` — 2026-09-29
- Room deploy hold — `atrium_deploy request|start|wait|cancel`, other cards refused with "end your turn and wait", one wake line after — `changelog/runtime/2026-09-30-r-deploy-hold.md` — 2026-09-30
- Hub restart gate — hub-only deploy waits for an idle board, countdown toast, cover while restarting — `docs/fabric/hub-restart-gate.md` — 2026-09-24 (`14c22637`)
- Deploys wait for a new context in flight — `GET /_hub/new-contexts` — `changelog/runtime/2026-10-01-r-clear-vs-restart.md` — 2026-10-01
- Merged cull — git `post-merge` runs `atrium merged`, workers culled after 30m grace; `atrium_cull` with a `tip` merge proof — `changelog/runtime/2026-09-29-r-019.md`, `changelog/fabric/2026-09-29-f-010.md` — 2026-09-28 (`2ac78ac0`)
- Item dependencies — `atrium_deps`, gates met only when hub reads the item's changelog file on `claude/main`; `atrium_launch` refuses blocked items — `changelog/runtime/2026-09-30-r-new-item-dependencies.md` — 2026-09-30
- Per-department changelog files — `changelog/<dept>/<date>-<item>.md`, CHANGELOG.md frozen — `changelog/README.md` — 2026-09-29 (`972af49f`)
- One-file-per-item backlog — `docs/backlog/<dept>/<id>.md`, `scripts/backlog-index.ps1` — `changelog/rnd/2026-09-29-docs-layout.md` — 2026-09-29
- Sharded headless board suite, run remotely on sg3 — 166 units, 22 min to ~3 min — `changelog/fabric/2026-10-01-board-suite-*.md` — 2026-10-01
- Landing to `origin/main` — only clint's signed, re-signed commits — `docs/orchestrator/cold-start.md`, decisions-log 09-23/24 — ongoing

### Org of directors and workers

- Control MCP on the hub — `/_hub/mcp`: status, peers, say, task, exit, cull, launch, report, alias, model, deploy, deps, git sync, publish, restart — FEATURES, `docs/runtime/agent-messaging.md` — 2026-09-18 (`d192062`)
- Worker tool set narrowed — workers see 6 control tools, directors see all — `changelog/fabric/2026-09-30-f-021.md` — 2026-09-30
- Peer bus — `atrium peers|tell|ask --peer|answer`, typed when free, retried 2s to 4h, 8000 chars and 20/min caps — `docs/runtime/agent-messaging.md` — 2026-09-06
- `atrium finish` / `atrium ask` — agent declares done or stuck — `docs/user-guide.md` patterns 8, 12 — 2026-09-03/07
- Launch cap — 10 concurrent agent-launched sessions, counts only `atrium:subagent` — FEATURES — 2026-09-19, per room 09-30
- `--report-to`, `atrium_report`, owed reports only for launcher prompts — `changelog/runtime/2026-09-29-r-014.md`, CHANGELOG — 2026-09-28/29
- Silent-stop backoff — 1m..24h, never a forced turn; directors not nagged while workers are out — decisions-log 09-23, `changelog/runtime/2026-09-29-r-007.md` — 2026-09-23/29
- Held notices for the orchestrator — notices recorded not typed, `fyi` vs `needs` kinds, `atrium_task notices:true` — `changelog/runtime/2026-09-30-r-hold-notices.md`, `r-fyi-kind.md` — 2026-09-30 (`86d79282`)
- Exit on done report — spawned worker that reports done exits 5s later — `changelog/runtime/2026-10-01-r-exit-on-report.md` — 2026-10-01
- Aliases — `@ui`, `@runtime`, `@fabric`, `@rnd`, `@review`, `@merge` handles, unique among live cards — commits `92fc9d04`..`ccc14268` — 2026-09-28
- Child fold / no ready alert while children run — `changelog/ui/2026-10-01-child-fold.md`, `no-ready-children.md` — 2026-10-01
- Read-only subagents allowed inside doers — Explore/Plan exempt from the Task guard — decisions-log 2026-09-21 — 2026-09-21

### Packaging, release, website

- deb, rpm, macOS pkg, Windows MSI, per-user no-admin paths; `make release` five platforms — `docs/release/packaging.md` — release 2026-09-06, packages 2026-09-19 (`7560db7`)
- Logon task, not a service — `scripts/atrium-autostart.ps1` — `docs/user-guide.md` pattern 0 — 2026-09-04
- Room autostart on Windows/macOS/Linux by default at provision — `changelog/fabric/2026-09-30-46c.md`, `2026-10-01-provision-mac-start.md` — 2026-09-30
- Docs site — Docusaurus, GitHub Pages under `/atrium/`, drawn HTML mockups not screenshots, refreshed only at release (`0.0.1`) — `website/DECISIONS.md`, `website/docs/*` — website built late Sept
- Live deploy scripts — `scripts/live/build-deploy.ps1`, `deploy-hub-only.ps1`, `deploy-batch.ps1`, one revert binary kept — `changelog/merge/2026-09-29-deploy-guards.md` — 2026-09-25 onward

### Other

- Research shelf in `docs/rnd/` — competitor matrix of 18 tools, source reads of Charon, bb, Orca, gateway fit, factory landscape, a $2,500 room-machine spec — `docs/rnd/competitor-features.md`, `charon.md`, `bb.md`, `orca.md`, `gateway-fit.md`, `room-machine-2500-design.md` — 2026-09-03 to 10-01

## 2. Designed, not built (or only partly built)

The pause has held designs only since 2026-10-01, so nearly every design from 10-02 is "designed, held". A few lines
here name a design that was later built, and the line says so: the design doc is still where to read it.

### Rooms, hub, federation, hub forge

- Hub as a forge, rev 2 — the hub keeps its own `main` and takes finished work by push with plain git rules; work in progress stays on its room and is passed through live, with no copy — `docs/rnd/hub-forge-design.md` — partly built: stage 1's hub git store (decd2a7b) and the board's repos tab (333533e7) landed; `f-new-hub-receive` and `r-new-hub-remote` held; stages 2 to 5 (scm-folder clones, pass-through, `atrium_git_url`, change requests between rooms) designed
- Git sync stages 2 to 4 — a worktree per card made by the room, a hub merge queue, then the forge (stage 4 is the hub-forge doc) — `docs/rnd/git-sync-design.md` — stage 1 built (bare repos, rooms fetching `claude/main`, hub collecting `claude/*`); stages 2 and 3 designed
- Room handoff, `atrium move` — move a card and the work it holds to another room: check, freeze, capture, park, launch, cut over, a `moved_to` chain that follows messages — `docs/rnd/room-handoff-design.md` — designed, @review OK b56ca32f; stage M1 queued to @runtime, nothing in code (`moved_to` absent on claude/main)
- Room-to-room access — let a director on one room fix another machine through typed ops the hub runs with its own ssh, and only second a per-pair reach grant — `docs/rnd/room-to-room-access-spike.md` — spike, 4 questions held
- Rolling room restart (f-011) — upgrade atrium without stopping agents, by moving ptys into a separate pty host that outlives the room — `docs/rnd/rolling-restart-design.md`, `docs/rnd/f-011-stage0-spike.md`, `docs/terminal/ptyhost-protocol.md` — partly built: stage 0 spike and f-011b to d (the host, reattach, tests) landed behind a setting; the backlog still says waiting on clint's approval (`docs/backlog/fabric/f-011.md`)
- Room deploy hold — one call asks for a room deploy, every agent holds, the room redeploys, every agent resumes — `docs/rnd/room-deploy-hold-design.md` — built (r-deploy-hold, `docs/changes/r-deploy-hold.md`); follow-ons R5 and R6 of resume-says-continue parked
- Two rooms on one machine (f-004) — bring a second room up beside the first to migrate, and move a card's branch room to room — `docs/fabric/f-004-two-rooms-design.md` — designed, accepted by @rnd; stage 1 waits on f-019; in @fabric's queue
- More than one room per machine, and a room that drains — `docs/rnd/multi-room-design.md` — designed only; backlog item 59 parked as deep backlog by clint
- Resource inventory (f-003) — a per-room list of what an agent may use (`atrium_resources`, `resources.md`) — `docs/fabric/f-003-resources-design.md` — designed, accepted, low; stage 1 queued at @fabric
- Room requirements file — `atrium.requirements.yaml` per project, and a command that checks a room, fixes what it can and lists what needs a human — `docs/fabric/room-requirements-design.md` — partly built: the yaml, `room-git init -Check` and `scripts/room-check.ps1` (f-013, f-014) landed; the full design (f-005) not
- Room autostart — a room comes back by itself after a logoff or a reboot on Windows, macOS and Linux — `docs/rnd/room-autostart-design.md` — partly built (`scripts/atrium-autostart.ps1` logon task, Linux systemd user unit via packaging); 6 questions; the unattended-reboot path unproven
- Overlay room identity (decision 18) — what proves a room's name over an overlay: the hub-signed certificate on every transport — `docs/rnd/overlay-room-identity.md` — built as f-022
- Pinned strip across rooms (item 52) — a drag in a pinned strip holding two rooms' cards saves the order on both — `docs/fabric/pin-order-rooms-design.md` — built (5263f9a9)
- A card on every room (item 49) — the orchestrator appears on every room's board through a hub index — `docs/rnd/everywhere-card-design.md` — built ("everywhere" commits, 1a98cf34)
- Remote launch — hand a backlog item to another machine and keep orchestrating from here — `docs/fabric/remote-launch.md` — mostly built (launch on `room=`, routing, launch caps); permission forwarding and the four "send work" answers listed as not built
- Federation v2, the forum — one board over many machines, a forum that holds nothing and leaves that dial in — `docs/rnd/federation-design-v2.md`, `docs/rnd/forum-implementation.md` — superseded in part (see section 3); its shape lives on as the hub and rooms (`docs/fabric/hub-room-plan.md`, built)
- Room machine for $2,500 — buy one Linux desktop (16-core Ryzen, 64 GB) as the new heavy room, keep the Mac mini, retire the laptop as a work host — `docs/rnd/room-machine-2500-design.md` — research, nothing bought, 3 questions held
- Handle-addressed HTTP — a script names a card by `@handle` the way an agent does and the hub routes it — `docs/rnd/handle-addressed-http-design.md` — built (`changelog/runtime/2026-09-30-r-handle-http-*`)
- Card URLs — a card has a readable address made of names, and every share keeps it — `docs/rnd/card-urls-design.md` — built (`docs/changes/u-card-urls.md`)
- Hub documents — a place on the hub to publish, version and view what agents and clint write, with stable links — `docs/rnd/hub-documents-design.md` — stage 1 built (D1 API, D2 views); attaching as context and later stages designed
- Multi-tenant atrium — several people on one atrium — `docs/rnd/multi-tenant-decision.md` — decided not to build yet (authentication exists but is a turnstile, not a boundary)

### Review and change lifecycle

- Change record — one record per branch or PR with every fact pinned to the commit it was about, so "where are we" is a read (`atrium change`, `atrium_change`, `atrium_record`) and a new PR's session is handed related records — `docs/rnd/change-record-design.md` — designed, held; 3 questions
- Change lifecycle — eight stages computed from evidence (work, tested, reviewed, walked through, signed, finished, PR open, merged), with walls against an early push and clint as the only signer — `docs/rnd/change-lifecycle-design.md` — designed, @review OK after HOLD 44dd83df; stages L1 to L7 held; 4 questions
- Review on atrium — one call (`atrium_review`) runs the reviewer panel as quiet child cards and wakes the caller once with one merged report; amended to standing personas with stable handles and a per-repo knowledge base — `docs/rnd/review-on-atrium-design.md` — designed, held; 7 questions
- Pulls view — PR review that atrium runs itself: one checkout, one prime session that every reviewer forks, JSON findings merged by code, a walk on the board; target under 10 minutes and under $2 (PR 378 took about 1h49m and $7) — `docs/rnd/pulls-view-design.md`, `docs/rnd/pulls-api.md` — partly built: P1a to P1c and P2 landed; P3 (doors, second opinion) parked on HOLD c0bccc01; the end-to-end acceptance not run
- Review tab, the walk — walk a PR review on the board hunk by hunk, as clint did by hand on one PR and liked — `docs/rnd/review-tab-design.md` — stage 1 (the walk drawer, u-005) built; stages 2 and 3 folded into the pulls view
- Reviews that remember (item 16) — a review panel keeps what it learned per repo instead of re-reading everything per reviewer per PR — `docs/review/review-memory-design.md` — designed; stage 1 waits on clint's yes, stage 2 on 3 questions; partly overtaken by review-on-atrium's knowledge base and `REVIEWER-NOTES.md`
- A PR card that registers itself with @review — `docs/review/review-pr-start-design.md` — proposed by @review, accepted with changes by @rnd; status unclear, folded into the pulls flow
- PR and CI state on the card — PR state, checks and review decision on the card that owns the branch, and CI failures fed back to it — `docs/rnd/pr-ci-state-design.md` — designed, clint ranks; 3 questions
- Changes view — every card shows what it changed, per turn — `docs/rnd/changes-view-design.md` — partly built (the room's `/changes` endpoint, `changelog/runtime/2026-10-01-r-changes.md`, and code review on the phone, `docs/changes/u-m-changes.md`); the desktop view as designed not confirmed
- Peer review on the mercurius protocol inside an atrium session (item 30) — `docs/review/peer-review-brief.md` — not started, 2 questions

### Operator focus and decisions

- The decision list — one numbered, never-reused list of every open decision on the hub, answered in batches by number (`12 y, 14 n, 15 b`), parsed by plain code, handed to the asking card with no model in between — `docs/rnd/operator-focus.md` sections 2.1 to 2.4 — designed; stage L0 (a file on sg4, outside the repo, 51 items) is in force; L1 to L5 held
- Safe defaults on a deadline — a row may default after two days only if its typed effect is internal and undoable — `docs/rnd/operator-focus.md` 2.3 — designed, held (revised for review HOLD e40a3c27)
- One reminder a day — a single daily bell item and a "where things stand" page built from rows with no model, plus at most one urgent per director per day — `docs/rnd/operator-focus.md` 2.6, 2.7 — designed, held
- Idea capture — `idea: ...` becomes a row with a suggested owner — `docs/rnd/operator-focus.md` 2.5 — designed, held
- Two-part director reports — at most five lines for clint, the rest kept in a file outside the repo — `docs/rnd/operator-focus.md` section 3 — in force as a habit (L0), no build
- Interview mode — a design interview shown one question at a time with buttons, a default, free text, progress and "back", driven by an `atrium_interview` tool, answers kept on the hub outside every repo — `docs/rnd/operator-focus.md` 2.9 (stage L6), `docs/rnd/interviewer-brief.md` — designed, held; the brief is a standing template in use
- The clint inbox — questions and reports land as data on the hub and answers go back with one tap — `docs/backlog/rnd/rnd-new-clint-inbox.md` — held; to be built as one item with the decision list
- Held-message escalation — a message or card that has waited too long says so once, and the card face shows it — `docs/rnd/held-message-escalation-design.md` — partly built (R1, R3 landed); R2 parked
- Persistent growler — an alert that stays until acted on or dismissed — `docs/rnd/persistent-growler-design.md` — built (`docs/changes/u-growler.md`), then switched off at clint's ask and its job moved into the bell (u-growler-off, u-remind-me)
- Reply suggestions — quick-reply buttons that say what this question's answers actually are — `docs/rnd/reply-suggestions-design.md` — designed (RS1 to RS4 split @ui/@runtime), not built
- A board freeze and a token and tool-call budget that trips it — `docs/rnd/freeze-budget-design.md` — design approved by clint 2026-09-30, build later (r-037); the factory refactor extends it to a pause with a scope and named exceptions
- What am I working on — a "you, now" section of the cards clint touched last — `docs/rnd/what-am-i-working-on-design.md` — closed: rejected (see section 3); only the "sort by started" option was built

### Runners and runner switch

- Runner switch — `atrium move <card> --runner codex|opencode|gemini` when a Claude account runs out: always a cold start from a brief, early capture at 90% of the limit, a data-only brief when the account cannot take a turn, and `--back` resumes the original Claude conversation — `docs/rnd/runner-switch-design.md` — designed, @review OK after HOLD 6ea94216; held; 4 questions
- OpenCode token routing — use the cheaper Kimi and OpenCode Go models only where a Claude step already checks the result (second opinions, first-pass search, drafts), bake off for a week before routing, and resolve a terms-of-service question before any unattended use — `docs/rnd/opencode-token-routing.md` — designed, held; 4 questions
- LLM-agnostic hooks — every atrium hook works for every runner — `docs/backlog/runtime/r-new-llm-agnostic-hooks.md` — wanted, design first
- Runner scoping — offer only the runners a room can actually start — `docs/fabric/runner-scoping-design.md` — status not stated in the doc; not confirmed built
- Keeping codex up to date (item 12) — sections 3 and 4 (update policy) — `docs/runtime/codex-update-design.md` — sections 1 and 2 built; 3 and 4 designed, waiting on clint
- Child runners spawned by an agent, grouped under it — `docs/rnd/spike-nested-subagents.md` — spike; the folding under a parent was later built (`docs/changes/child-fold.md`)
- `dcode` (Deep Agents Code) as a runner row — `docs/rnd/langchain-openwiki-spike.md` — held, low
- Lean context cycle — a cheaper, less stale new-context cycle that waits for the turn instead of a capture prompt — `docs/rnd/lean-context-cycle-design.md` — parked by clint 2026-09-30 (but lever 1 of operator focus leans on it for @review)

### Intake and PR review

- Intake from systems that are not a directory — a card started from a GitHub issue, a review request, a support ticket or a forum topic, as an "offered" card that has no runner yet — `docs/runtime/intake-design.md` — designed, "nothing is a promise"; the pulls view's doors are its first concrete slice
- Source control and ticketing, inbound and outbound — `docs/runtime/scm-design.md` — partly built (the recognisers and providers, `docs/runtime/providers-design.md`); the rest designed
- Outside-repo contribution gate — before cloning someone else's repo, read license, CLA or DCO, AI policy, toolchains and test OSes from the forge API and tell clint what he would sign up for — `docs/rnd/factory-refactor.md` (held on claude/rnd) section 3, `docs/rnd/change-lifecycle-design.md` — designed, held
- Pluggable backlog — atrium drives the backlog, with files, atrium, GitHub, GitLab or Bitbucket issues as backends — `docs/backlog/rnd/rnd-new-backlog-in-atrium.md` — spike not started, held for clint by name
- Scheduled launches — a row with a schedule and a launch request; the nightly test suite first — `docs/rnd/scheduled-launches-design.md` — designed, build later (r-038)
- Item dependencies — work that waits on other work, with a gate only the board resolves — `docs/runtime/item-dependencies-design.md` — built (`docs/changes/r-new-item-dependencies.md`)

### Supervision, terminals, UI

- Process registry — a long-running background process an agent starts, owned by its card, its lifetime tied to the runner — `docs/rnd/process-registry-design.md` — designed, revised to clint's answers; stage 1 waited on one-atrium; nothing in code
- Background hold — a worker that ends its turn with background shells still running is not marked stuck — `docs/rnd/background-hold-design.md` — built in r-007 stage 5 ("background work blocks parking")
- Resume says continue — a card a restart interrupted is told to carry on, an idle one is left alone — `docs/rnd/resume-says-continue-design.md` — partly built; R5 and R6 parked
- Restart and keep-alive only for what a human uses (items 38, 39) — `docs/rnd/keepalive-policy-design.md`, `docs/rnd/restart-idle-spec.md`, `docs/rnd/keepalive-marked-spec.md` — the policy draft replaced the two specs and its parking half shipped as r-007; marked keep-alive and resume-only-working wait on item 37
- Keep-alive fork carries the card's launch args (item 73) — `docs/runtime/keepalive-fork-args-design.md` — built (folded as DO)
- Terminal resize decoupling — a viewer's size no longer resizes the shared pty, and no preamble on attach — `docs/terminal/terminal-resize-decoupling-design.md` — design revised after t-003b; partly built
- Shared multi-pane input — keystroke fan-out to several panes — `docs/terminal/multi-pane-input-design.md` — display-only half built, room side parked
- File transfer and paste into a session — `docs/runtime/file-transfer-design.md` — designed, "nothing here is built" (hub documents and the phone file viewer covered part of the need)
- Event sink stage 3, the permission table — bound the permission table with a trim marker — `docs/rnd/event-sink-stage3-design.md` — designed, reviewed ready to build, waits on clint's answers (5 questions)
- Agent lineage — record who started whom and draw it — `docs/rnd/agent-lineage-design.md` — partly built (`report_to`, launcher wakes); the "you started this" grouping not
- Owed report — a card owes its launcher a report only for a prompt its launcher sent — `docs/rnd/owed-report-design.md` — built (`promptOwes` in code)
- Merged cull — a merged worker is culled with nobody remembering to — `docs/rnd/merged-cull-design.md` — built (`internal/daemon/mergedcull.go`), but the factory log says nothing calls it automatically end to end; the refactor ranks "clean up finished helpers" #2
- Machine health in the gear, and "profile this machine" — `docs/rnd/machine-health-design.md` — parked by clint
- Open with the system — open a folder in the file manager or a file in the system editor from a card — `docs/rnd/open-with-system-design.md` — parked by clint
- Defender advice — notice antivirus eating a Windows room and say the fix — `docs/rnd/defender-advice-design.md` — parked; the install-time half was built separately (`docs/changes/f-new-defender-at-provision.md`)
- Web Push for the phone — buzz a locked phone, end-to-end encrypted, run ntfy first — `docs/rnd/web-push-design.md` — designed, not built
- Telegram notifications — outbound through the existing notify command with clint's own script; two-way refused — `docs/rnd/telegram-notify-design.md` — designed; needs no atrium code
- OpenTelemetry export — traces, metrics and logs to a collector the operator runs — `docs/rnd/otel-export-design.md` — designed, revised for HOLD 0d5d2db5; 4 questions
- Usage tab — cache reads out of the way, plus what else the tab should say — `docs/rnd/usage-tab-design.md` — built (u-011, u-027, burn chart)
- Mobile design workshop — the board and the terminal on a phone — `docs/backlog/ui/mobile-design.md`, `docs/rnd/mobile-research.md` — largely built as `/m` (u-m-* change files); the draft's open options remain
- Preview board — a second daemon on a copy that acts on nothing — `docs/ui/preview-design.md` — built
- Security design — close the browser edge first, then tell the board apart from a plain script — `docs/rnd/security-design.md` — stages 0 and 1 built (r-security-edge); later stages designed (kept high level here on purpose)
- Loopback is not the operator behind a local proxy — `docs/rnd/local-proxy-trust-design.md` — LP1 built; LP2, LP3 wait

### Software-company org (directors, workers, orchestrator)

- Factory shape — keep directors but make them cheaper: @review on Opus, coding directors on Sonnet with a context ceiling, @rnd on demand; hub orchestration "partial" — `docs/rnd/factory-shape.md` — partly built (the 150k director ceiling r-director-ceiling, `fyi` kind r-fyi-kind, one-click deploy-ready r-deploy-ready); the automatic deploy after a week of clean clicks not built
- Factory refactor — a standing, re-ranked list of builds that remove the most hand work: auto-landing reviewed work (30 hand steps a day), auto cleanup of finished helpers (20), the clint inbox (40 relays and about 4M context tokens a day), a scoped pause, cross-machine reads, a verifier on any unreviewed AI output, the outside-repo gate — `docs/rnd/factory-refactor.md` (held on claude/rnd) — standing doc, nothing built, held (revision 2 not landed, public-repo question)
- Remove the orchestrator from between clint and the directors, in five steps — `docs/rnd/factory-refactor.md` (held on claude/rnd) section 2 — designed, held
- The factory feeds itself — the factory log gathered from the hub's own records instead of written by hand — `docs/backlog/rnd/rnd-new-factory-feed.md` — held
- Roles — a director anyone can define, share and launch — `docs/backlog/runtime/r-003.md` — wanted, design first
- Work ledger stages 2+ — who did what, where it is, and who said it was done; `done` checked against the worktree — `docs/runtime/work-ledger-design.md`, `docs/runtime/work-ledger-plan.md` — stage 1 built (70ed833b); later stages designed
- Agent-to-agent reliability stages 2+ — no silent stall, no silent loss — `docs/runtime/a2a-reliability-design.md`, `docs/rnd/turn-end-spike.md` — stage 1 built; the spike asks clint to reverse one decision
- Worker gateway (r-035) and lean workers — a worker boots with only what it needs — `docs/runtime/worker-gateway-design.md`, `docs/runtime/lean-workers-design.md` — built
- Software factory landscape — 23 factories surveyed; none combines atrium's four traits; borrow status-from-facts, landing rules as policy, evidence per run, a plan gate — `docs/rnd/factory-landscape.md` — spike, 4 questions

### Overlays

- OpenZiti and zrok flow — atrium drives an overlay rather than becoming one, designed with clint by interview on 2026-09-19 — `docs/fabric/ziti-zrok-flow-design.md`, `docs/fabric/overlays.md` — built up to the bind (rooms join over mTLS, private zrok, or OpenZiti)
- zrok public share for OpenWiki's Slack OAuth tunnel — `docs/rnd/langchain-openwiki-spike.md` — designed, about a day, an upstream PR for clint to decide on; the trial is held until the pause ends
- Hub hosts setting — the names the hub answers, in the gear — `docs/backlog/runtime/r-new-hosts-setting.md` — built (`changelog/runtime/2026-09-30-r-hosts-setting.md`); the gear row parked

### Other

- Postgres probe — all 80 DDL statements across 38 migrations apply to Postgres 17 unmodified; what breaks is the halt, the migration runner and four read-then-write pairs — `docs/rnd/postgres-probe.md` — research, no port planned
- Hook coverage — what the seven unused Claude Code hook events would buy — `docs/rnd/hook-coverage-spike.md` — spike
- mcp-gateway, llm-gateway and sterling fit — `docs/rnd/spike-mcp-gateway.md`, `docs/rnd/gateway-fit.md`, `docs/rnd/ai-platform-fit.md` — spikes; ideas 2 and 3 built as f-020 and f-021
- Competitor reads — Charon, Orca, bb and the wider field — `docs/rnd/charon.md`, `docs/rnd/orca.md`, `docs/rnd/bb.md`, `docs/rnd/competitors.md`, `docs/rnd/competitor-features.md` — research; competitor-features fed the changes view and PR/CI designs
- Packaging and publishing costs — `docs/release/packaging.md` — partly built (deb, service)

## 3. Wanted (asked for or queued, not designed yet)

- Adopt a session started outside atrium as a card (`atrium adopt <pid|session>`) — `docs/backlog/rnd/rnd-new-adopt-session.md` (held, low)
- A chat message becomes work without a paste — `docs/backlog/rnd/rnd-new-chat-intake.md` (held, low)
- Forge-side scm work for outside repos, own-PR authoring — `rnd-new-scm-forge`, `rnd-new-own-prs-authoring` (on sg4, untracked; named in `docs/rnd/factory-refactor.md` (held on claude/rnd))
- A card owns what it created, and closing it cleans up (worktrees left by PR reviews) — `docs/backlog/runtime/r-006.md`
- Roles anyone can define and launch — `docs/backlog/runtime/r-003.md`
- Every hook works for every runner — `docs/backlog/runtime/r-new-llm-agnostic-hooks.md`
- Move a card between rooms as a command and an MCP tool (the item behind room handoff) — `docs/backlog/runtime/r-new-move-card-between-rooms.md`
- Notice a session on an old runner and restart it with one click — `docs/backlog/runtime/r-new-restart-to-update.md` (parked)
- A launch stuck at Claude Code's folder-trust prompt — `docs/backlog/runtime/r-new-trust-prompt-stall.md` (parked)
- The settings importer applies deny-first like Claude Code — `docs/backlog/runtime/r-045.md`
- Restart resumes only working cards; keep-alive only marked cards — `docs/backlog/runtime/38.md`, `39.md` (wait on 37)
- Board views beyond groups (saved filters, by role, by launcher) — `docs/backlog/ui/50.md`
- A better signal than toasts that an agent is working or ready — `docs/backlog/ui/95.md`
- The toast log follows you between browsers — `docs/backlog/ui/96.md` (deep backlog)
- Per-card notification log — `docs/backlog/ui/14.md` (tentative, clint unsure)
- Answer an agent's open questions from the board — `docs/backlog/ui/u-004.md`
- No notifications from agent-launched cards (a gear checkbox) — `docs/backlog/ui/44.md`
- The notification drawer can turn notifications off — `docs/backlog/ui/79.md`
- "New context" on the terminal list's right-click menu — `docs/backlog/ui/71.md` (end of backlog, clint unsure)
- Growler in a popped-out window — `docs/backlog/ui/u-new-growler-popout.md`
- /m takes less vertical space, pinch sizes the text — `docs/backlog/ui/u-new-m-compact.md`
- A mobile styling pass over the whole board — `docs/backlog/ui/u-001.md`, audit `docs/backlog/ui/u-001-audit.md`
- No "ready" alert while a card waits on its own children — `docs/backlog/ui/u-new-no-ready-while-children-run.md`
- Machine bootstrap reuses the operator's shared folder; add-a-machine dialog (46 stage 2) — `docs/backlog/fabric/75.md`, `46.md`
- What it took to make a machine contribute, written up as a design — `docs/backlog/fabric/f-005.md`
- A worktree helper that links every CLAUDE.md into workers (HIGH, FIRST) — `docs/backlog/release/76.md`
- A merge pipeline that does not conflict or rerun (HIGH) — `docs/backlog/release/77.md`
- Evaluate every test for efficacy — `docs/backlog/release/m-001.md`
- A deploy's revert snapshot named after the right file — `docs/backlog/release/65.md`
- "Asked, not answered", a standing list waiting on clint — `docs/backlog/release/13.md`
- Security follow-ups from the 2026-10-01 audit, held by the pause (details deliberately omitted) — `docs/backlog/runtime/r-new-sec-*.md`, `docs/backlog/ui/u-new-sec-*.md`, `docs/backlog/review/review-new-security-audit-kimi.md`
- sg4 re-evaluation of relay cost (the orchestrator's half of the token study) — `docs/rnd/operator-focus.md` 4.5 (T1, after the pause)
- The OpenWiki trial on one repo — `docs/rnd/langchain-openwiki-spike.md` section 5 (held until the pause ends)
- Two smaller spikes: ACP as a harness protocol, and a sandboxed room type — `docs/rnd/factory-landscape.md` section 0
- Older, from the orchestrator's dispatch queue (around 2026-09-06 to 09-20): publishing atrium, many boards on one machine, watching the fleet, context and rate limits on a card, handing somebody a session — `docs/orchestrator/dispatch-queue.md` sections A to S

## 4. Abandoned or replaced

### What shipped, then went

- **v1 chat-window broker (Mode A) replaced by the v2 task board.** June's atrium was a TUI where each claude session loaded an MCP tool `submit` that long-polled the hub; the hub was amnesiac by rule. v2 (09-01, `fdc5c8e6`) reframed it as a task tracker with live agents and reversed "restart equals reset" with SQLite. The TUI rewrite against HTTP (stage 5) was **abandoned**; the TUI, `atrium agent`, `internal/tui`, `internal/agent` were deleted instead. Evidence: `docs/architecture-v2.md` "Staged migration", `website/docs/story.md`, `docs/fabric/one-atrium-plan.md`, `c739bdc7` (09-24).
- **Mode B read-only aggregator.** `atrium serve|status|watch`, `internal/server`, `internal/state` (the June scaffold's own files, `fc4ccd0a`) removed: nothing called them and `gwt watch` tails the same ledger. `a8afe39b` (09-25).
- **The `{choices}` picker.** A convention taught by the v1 `atrium-agent` tool and rendered by the TUI, retired with Mode A (`docs/test-plan.md` section D). Quietly reborn on 09-30: a `{choices}` block in a question growler becomes reply buttons (`changelog/ui/2026-09-30-u-growl-reply.md`).
- **Adopting sessions from the gwt ledger.** Implemented then removed: it turned every session the ledger ever saw into a card nobody could talk to, hundreds of ghost "waiting on a human" cards. Replaced by SessionStart/SessionEnd hooks. `docs/architecture-v2.md` "Abandoned".
- **React SPA.** The decisions table chose React+Vite; the board shipped as a plain page on the same JSON+SSE contract and stayed that way. `docs/architecture-v2.md` stage 6.
- **Heartbeat federation.** `atrium room` posting to `/v1/rooms` every 20s (`c78ff42c`, 09-06). Superseded by the hub/room mTLS link (09-17). Notably the first federation design (`docs/rnd/federation-design.md`) argued against a central aggregating atrium ("federate in the client"); the hub got built anyway, with no card state.
- **"The hub holds nothing" / "the forum holds nothing."** Eroded in steps: hub store (decision 11), then decision 19 (09-29) made the hub own the integration branches, explicitly reversing one sentence of decision 11 and retiring the federation-v2 rule. Then hub documents (09-30) and a hub git store (10-02). `docs/decisions.md` 19.
- **The hub as its own room.** Dropped 09-18 (`c4248d71`); old hubs drop the defunct row at startup (`b5c2cf4d`).
- **Anonymous joins.** `atrium2 hub token` and `--name` on the room removed 09-17 (`153078c`) because a join string authorised any name. Overlay joins kept the gap (decision 18, "NOT SETTLED") until f-022 put mTLS inside the overlay on 09-30, with legacy rooms marked unproven.
- **atrium2 as a second binary.** `cmd/atrium2` shim and cutover/rollback scripts removed 09-26 (`ce1a458f`, `3bb76c0b`). The plan's third step, renaming "the hub" to "the atrium", is in the plan (`docs/fabric/one-atrium-plan.md` decision 3) but the vocabulary on routes (`/_hub/...`) and in changelogs is still "hub".
- **Per-session stdio control MCP.** Each session spawned its own ~24MB `atrium-control`; replaced by one HTTP MCP on the hub, 09-18 (`d192062`).
- **`atrium install`.** Written and removed before shipping: copying a file is the shallow half of installing. Replaced by real packages. `website/docs/story.md`.
- **A Windows service for autostart.** A service runs in session 0 and cannot open a pty you can attach to; it would report Running and supervise nothing useful. Logon task instead. `website/docs/story.md`.
- **The v1 `start-atrium.ps1`.** Stubbed 09-19 (`ee7a589c`).
- **Dragging cards on the board.** Removed 09-06 (`c67b3457`): drag stole text selection, and copying paths off cards is daily. Menu moves replace it.
- **Projects button / `worktree_command`.** Scanned two levels for `.git` and ran a shell template; replaced by Providers 09-22 (`17fdffae`) because it guessed a layout and ran a command atrium could not see into.
- **`scripts/sources/runner-updates.ps1`.** Started each runner binary every 10 minutes to read its version; replaced by an in-process `package.json` + registry check 09-15 (`2017744d`).
- **The flattener for scrollback.** Replaced by a screen-model replay 09-14 (`53b728cb`). The changelog admits the rendering was twice declared fixed on tests written beside it and twice reverted; `replay_flat = on` remains as the escape hatch. Commit subjects on 09-14 (`93f6e59f`, `38572ba0`, `c6b9eda7`, `53b728cb`) read like a diary of the hunt, ending in a hopeful "finally".
- **Width change replaces transcript in scrollback.** Landed and reverted the same day, 09-22 (`0d99f059` then `069850cf`).
- **Collapsing tree renderer, attach preamble, width-mismatch note, model tickbox.** Removed 09-11, 09-19 (`3a6abd86`), 09-20 (`cfb383a8`), 09-14. FEATURES "Terminals: removed".
- **Peer messages always queued, never typed.** Original rule (README "Scope", backlog "Out of scope, deliberately"): no prompt injection, even where atrium owns the pty. Overruled by clint: the pty is shared, so peers type when the line is free. The README and backlog still carry the old rule. `docs/architecture-v2.md` "A message back channel".
- **"A supervised runner can never outlive the daemon."** Stated as a fact (ConPTY has no reattach) in README, architecture-v2 open risks and backlog out-of-scope. f-011 stage 0 (`555b85d5`, 09-29) showed a detached process owning a ConPTY can be reattached with no byte lost; `atrium ptyhost` shipped behind an off setting (09-30/10-01).
- **Echo plus smallest-viewer terminal sizing.** Being replaced by single-owner input/viewport ("take control"), decided in design 09-21 (decisions-log); width now follows widest viewer.
- **Persona pack.** Specialist agents with memory in `dotagents/personas/`, built through six stages on 09-23 (catalog, review-with, lessons view, pack nag), then reverted the same day (`367a38da`). Per decisions-log, clint stopped the design after round 3 and later said it was far more than the few files he expected; direction kept as one agent CLAUDE.md per repo. Personas resurface on 10-02 as standing reviewers in the review-on-atrium design.
- **Dollar cost on the board.** Usage rows were priced and shown; on 09-29 clint decided the figure was not worth having, so no dollar amount is sent or shown (`0bbd2caf`, CHANGELOG top). Ironically the factory analysis a day later (`docs/rnd/factory-shape.md`) is written almost entirely in dollars.
- **ntfy for phone notifications.** Ruled out in the mobile design round (token concerns) and replaced by a generic hub notify command (`d5353bef`, `96b693fb`, 09-29). Telegram notify later designed outbound-only via that command, two-way refused (`b5bd914b`).
- **Full phone header hide (u-016).** Replaced by a slim one-row header behind a chevron (u-021), 09-29.
- **Persistent growler on by default.** Built 09-30 as the "one row every screen reads"; on 10-02 made a per-browser setting OFF by default with the bell keeping every alert (`a8134b47`).
- **Dotfiles pwsh permission gate.** Replaced by the Go gate in the binary (r-023, 09-29) which now rewrites the old row in place (f-006 replace, `b2f52fc4`, 09-30).
- **f-002 room-side merge queue.** Superseded by f-019 hub git sync (`f00f44c2`).
- **Polling.** Board polls for rooms, inventory and auto-approve replaced by hub events (f-008); the board's own list polling replaced by SSE plus a 60s resync (u-009/u-009b).
- **Owed reports for every prompt.** A launched card used to owe its launcher a report for any prompt, so a merger rang the orchestrator for each worker message; narrowed to launcher prompts (item 41, `81d91a85`).
- **"No forced turns" (a2a stage 1).** Decided 09-23; the turn-end spike's step 2 (nudge a silent worker) would reverse it and is still undecided (decisions-log).
- **Runner env allowlist (r-033).** Built as `cd0567ba`, reverted, marked WON'T DO 09-30: every runner must start with the same environment. `docs/backlog/runtime/r-033.md`.
- **r-036 lock wait-through.** Backed out 09-30 (`bb8b7524`): it made the lock test flake more.
- **merge-check at BelowNormal priority.** Reverted 09-29 (`760eb94d`).
- **Keep-alive 5% write rule.** Dropped 09-28 (`fa2b2cce`).
- **The flat factory.** 09-27 18:00 to 09-28 22:00 the orchestrator dispatched Sonnet `saNN` workers with one merger (saorch, then @merge). Replaced at 09-28 22:00 by five resident Opus directors (@ui, @runtime, @fabric, @rnd, @review). `docs/rnd/factory-shape.md` compares eras: per commit cost about the same, ~2.3x throughput.
- **The orchestrator on claude-sg4.** Moved to its own room `sg4-control` on 09-30 so a room restart does not take it down (which then triggered the zombie-card incident below). Further, `docs/rnd/factory-refactor.md` (held on claude/rnd) (10-02) plans to remove the orchestrator from between clint and the directors entirely, after clint questioned whether it was a token-wasteful relay.
- **`/m` fixed quick replies, camera button, office doc uploads.** Removed 09-30 (`f41e6c86`, `8403c3be`, `fca1c128`).
- **`launch-refused` audit kind.** Replaced by `ctl-launch ... refused:` lines (f-020).
- **Ziti board serving, "built, not proved".** architecture-v2 stage 8 says nobody had enrolled an identity, so it was never run end to end; later overlay work (09-17 to 09-20) built transports, JWT enrol and f-022 identity.

### Designs dropped or revised away

- **Hub forge rev 1: mirror everything.** Rev 1 (d32a34fd to 1c965657, @review doc-ok 8eaa86b1) mirrored every repo on the hub automatically, fetched forge branches into `refs/forge/`, served rooms' branches under `rooms/<room>/`, kept a fetch-through copy as an offline fallback and put push in a later stage. clint's interview said no to the mirror and no to the fallback: the hub owns `main`, takes pushes first, and passes everything else through live to the room that has it, failing when that room is off. Evidence: `docs/rnd/hub-forge-design.md` section 1 table; commits 1460c81a and 1c30e5f7 (rev 1) and 018c141e (rev 2). The interviewer brief says the core picture was wrong and the interview found out only at question 4 (`docs/rnd/interviewer-brief.md` section 1).
- **The forum that holds nothing.** Federation v2 said the forum (the hub) stores nothing and is a pure pane of glass. Decision 11 gave the hub its own store (`internal/hubstore`), and clint's 2026-09-17 requirements overrode v2 on the aggregate question; the "forum" name was then dropped for "hub" when Mode A was removed. Evidence: banner at the top of `docs/rnd/federation-design-v2.md`, its section marked Superseded, `docs/fabric/hub-room-requirements.md`, `docs/fabric/one-atrium-plan.md` around line 373. No commit mentions "forum" on claude/main; `docs/rnd/forum-implementation.md` was never built as written.
- **Federation v1, a separate aggregator.** `docs/rnd/federation-design.md` argued about what sits on top of overlays; rejected mirror cards and a mirroring aggregator, replaced by v2 and then the hub/room plan.
- **Mode A and the TUI.** The plan to rewrite the TUI against the HTTP API was abandoned; the TUI was deleted with Mode A. Evidence: `docs/architecture-v2.md` migration step 5 marked Abandoned; `docs/fabric/one-atrium-plan.md` stage 1.
- **Adopting sessions from the gwt ledger.** Built, then removed: the board filled with hundreds of sessions it could see but not talk to. Hooks bring a session in when it acts. Evidence: `docs/architecture-v2.md` "Abandoned". (A new, narrow `atrium adopt` is now wanted again: `docs/backlog/rnd/rnd-new-adopt-session.md`.)
- **Room-owned git (f-002).** One integration checkout per repo on each room and a room merge queue; superseded by f-019 (decision 19), which puts integration branches on the hub. Evidence: `docs/backlog/fabric/f-002.md`.
- **A shipped permission-gate script (f-006).** Replaced by the Go subcommand `atrium hook --event permission` (r-023). Evidence: `docs/backlog/fabric/f-006.md`, `changelog/fabric/2026-09-30-f-006-replace.md`.
- **"You, now" (what am I working on).** Option B, a section of the cards clint last touched, was rejected by clint; only his own ask, sort by started, was built. Evidence: `docs/rnd/what-am-i-working-on-design.md` status, `docs/changes/term-sort-started.md`.
- **A forced end-of-turn nudge.** An earlier draft of the turn-end fix blocked the turn; clint rejected it because a forced turn spends tokens every time it fires. Evidence: `docs/rnd/turn-end-spike.md` line ~51.
- **The persistent growler, switched off.** Designed as clint's priority on 2026-09-30, built, then turned off at his ask on 2026-10-02 because it nagged more than it helped; its job moved into the bell. Evidence: `docs/rnd/persistent-growler-design.md`, commits for u-growler-off and u-remind-me, `docs/rnd/operator-focus.md` 1.2 item 6.
- **Specs for items 38 and 39.** The two spikes were replaced by one keep-alive and parking policy draft (4ed791fd). Evidence: `docs/rnd/keepalive-policy-design.md`.
- **A new "offered" status vs `backlog`.** The intake design argued for a new card status and then found the existing `backlog` status in the CHECK constraint was exactly it; the correction is kept in the doc as a lesson. Evidence: `docs/runtime/intake-design.md` around lines 201 to 223.
- **Telegram two-way, and a Telegram sink inside atrium.** Rejected: atrium will not hold that token or take approvals through a chat app. Evidence: `docs/rnd/telegram-notify-design.md` verdict and rejected options.
- **Outside review services for PRs.** Rejected in the pulls view (not clint's walk, and the service would hold a credential). Evidence: `docs/rnd/pulls-view-design.md` option C.
- **Codex update cards that never withdraw.** The old offer design was replaced by withdraw-or-rewrite in place. Evidence: `docs/runtime/codex-update-design.md` section 1, commit fc174988.
- **Flattening the org.** Considered and rejected in the factory shape: flattening saves money only because less work gets done. Evidence: `docs/rnd/factory-shape.md` "The answer".
