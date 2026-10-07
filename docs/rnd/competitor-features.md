# Competitor features: what atrium should build next

Status: research, 2026-09-30. Its conclusion: five features to build first, led by a board freeze with a budget and a
card that carries its PR and CI state.

Written 2026-09-30 for rd-002 by worker rd-002, for @rnd and clint. Research only, nothing was run and no code was
changed. It builds on `docs/rnd/competitors.md`, `docs/rnd/orca.md` and `docs/rnd/bb.md` and does not redo them.

**Origin: done, 2026-09-30.** The worker wrapped up at its context limit, and @rnd then checked the atrium side in
code for every ranked item and the matrix rows that decide them (section 7 says what was checked). The other HAVE,
PARTIAL and MISSING marks rest on README.md, CHANGELOG.md and `docs/rnd/competitors.md`, which traced code on
2026-09-29.

Method: competitor claims come from three kinds of evidence, and each item says which. **Release notes** are GitHub
release bodies fetched with `gh api` (Orca, herdr, ruflo, oh-my-claudecode). **README** means the repository README
fetched with `gh api repos/<r>/readme` on 2026-09-30, cited as README line numbers of that raw file. **Source** means a
path in a local clone from the earlier survey, and only `docs/rnd/competitors.md` and `docs/rnd/orca.md` did that.
Nothing new was read at source depth for this item.

## 1. Recommendation

Build these five first, in this order. (1) **A board freeze and a token and call budget.** Every serious control plane
now has one (Gas Town's estop, Omnigent's `cost_budget` and `max_tool_calls_per_session` policies, ruflo's global AI
budget), atrium's dollar figure was hidden on purpose (item 37) so the guard has to be in tokens and calls, and it is
the cheapest way to stop ten unattended agents from spending a night. Small to medium, runtime. (2) **A card that
carries its pull request and CI state, and moves to needs-you when CI fails or changes are requested, with the failure
sent back to the same card.** Agent Orchestrator derives its whole board from that, cmux shows the linked PR in its
sidebar, Superset and Orca send review comments to the agent that owns the work. Atrium columns already mean "a
human has to act", so this is the fact that most often should move a card and does not. Medium to large, review with
runtime. (3) **A Changes view on every card**: the live diff of the card's worktree, with selected lines sent to that
card as a prompt. Superset, Agent Orchestrator, Orca and cmux all have one and it is the step between "the agent says it
is done" and "accept". Medium, review with ui. (4) **Scheduled launches**: Superset and Orca both shipped cron
automations and Mission Control lists schedules. Atrium's sources find work on a timer but nothing launches a runner on
one. Medium, runtime. (5) **Dependencies between work items, with a gate only the board resolves**, still unfiled
and still absent from every tool read except Orca and Gas Town (item 8 below). Medium, runtime. The worker's original
fifth, paced resume, turned out to exist already: `reopenSaved` starts cards one at a time, `reopenGap` 400ms apart
(`internal/daemon/reopen.go:40-43`, `:150`). Only herdr's other half, an agent reporting its own resume command, is
left, and it is small (item 5 below). Items 5 to 10 follow in section 4.

## 2. Feature matrix

Columns are the tools read in earlier surveys plus the newer entrants of section 3. `Y` means the README, a release
note or the earlier source read says it ships. A blank means not seen, which is weaker than "does not have it". The
atrium column is HAVE (with a file), PARTIAL (with what is missing), or MISSING. Tool keys: **O** Orca, **H** herdr,
**R** ruflo, **M** oh-my-claudecode, **V** vibe-kanban (sunsetting), **S** Symphony, **G** Gas Town, **HL** humanlayer,
**C** claude-squad, **B** bb, **AO** Agent Orchestrator, **SS** Superset, **OG** Omnigent, **AT** Atlas, **MC** Mission
Control, **PS** Paseo, **CM** cmux, **OM** open-multi-agent.

### Launching work

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| Worktree per agent | O, C, G, AO, SS, PS, CM, V | PARTIAL. A launch takes a directory you prepared. `scripts/room-git.ps1 worktree` makes one on a room. Room owned git is f-002 |
| Start from an issue or ticket URL | O, V, S, SS (Slack, Linear), AO | HAVE. `internal/store/recognisers.go`, `internal/store/sources.go` |
| Scheduled or recurring runs | SS (automations), O (automations with cron), MC (schedules) | MISSING, checked. Sources run a command on a timer and raise an inbox card, and nothing launches one on a schedule (`internal/store/sources.go:29-35`) |
| Many agent types | O (19 hook installers), AO (32 agents), H (22 detect manifests), PS, OG | PARTIAL. Runners are rows in `internal/store/harness.go`, claude and gemini have setup adapters in `internal/runnersetup/` |
| A task graph with dependencies | O (`coordinator.ts`), G (molecules) | MISSING. Ledger states exist in `internal/store/ledger.go`, no dependency edge |
| Launch held under a cap | G (scheduler) | PARTIAL. The cap refuses. See competitors.md 3.1 |
| Setup and teardown scripts per repository | O (`orca.yaml`), SS, S (`WORKFLOW.md` hooks) | MISSING as a repository held file. Atrium has fixtures and export, not a per repo hook |
| Per worker browser preview and port detection | AO, SS, CM, O | MISSING, checked. `docs/ui/preview-design.md` is a preview of the BOARD itself on a copied database, not a browser pane for a worker's app |
| Trigger an agent by voice | PS, O (mobile dictation) | MISSING. In the mobile design's scope |

### Supervising and permissions

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| A gate that answers before a tool runs, with durable rules | none of the others. B, V, HL gate through their own channel without durable rules | HAVE. `internal/daemon/daemon.go` chain, `internal/store/rules.go` |
| Policies that stack per server, agent and chat | OG (README:543-579) | PARTIAL. Rules and auto mode exist, a per room ceiling does not (competitors.md rank 10) |
| Approval bound to a hash of what the reviewer saw | OM (README:91) | PARTIAL. Not checked whether an approval is tied to the exact input shown |
| Freeze everything, resume by a person | G (estop) | MISSING. competitors.md rank 4 |
| Spend or call budget | OG (`cost_budget`, `max_tool_calls_per_session`, README:564-571), R (global AI budget), MC (cost views) | MISSING as a stop. Usage is recorded (`internal/store/usage.go`) and dollars are hidden by decision (item 37) |
| Live activity per agent | O, H (hooks plus screen rules), SS, CM | HAVE. `internal/daemon/activity.go` |
| Screen detection as data | H (`src/detect/manifests`) | MISSING. competitors.md rank 2 |
| Subagent and child work status | O (v1.4.2xx notes) | HAVE. Subagent hooks in `internal/claudeconf/hooks.go` |
| Peer messaging between agents | O, G, M, PS (`/paseo-handoff`, `/paseo-advisor`, `/paseo-committee`) | HAVE. `internal/daemon/peers.go` |
| Question that blocks a card until a person answers | O (decision gate) | PARTIAL. `internal/store/ask.go` asks. u-004 is filed for the board side |

### Review, diffs, merging and git

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| Diff of the agent's working tree, with line comments | SS (README:40-55), AO (README:61), O (multi-line review comments), CM | PARTIAL. Pending edit diffs exist in the permission view (README:93). No worktree diff exists, checked (no `git diff` exec anywhere in `internal/`) |
| PR, CI and review state on the card | AO (README:77-84), CM (README:65), O (PR page) | MISSING as far as the docs show. u-005 covers reviewing someone else's PR, which is a different job |
| Send review or CI feedback to the owning agent | AO (README:94), SS (README:43), O (`pr-comments-resolution-prompt.ts`) | MISSING. `docs/runtime/scm-design.md` files it as not built |
| Stacked pull requests, Bitbucket | O (v1.4.182 and v1.4.183) | MISSING and out of scope for now |
| A merge queue | G (Refinery, not read) | MISSING. f-002 and f-019 are the filed designs |
| Commit to session provenance that survives rebase | AT (README:71-77, 124-125) | PARTIAL. `SetReportSHA` in `internal/store/a2a.go` records the commit a done report names. Nothing links every commit to its card |
| Evidence based worktree cleanup | O (`workspace-cleanup.ts`) | PARTIAL. r-006 and r-019 are filed, `internal/daemon/worktreegone.go` handles the vanished case |

### Multi-machine

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| One window over local and SSH machines | H (v0.9.0 `machine add`), SS (remote access), PS (`--host`), CM (`cmux ssh`), O | HAVE. Rooms and the hub, `docs/fabric/` |
| Wake an offline host by a custom command | SS (README:139) | MISSING |
| Search session history across every machine | O (v1.4.206 `session-search`), AT | PARTIAL, checked. Card fields are searched across every room (`store/history.go:29-66`, merged by `link/fanout.go:363`). Transcript contents are not |
| Reconnect and MFA on the SSH hop | H (v0.9.1 and v0.9.2), O (v1.4.217) | PARTIAL. Provisioning is `scripts/provision-room.ps1` and friends, f-005 and f-007 |
| Sessions survive the client closing | H, SS | HAVE for the board. The runner dies with the daemon on Windows, see README "Scope" |

### Mobile and notifications

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| Native phone app with push | O (real background push, v1.4.199), SS (iPhone, Pro), PS (iOS, Android), happy | PARTIAL. Desktop notifications and a service worker exist. Phone page is mobile-design, u-001, r-024 |
| Notify the phone only when the desktop is away | O (`desktop-away-state.ts`) | MISSING. orca.md item 9 |
| Notification rings and a panel of pending items | CM (README:37-51), SS | HAVE. The drawer, item 79 |
| Sound per card | none seen | HAVE. README:96-99 |

### Cost and usage

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| Usage per provider, rate limit readings | O (Cursor, Muse, Codex usage, v1.4.211 to 214), M (HUD rate limits, v5.5.0), MC, AT | HAVE. `internal/store/usage.go`, u-011, r-015 |
| Context window meter | O (v1.4.214 native chat) | HAVE. Item 45 |
| Auto switch account on a limit | not in the ten. Small repositories only, not read | MISSING |

### Memory and handoff

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| Shared memory every agent reads | AT (README:36-40), R (agentdb), claude-mem (95k stars, only the description read) | MISSING and see section 5 |
| Session handoff to a fresh context | AT (README:97), PS (`/paseo-handoff`) | HAVE. Item 66, new context |
| Agents report their own resume command | H (v0.9.2) | MISSING. Resume args come from the harness row |
| Resume paced one at a time | H (v0.9.2 `startup_per_agent_delay_ms`) | MISSING as far as the docs show. Item 38 is about which cards resume |

### Extensibility

| Feature | Who ships it | Atrium |
| --- | --- | --- |
| Plugins by git or npm | H, PS (`paseo plugin install`), B, AT (packs) | PARTIAL. competitors.md 3 and rank 1 |
| A CLI, socket or SDK an agent drives the tool with | H, PS (`@getpaseo/client`), SS (CLI and MCP), O | HAVE. `internal/cli/`, `internal/link/control_mcp.go`. No published client library |
| Command palette | SS, CM | PARTIAL. The switcher, `docs/ui/switcher-design.md` |
| Built-in skills provisioned at launch | SS (`superset:*`, README:157) | PARTIAL. `BRIEF.md` at launch. Orca's guide printed by the binary is rank 18 in competitors.md |

## 3. What changed, and who else arrived

### 3.1 Since competitors.md was written

competitors.md is dated 2026-09-29, one day before this. So very little moved, and the honest summary is a table of what
did.

| Tool | Stars then, now | Since the survey |
| --- | --- | --- |
| Orca | 81,569, 81,715 | Nothing after `v1.4.217` (2026-09-29 19:40 UTC), which the survey already cited |
| herdr | 41,412, 41,494 | `v0.9.2` and `v0.9.3` on 2026-09-29 (details below) |
| ruflo | 73,492, 73,525 | `v3.48.0` on 2026-09-28, before the survey. Internal packages and hardening |
| oh-my-claudecode | 39,403, 39,432 | Nothing after `v5.5.0` on 2026-09-22 |
| vibe-kanban | 28,216, 28,225 | Nothing. Last push 2026-09-19, sunsetting |

The more useful window is the last ten weeks, from the release bodies. **Orca** (54 stable releases since 2026-08-01):
stacked pull requests and Bitbucket (`v1.4.182`, `v1.4.183`), automations with a cron field (`v1.4.190`, `v1.4.198`,
and a cron step fix later), real background push for the phone (`v1.4.199`), a search of agent session history across
every connected computer and from `orca search` (`v1.4.206`), an over the air mobile web bundle served by the desktop
(`v1.4.206` to `v1.4.209`), native chat with a context window meter (`v1.4.214`), a finished turn notifying every time
(`v1.4.211`), Cursor and Muse usage providers, and subagent status across Codex, Claude, Pi and Grok. **herdr**:
`v0.9.0` (2026-09-07) manages local and saved SSH machines from one window with a combined agent list, machine scoped
navigation and reconnect, and multiple clients on different workspaces. `v0.9.1` adds `--machine` control of agents
on a saved machine and Windows SSH hosts. `v0.9.2` adds agents reporting their own resume command, restored agents
started 100 ms apart by default, `herdr machine status` and `reconnect` with MFA, and Windows panes defaulting to
`pwsh`. Its notes also record that a client update can leave compatible servers and their running agents untouched,
which is the same wish as atrium's f-011. **ruflo**: mostly memory correctness and packaging in the ten weeks read
(`v3.43.0` to `v3.48.0`), an optional model based agent picker, and a frozen 197 request benchmark used to decide its
default. **oh-my-claudecode**: `v5.5.0` is 22 new features, almost all skills and hook "judgment points", plus HUD usage
from rate limit headers. **vibe-kanban**: sunsetting.

### 3.2 New entrants with real adoption

Found with `gh search repos` on topics `coding-agents`, `claude-code` and `agent-orchestration` filtered by creation
date and stars, then a README read of each. Not source reads.

| Tool | Stars | Created | What it is | Why it matters here |
| --- | --- | --- | --- | --- |
| `omnigent-ai/omnigent` | 10,355 | 2026-06-11 | Python meta-harness over Claude Code, Codex, Cursor and others, cloud sandboxes, phone and browser | Policies that ask, cap spend or limit tools at server, agent and chat level (README:543-579). Session sharing |
| `pacifio/atlas` | 8,429 | 2026-05-14 | Tauri editor and agent runner, "source control for agents" | Checkpoints linking every commit to the session that made it, surviving rebase and amend (README:71-77, 124-125). Shared memory across agents |
| `open-multi-agent/open-multi-agent` | 6,970 | 2026-04-01 | TypeScript agent runtime library | Durable approvals bound to a hash of what the reviewer saw, and a verifiable run journal (README:89-105). A library, not a supervisor |
| `builderz-labs/mission-control` | 6,287 | 2026-02-13 | Self hosted dashboard, SQLite, alpha | Tasks, schedules, alerts, webhooks, spend, roles and approvals in one table (README:76-83). The nearest in pitch to atrium, with a login |
| `Untrivial-ai/agent-orchestrator` | 12,540 | 2026-02-13 | Desktop app and daemon, kanban derived from PR and CI | Was in the survey table and unread. Now README read (README:77-84) |
| `superset-sh/superset` | 14,751 | 2025-10-21 | Agentic IDE, worktree per agent | Automations, remote access with wake command, iPhone app (README:110-151). No Windows build |
| `getpaseo/paseo` | 19,034 | not read | Daemon plus desktop, mobile and CLI, one maintainer | Plugins by git or npm, an SDK, handoff, advisor and committee skills (README:50-53, 128-164) |
| `manaflow-ai/cmux` | 27,508 | 2026-01-28 | Ghostty based macOS terminal | Notification rings, sidebar showing branch, PR and ports (README:37-65) |
| `chaitanyagiri/munder-difflin` | 8,177 | 2026-05-31 | Local multi-agent harness | Description only, not read |
| `slopus/happy`, `pingdotgg/t3code`, `stagewise-io/stagewise` | 23,951, 23,932, 6,826 | | Phone client, agent GUI, agentic IDE | Not read beyond descriptions. happy is covered in `docs/rnd/mobile-research.md` |

No new entrant owns the terminal and gates every tool call. Mission Control and Omnigent are the two closest in intent
and both are login based servers.

## 4. Ranked list

Only what competitors ship and atrium lacks or half has. Items already filed are in 4.2, not ranked. Files were checked
to exist by name in the repo tree listed in `CLAUDE.md`, and `internal/store/usage.go`, `a2a.go`, `ledger.go`,
`sources.go`
and `internal/daemon/launch.go`, `daemon.go`, `sweep.go` were named by competitors.md, which read them on 2026-09-29.

### 4.1 The list

**1. A board freeze plus a token and call budget.** A setting `board_frozen` holding reason and who, checked in the
permission chain after a shelved card and before standing rules, plus per card and per board budgets in tokens and tool
calls that trip the freeze or shelve the card. *Who ships it:* Gas Town estop (`internal/estop/estop.go`, read in
competitors.md 2.7), Omnigent `cost_budget` and `max_tool_calls_per_session` (README:564-571), ruflo's
`global-ai-budget`
import (competitors.md 2.9). *Why:* an operator with many unattended agents needs one switch and one automatic stop, and
dollars were hidden by decision (item 37), so this is the token form. *Files:* `internal/daemon/daemon.go`,
`internal/store/settings.go`, `internal/store/usage.go`, `internal/api/settings.go`, and a board control in
`internal/api/web/`. *Owner:* runtime. *Size:* small for the freeze, medium with budgets. Not filed (grep of
`docs/backlog` for freeze, estop and budget found only keep-alive budget items). It stops the next tool call and not a
running one.

**2. PR, CI and review state on the card, and feedback returned to the owning card.** The daemon reads the pull request
for a card's branch (through `gh`, run as a bounded command like a source), shows checks and review state on the card,
moves it to needs-you on a failed check or requested changes, and offers "send this back" that says a stored prompt to
that card with the failing output or the ticked comments as untrusted data. *Who ships it:* Agent Orchestrator derives
column position from session, PR, CI and review facts and returns feedback to the same agent (README:77-84, 94), cmux
shows linked PR status in the sidebar (README:65), Superset sends diff line feedback to an agent (README:43), Orca's
`pr-comments-resolution-prompt.ts` (orca.md section 2). *Why:* it is the fact that most often should change a card's
column. *Files:* `internal/store/ledger.go`, `internal/store/actions.go`, `internal/daemon/actions.go`, a new
`internal/daemon/prstate.go`, `internal/api/web/`. *Owner:* review with runtime. *Size:* large. Related and different:
u-005 (reviewing someone else's PR) and `docs/runtime/scm-design.md` (outbound configuration and inbound recognisers).
Checked by @rnd: nothing polls PR or check state. `internal/store/recognisers.go` only turns a PR URL into a filled-in
launch dialog, and the "CI failed on main" cards on the board come from outside the repo.

**3. A Changes view per card.** The card's worktree diff (`git diff` against its base) rendered with the board's
existing diff styling, with selected lines sent to that card as a prompt. *Who ships it:* Superset (README:40-55),
Agent Orchestrator (README:61, "changed files"), Orca (release notes: multi-line review comments, collapsing unchanged
regions), cmux. *Why:* it closes the gap between "done" and "accepted" without leaving the board, and it is what a
phone page could show instead of a terminal. *Files:* `internal/api/` (a new endpoint beside `files.go` and
`filetext.go`, resolved through `internal/safepath`), `internal/api/web/`, reuse of the edit diff renderer.
*Owner:* review with ui. *Size:* medium. Checked by @rnd: no worktree diff endpoint exists (no `git diff` exec
anywhere in `internal/`).

**4. Scheduled launches.** A row like a source with a cron or interval field and a launch request instead of an inbox
item, with the same bounds and three strikes switch off. *Who ships it:* Superset "Automations" (README:129-135), Orca
automations (`v1.4.190`, `v1.4.198`, cron step fix), Mission Control schedules (README:82). *Why:* overnight triage and
recurring reports are the standing use of an unattended fleet. *Files:* `internal/store/sources.go`,
`internal/daemon/sources.go`, `internal/daemon/launch.go`, `internal/api/`. *Owner:* runtime. *Size:* medium. Checked by
@rnd: not filed (no backlog item mentions a scheduled or recurring launch, or cron), and a source only has
`interval_secs` for finding work (`internal/store/sources.go:29-35`).

**5. Self reported resume.** Let an agent report its own resume command, so a harness with no built in resume still
comes back after a restart. The pacing half is ALREADY BUILT: `reopenSaved` starts cards one at a time, `reopenGap`
400ms apart (`internal/daemon/reopen.go:40-43`, `:150`), so only the self reported command is proposed. *Who ships it:*
herdr
`v0.9.2` release notes ("Restored agents start one at a time, 100 ms apart", "Agents can report their own resume
command"). *Why:* a room with dozens of cards resuming together is a CPU and rate limit spike and herdr saw it in the
field. *Files:* `internal/daemon/launch.go`, `internal/store/harness.go`, and whichever file runs the wake after a
restart (not
found, see `docs/runtime/restart-wake.md`). *Owner:* runtime. *Size:* small. Related: item 38 (which cards resume) and
`docs/rnd/rolling-restart-design.md`.

**6. Search inside transcripts, across rooms.** Card-level search across rooms ALREADY EXISTS: `EverRun` matches
title, reason, worktree, tags, recap and external id (`internal/store/history.go:29-66`), and the hub merges
`/v1/history` over every room in order (`internal/link/fanout.go:363`, `internal/link/proxy.go:502`). What is missing
is searching what a session SAID and DID, its transcript. *Who ships
it:* Orca `v1.4.206` (session-search "every computer", `orca search`), Atlas (README:126). *Why:* "which agent touched
that config last week" is the question a multi machine operator asks and the answer is on another machine.
*Files:* `internal/store/history.go`, `internal/link/` (a fan out like `pinorder.go`), `internal/api/`. *Owner:* fabric.
*Size:* medium, since transcripts are on each room's disk and the fan out already exists.

**7. Every commit linked to the card that made it.** Record the head of the card's worktree at each stop, list the
commits since the base, and re-point on amend or rebase by patch id. *Who ships it:* Atlas checkpoints (README:71-77,
124-125). *Why:* review needs "which card wrote this line" and a report's single sha covers only the last commit.
*Files:* `internal/store/a2a.go`, `internal/store/ledger.go`, `internal/daemon/finish.go`, a new git helper.
*Owner:* review. *Size:* medium. A squash makes the link ambiguous and Atlas orphans it. Do the same.

**8. Dependencies between work items and a gate only the board resolves.** Already rank 6 in competitors.md and orca.md
item 1, kept here because it is unfiled and still absent from every other tool read except Orca and Gas Town. *Files:*
`internal/store/ledger.go`, `internal/store/schema.go` (end of slice), `internal/daemon/sweep.go`. *Owner:* runtime.
*Size:* medium.

**9. Notify the phone only when the desktop is away.** Orca `desktop-away-state.ts` (orca.md item 9) and Claude Code's
presence file (`docs/rnd/mobile-research.md`). *Files:* `internal/api/web/sw.js`, `internal/api/web/js/notify.js`.
*Owner:* ui. *Size:* small. Only matters once the phone page exists.

**10. Wake an offline room by a custom command.** Superset remote access "Wake offline hosts with a custom command"
(README:139). A room row gets an optional argv the hub runs when it is asked to reach a room that is not attached.
*Files:* `internal/link/`, `internal/store/dispatch.go`, `docs/fabric/`. *Owner:* fabric. *Size:* small. Niche until an
operator has a machine that sleeps.

### 4.2 Already filed, and the new evidence

| Feature | Backlog | New evidence |
| --- | --- | --- |
| A phone page and push | mobile-design, u-001, r-024 | Orca real push (`v1.4.199`), Superset iPhone, Paseo apps |
| Reviewing a PR on the board | u-005 | Orca and Superset send comments to agents (feature 2 above extends it) |
| One integration checkout and a merge queue | f-002, f-019 | Gas Town Refinery (unread), Orca stacked PRs, Agent Orchestrator "ready to merge" column |
| Cleanup of a card's worktree | r-006, r-019 | Orca `workspace-cleanup.ts` |
| Roles and packs | r-003, competitors.md rank 1 | Atlas packs, Paseo plugins, herdr marketplace |
| Live restart without stopping agents | f-011 | herdr `v0.9.0` "client updates can leave compatible servers untouched" |
| Preview of a worker's app | `docs/ui/preview-design.md` (not read) | Agent Orchestrator, Superset, cmux, Orca all ship a browser pane |
| Context meter, usage tab | 45, u-011, r-015 | Orca and oh-my-claudecode ship both |

## 5. Do not copy

- **A shared vector memory across agents** (Atlas, ruflo, claude-mem). It is a plugin category that sits inside the
  agent,
  the agent's own memory files already hold what atrium can safely read, and it would give the daemon an embedding
  model and a store that can leak one card's secrets into another's prompt. Atrium's handoff (item 66) is the bounded
  form.
- **Cloud sandboxes** (Omnigent lists a dozen providers). The overlay answer in `docs/fabric/overlays.md` already covers
  "another machine", and a sandbox provider list is an integration surface atrium would carry forever.
- **A model picked agent router and swarm memory** (ruflo `v3.43.0` to `v3.44.0`). Its own release notes say none of the
  options met the bar, so the benefit is not yet measured.
- **A login and accounts** (Mission Control, Superset, Omnigent organisations). Loopback and an overlay stays the rule
  (`CLAUDE.md`, "Authentication").
- **Nineteen or thirty two hook installers** (Orca, Agent Orchestrator). Add an adapter when a runner is added, as
  orca.md section 6 already said.
- **A hash bound, offline verifiable run journal** (open-multi-agent). Atrium's event log and decision record answer the
  operator's question, and a tamper evident chain is for a threat model this single operator tool does not have. Its
  one useful idea, an approval tied to what was shown, is a small check to make in the permission view.
- **Native editors and notebooks** (Orca, Atlas). The board is not an IDE, and "open in your editor" is enough.

## 6. Where atrium differs

Short, because competitors.md section 5 has it. None of the eighteen tools here gates every tool call with durable
rules while owning the terminal a human types into. The newer control planes (Mission Control, Omnigent,
open-multi-agent)
went to login, servers or libraries. Storage failure halting and not degrading has no counterpart in any README or
release note read. Windows first: Superset has no Windows build, herdr's live handoff is Unix only and Atlas lists
Linux as untested, while atrium's supervision is built for ConPTY. What atrium's peers all have that it does not is the
feedback loop from pull request and CI back to the agent, which is why feature 2 ranks second.

## 7. What could not be verified

- **Checked in code by @rnd, 2026-09-30, for the ranked items.** Item 2: nothing polls PR or check state
  (`recognisers.go` is inbound only). Item 3: no worktree diff anywhere in `internal/`. Item 4: no scheduled launch,
  and a source has only `interval_secs` for finding work. Item 5: pacing exists (`reopen.go:40-43`), so only the self
  reported resume command is proposed. Item 6: card-level search across rooms exists (`history.go`, `fanout.go:363`),
  so only transcript search is proposed. Items 1, 7, 8, 9 and 10 rest on the worker's docs reading and competitors.md.
- **The backlog, checked by @rnd.** No item mentions a freeze, estop, budget cap on tokens or calls, a scheduled or
  recurring launch, cron, PR or CI state on a card, or work item dependencies. The earlier 39-file "schedule" hit list
  was words in passing (keep-alive budgets, notification timing, idle parking).
- **The matrix is the ten survey tools plus eight entrants at README depth.** Gas Town's Refinery,
  Symphony's and humanlayer's recent releases, `munder-difflin`, `stagewise`, `happy` and `t3code` were not read.
  Blank cells mean not seen.
- **Claude Code's own features** (Agent View, Agent Teams, Remote Control, scheduled tasks) were not
  fetched. They may cover items 4 or 9 for Claude runners, and `docs/rnd/mobile-research.md` has the Remote Control
  page only.
- **No new source reads.** The clones in `D:/tmp` were not refreshed or opened. Citations in section 4 to README line
  numbers refer to the raw README fetched on 2026-09-30 and not committed, so they will drift. Release note claims are
  from bodies fetched on the same day. The Orca release bodies quoted (for example "`v1.4.190`") were matched by grep
  of feature lines and headings, not read in full, and the 54 stable releases since 2026-08-01 are far more than was
  read.
- **Adoption numbers** are GitHub stars from the API on 2026-09-30. Downloads and the badges some READMEs quote were not
  checked. Newer entrants' claims of production use (open-multi-agent lists three users at about 60 to 80 stars each)
  were not checked either.
- **Nothing was run**, and no tool listed here was installed or started.
