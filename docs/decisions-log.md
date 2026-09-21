# Decisions log

The canonical journal of decisions clint has made about how atrium (and how the orchestrator works) should behave.
Its job is memory: so a decision is made once, recorded, and not re-litigated or forgotten. Newest at the top of
each section.

Rules for adding:
- One entry per decision: the DECISION, the WHY, and where it is implemented (commit / file / doer) if known.
- If a later decision reverses an earlier one, keep the old line and mark it `(SUPERSEDED YYYY-MM-DD)`.
- Design-interview Q&A that only needs dedup lives in `docs/interview-log.md`; behavior changes to the orchestrator
  itself are also logged one line each in `dotfiles/claude/tuning-changelog.md`. This file is the human-facing
  index of the substantive product/architecture decisions.

## 2026-09-21

- **Subagents inside doers: allow READ-ONLY (Explore/Plan), redirect the rest.** A doer using Explore to read the
  codebase is not the ephemeral-work problem the guard exists for. The `Task` guard
  (dotfiles pre-tool-use-hook.ps1) now exempts Explore/Plan and still redirects other subagents to atrium_launch.
  Every subagent start is logged (type + description = the why) to state.log / agent-log. Why: subagents fire ~50x
  a day here, almost all read-only Explore; blocking them outright would cripple the doers.
- **Terminal input + viewport OWNERSHIP (design, in progress).** One owner per terminal: whoever holds it types
  and sets the pty/viewport size; another pane "takes control" to steal input and sizing. This replaces the
  echo-plus-smallest-viewer model and removes the operator-vs-peer lock fight (typing lag) and the resize churn.
  DECIDED: ownership gates ONLY human typing - `atrium_say` / the peer bus is UNCHANGED. Still open: keep or drop
  multi-pane echo, the take-control UX + term, and who owns at start. The typing-lag split-lock is a separate fix
  still needed because peer injection keeps its lock.
- **Runners are room-scoped.** The new-agent picker must offer only the runners the SELECTED room supports
  (`found` = binary resolvable on that room). A runner should also carry an explicit per-room `bin_path`, not rely
  on PATH only (sgg had claude installed but off the room PATH). Picker filter = hub-only; bin_path = room-side,
  parked. Doer: runner-scoping.
- **Flicker is two bugs.** (1) attach-upgrade STALL when resolving a bare id across rooms - FIXED, live, hub
  roomHolding returns on first claim (3bf0f30). (2) room-flap: when the room set changes, card ids flip
  tagged<->bare and the board pane loops teardown/reattach forever on the stale tagged id, eating keystrokes and
  exhausting WebGL - STILL OPEN, doer flicker-flap, fix is to re-resolve the remembered id on a room-set change.
- **Cursor-on-attach: forward fix, keep the resize churn-guard.** The replay never restored the cursor; the old
  resize-on-every-attach masked it with a SIGWINCH repaint, which the churn-guard (8400fa8, correct) removed. Fix:
  the replay restores the cursor itself (8e95e21). Do NOT revert 8400fa8. Room-side, parked.
- **Deploy discipline: never hub/room restart while clint is active at the terminal.** An auth-fix hub deploy fired
  mid-typing bounced the board and caused a cursor/flicker mess. Gate deploys on clint being idle, or bundle into
  one announced maintenance window. A single-room restart keeps ids bare (no tagged-id flap); a hub restart drops
  every board pane.
- **Recycle idle validated doers to free the launch cap.** Once a doer's work is committed/merged and it is idle,
  `atrium_exit` it (card + branch + history persist) to make room under the 10-session cap. clint authorized
  culling idle validated doers without asking. Do NOT exit a doer mid-task.

## Before 2026-09-21

- **Board-share auth** (see docs/interview-log.md): public zrok share auth = OIDC or basic (updb) with creds set on
  the settings screen; a updb login needs BOTH name and password to count; a hub public share gets creds from zrok
  updb only (hub grows no login); JWT-enroll-at-join for both transports.
- **The board stays loopback, always; off-machine reach is overlay only** (docs/ziti-zrok-flow-design.md).
- **Hub-only vs room-side deploys.** The hub holds nothing and is disposable (hub restart deploys embedded board +
  hub/link Go). Room/daemon Go changes need a planned room restart, so work is split into a hub-deployable phase
  now and a room-restart phase parked. See the orchestrator-mode memory.
