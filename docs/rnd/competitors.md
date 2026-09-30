# Competitors: the field around atrium, and whether atrium can be extended

Standing reference. Written 2026-09-29 by reading source where it says so and a README where it says that instead.
Nothing was executed. Eight tools were read at source depth, of which bb (`docs/rnd/bb.md`) and Charon (`docs/rnd/charon.md`)
have their own files. The rest are in section 2. Section 3 (extensibility) was re-traced through atrium's own code
on the same day and corrects the first version, which had been taken from docs.

Rules this file keeps, from `docs/rnd/charon.md`: every claim is a static read, every clone has a date and a commit, and
where a claim rests on a README, a comment or a design note and not on traced code, it says so.

## 1. How the field was picked

The first pass used `gh search repos` by keyword and returned prompt packs, so it was thrown away. This pass used four
independent sources and kept a tool only if it runs or supervises **several sessions of third party coding agents**
(Claude Code, Codex, Gemini CLI and the like) or gates what they do.

| Source | What it gave |
| --- | --- |
| GitHub topic search, `topic:claude-code`, `topic:coding-agents`, `topic:agent-orchestration`, star floor 300, sorted by stars | The top of the field by adoption. Most of the top hits were skills and prompt packs and were dropped by the criterion above |
| `andyrewlee/awesome-agent-orchestrators` (2,043 stars), 239 repositories in it, every star count looked up by the GitHub API | The ranking below. It is the only curated list found that is about orchestrators and not skills |
| `hesreallyhim/awesome-claude-code` (54,796 stars), the "Agent Orchestration", "Alternative Clients" and "Session Monitors" rows of its resource table | Cross-check. Everything popular there was already in the first two sources, plus small session monitors |
| Web search and three roundup articles (augmentcode, codeagentswarm, nimbalyst) | Cross-check, and the names of closed source tools (Conductor) that no star count can rank |

Stars are the GitHub API on 2026-09-29. Downloads were not compared, because most of these ship as a desktop app or
`npx` package with no common counter. Three of them do quote a badge (herdr and oh-my-claudecode a download badge,
ruflo a claimed "8.1M ecosystem downloads") and none of those was checked.

| Rank | Tool | Stars | Kind | Read |
| --- | --- | --- | --- | --- |
| 1 | `stablyai/orca` | 81,569 | Electron app. Real terminals plus SDK sessions, worktree per agent | Source (2.2) |
| 2 | `ruvnet/ruflo` | 73,492 | In-model swarm framework, headless `claude -p` workers | Source, shallow (2.9) |
| 3 | `herdrdev/herdr` | 41,412 | Rust terminal multiplexer that owns the ptys | Source (2.3) |
| 4 | `Yeachan-Heo/oh-my-claudecode` | 39,403 | Claude Code plugin plus tmux CLI workers | Source, partial (2.4) |
| 5 | `BloopAI/vibe-kanban` | 28,216 | Kanban and workspaces, headless `claude` over stdio | Source (2.5) |
| 6 | `manaflow-ai/cmux` | 27,499 | macOS terminal app with notifications for agents | Not read |
| 7 | `openai/symphony` | 27,473 | Spec plus Elixir reference, polls Linear, Codex app server | Spec and config (2.6) |
| 8 | `slopus/happy` | 23,946 | Phone and web client for Claude Code and Codex | Not read |
| 9 | `pingdotgg/t3code` | 23,882 | Agent GUI, no description on the repository | Not read |
| 10 | `getpaseo/paseo` | 18,994 | Desktop and mobile orchestration | Not read |
| 11 | `gastownhall/gastown` | 18,209 | Go, tmux, a Mayor agent, mail, merge queue | Source (2.7) |
| 12 | `superset-sh/superset` | 14,733 | Agentic IDE, worktree per agent | Not read |
| 13 | `Untrivial-ai/agent-orchestrator` | 12,522 | Supervise agent teams, desktop, web and cloud | Not read |
| 14 | `humanlayer/humanlayer` | 11,622 | Go daemon with approvals. Repository says its code is deprecated | Source (2.8) |
| 15 | `smtg-ai/claude-squad` | 8,547 | Go, tmux and worktrees | Source (2.10) |

Also above the floor and left out on purpose, each because a README says it is something else: `openclaw/openclaw`
(390,780) and `NousResearch/hermes-agent` (250,002) are personal assistants, `paperclipai/paperclip` (94,134) and
`multica-ai/multica` (51,672) manage agents "at work" and were not checked against the criterion, `OpenHands/OpenHands`
(89,511) is an agent and not a supervisor of others, `farion1231/cc-switch` (138,598) switches configuration between
tools. bb (3,990 stars) and Charon are ranked 54th and lower on this list and were covered because they were the brief.

Coverage rule: the top three by stars that met the criterion (Orca, ruflo, herdr) were cloned and read, and ruflo only
shallowly. Six more were read because their architecture is the nearest to atrium's (gastown, humanlayer, claude-squad)
or the furthest (Symphony, vibe-kanban) or because they rank next (oh-my-claudecode). That is nine tools beyond bb and
Charon, three of them (ruflo, oh-my-claudecode, gastown) less deeply than the others, and section 2 says which parts.

| Clone | Path | Commit | Date |
| --- | --- | --- | --- |
| Orca | `D:/tmp/orca` | `31012aeb` | 2026-09-29 |
| ruflo | `D:/tmp/ruflo` | `3c8141d` | 2026-09-29 |
| herdr | `D:/tmp/herdr` | `dbe3a23` | 2026-09-29 |
| oh-my-claudecode | `D:/tmp/oh-my-claudecode` | `9fd35ec` | 2026-09-29 |
| vibe-kanban | `D:/tmp/vibe-kanban` | `d5cbb53` | 2026-09-29 |
| symphony | `D:/tmp/symphony` | `be10a1b` | 2026-09-29 |
| gastown | `D:/tmp/gastown` | `649b832` | 2026-09-29 |
| humanlayer | `D:/tmp/humanlayer` | `99abe67` | 2026-09-29 |
| claude-squad | `D:/tmp/claude-squad` | `ce1ffb4` | 2026-09-29 |
| bb, Charon | `D:/tmp/bb`, `D:/tmp/charon` | in `docs/rnd/bb.md`, `docs/rnd/charon.md` | 2026-09-29, 2026-09-03 |

All shallow, none committed here. Every path in section 2 is relative to that tool's clone unless it starts with
`internal/`, which is this repository.

## 2. The tools, one by one

The architecture axis that matters: does the tool **drive an SDK or a headless process** (no terminal, no human typing
into the session), **own a pty** (a supervisor, atrium's shape), or **wrap tmux**? The tools split cleanly.

| Tool | Drives the agent by | Human types into the session | Permission gate |
| --- | --- | --- | --- |
| atrium | Owns a ConPTY per runner | Yes, the design centre | `PreToolUse` hook, durable rules, auto mode with review |
| bb, Charon | Claude Agent SDK client | No | `canUseTool`, in memory |
| Orca | Own pty per terminal, plus an SDK path ("structured sessions") | Yes in terminal mode | None in terminal mode (observes `PermissionRequest`) |
| herdr | Owns the ptys, hooks and screen reading for state | Yes | None found |
| vibe-kanban | `claude -p` with `--input-format=stream-json`, `--permission-prompt-tool=stdio` | No | Approval objects in memory, with a timeout |
| Symphony | Codex app server | No | Policy in the workflow file, `approval_policy: never` in the shipped one |
| Gas Town | tmux sessions | Yes, through tmux | Not read |
| humanlayer (hld) | Headless `claude` plus an MCP approval tool | No | Approvals in SQLite |
| claude-squad | tmux panes | Yes | None. "Auto yes" presses Enter when it sees a prompt |
| oh-my-claudecode | In-session plugin, plus tmux CLI workers | Yes | "Advisory", by its own header |
| ruflo | Headless `claude` processes | No | Not read |

### 2.1 bb and Charon

bb is an SDK client with thirty-nine plugins, read in `docs/rnd/bb.md`. Charon is an SDK client for rented VPSes, read in
`docs/rnd/charon.md`. Both put the permission surface on `canUseTool`, neither has durable matchable rules, and both say in
their own words that an approval card is not a security boundary.

| | atrium | bb |
| --- | --- | --- |
| Relationship to the agent | Supervises the real interactive `claude`, owns its terminal | Is the SDK client, no terminal |
| Extension runs | Out of process, as a bounded command | In the server process, as trusted code |
| Extension is shared by | Whole-machine export and import, no named subsets | A marketplace manifest, npm or git, with semver over tags |
| Human at a keyboard in the session | The design centre | Not a case |
| Permission memory | Durable standing rules, most specific wins | Session grants, in memory (2.11) |
| Platform | Windows native first, one Go binary | macOS and Linux, Windows only in WSL2 |

### 2.2 Orca: 81,569 stars, MIT, created 2026-03-17

The full read is docs/rnd/orca.md. Numbers refreshed 2026-09-29: 5,291 forks, 393 contributors, 965 releases, 273 of them in
the last 90 days, latest `v1.4.217`. The section below is from the earlier clone at `31012aeb`.

**What it is.** An Electron desktop app (`src/main`, `src/renderer`, `src/relay`, `src/cli`, `mobile/`, `cloud/`) that
runs Claude Code, Codex, OpenCode, Pi and about twenty more CLIs side by side, one git worktree each, with a phone
companion and SSH worktrees. Source is very large (the relay directory alone has hundreds of files, most of them
tests), so what follows is the traced parts and not the whole.

**Architecture.** Both kinds. Terminals are real ptys (`src/main/pty`, `src/relay/pty-handler.ts`) with a node-pty
binding and a detached relay daemon on remote machines. A second path drives the Claude Agent SDK as a "structured
session" (`src/main/claude/claude-structured-session-adapter.ts` imports the SDK control requests). State for the pty
path comes from hooks: Orca installs a managed hook script into each agent's own configuration for nineteen agents
(`src/main/agent-hooks/managed-agent-hook-registry.ts`), and a local hook server reads posts on a per-pty port and token
that Orca injects into the pty environment (`ORCA_AGENT_HOOK_PORT`, `ORCA_AGENT_HOOK_TOKEN`, seen in
`src/main/agent-hooks/server-claude-permission-visibility.test.ts`). `PermissionRequest` maps to a `waiting` state that
shows the tool and command on the card. It answers `204`. It is a status and not a gate.

**Orchestration, traced.** This is the part atrium does not have. A coordinator loop
(`src/main/runtime/orchestration/coordinator.ts`, 320 lines, plain code and not a model) drives phases `decomposing`,
`dispatching`, `monitoring`, `merging`, `done` over a task table with dependencies
(`TaskCreateParams.deps`, statuses `pending`, `ready`, `dispatched`, `completed`, `failed`, `blocked`,
`src/shared/rpc-contract/orchestration-params.ts`). Workers report by typed messages: `worker_done`, `heartbeat`,
`escalation`, `decision_gate`. A decision gate blocks its task, and the code says why it is never resolved by the
coordinator: "the coordinator never auto-resolves gates (humans do)... that would defeat them as approval checkpoints"
(`coordinator-decision-gates.ts`). Dispatch is fenced, and a stale consumer gets `consumer_fenced`.

**Mail delivery, traced.** A message for a worker is delivered as a short *pointer* typed plus Enter into the pty,
only when `isAgentSettledForDelivery` says the agent is idle, and a waiter blocked in `check --wait` pre-empts
delivery entirely (`mailbox-pointer-delivery-contract.ts`, `mailbox-pointer-eligibility.ts`). The full text is fetched
by the agent with a CLI call. This is close to what atrium does today (see 3.1, the peer bus).

**Other things worth knowing.** Trust dialogs are pre-empted by writing the same trust artefact the agent would write,
for cursor, copilot, codex, antigravity and qoder, with the reason and the verified CLI version in the comment
(`src/main/agent-trust-presets.ts`). Repo-level hooks come from an `orca.yaml` (`src/main/hooks.ts`) run with a
two minute timeout and a 10 MiB output cap, where a run cut off by timeout withholds the exit code so a removal gate
reads `unverifiable` and not a pass. The orchestration skill in `skills/orchestration/SKILL.md` is a discovery stub
that says it is deliberately not the guide: the version-matched guide is printed by the binary
(`orca skills get orchestration`) "so it can never drift from the binary that will actually run your commands".

**Overlap with atrium.** Worktree per agent, hooks for state, a peer and mail path, a CLI an agent calls, a phone
view, SSH. Orca's is a desktop IDE and atrium is a daemon and a board.

**Ideas to steal.**
1. *Dependencies between work items, and a gate a director cannot resolve.* Sketch: `work_item` (`internal/store/
   ledger.go`) gets a `blocked_by` list of item ids. A launch onto an item whose blockers are not `accepted` is held
   in the same held state a capped launch would be (idea 5 in section 4), and a `decision_gate` is a report kind whose
   answer only the board can give. Atrium's ledger already has the states (`open`, `reported`, `accepted`,
   `abandoned`, `superseded`). What is missing is the edge.
2. *Trust artefacts for codex, cursor and copilot.* Sketch: `internal/runnersetup` has adapters for claude and gemini
   (`claude.go`, `claudetrust.go`, `gemini.go`). Orca documents where the other three keep the marker and how to write
   it without losing other keys. New adapters are the same shape as the existing two.
3. *A guide served by the binary.* Sketch: the agent-facing skill or brief is a stub that says "run `atrium help
   agent`". Whether `internal/daemon/help.go` already serves a whole guide and not per verb text was not traced.

**Where atrium is ahead.** A durable, matchable permission gate that can refuse before a tool runs. Orca observes
`PermissionRequest` and answers `204`. Also: one Go binary and Windows first. Orca lists Windows as a platform in its README badge and
carries WSL specific relay files (`wsl-agent-hook-relay.ts`). How well that works was not tested. Standing rules with an audit trail, auto mode with a review, the halt.

**Not traced.** The SDK structured path end to end, the federation code (`orchestration/federation-*.ts`), the cloud
app, the mobile app, `stale-base` flagging in the coordinator, and whether the coordinator is used by default or only
when a user asks for it.

### 2.3 herdr: 41,412 stars, Apache 2.0, created 2026-03-27

**What it is.** "The runtime your coding agents live on." One Rust binary (171,615 lines under `src/`) that is a
terminal multiplexer: a background server owns the ptys, a client attaches, and closing the client does not stop the
work. Windows is described as beta on the project's own site (`README.md`, not verified).

**Architecture.** Own the pty. This is the nearest tool to atrium in shape and the only one found that is, and it was
built by people who reached the same conclusion: "herdr doesn't wrap or replace them, it owns their terminals"
(`README.md`). A socket API (`src/api/schema.rs`) has methods `agent.list`, `agent.start`, `agent.prompt`,
`agent.wait`, `agent.explain`, `agent.read`, `pane.send_keys`, `events.subscribe`, `events.wait` and more, so agents
drive it the way atrium's control tools drive atrium.

**State, traced.** Two authorities, and the conflict between them is the interesting part. Hooks report `working`,
`blocked`, `idle` (`src/integration/claude_settings.rs` wires `PermissionRequest` to `blocked`, `Stop` to `idle`, and
so on, into the agent's own `settings.json`, and can uninstall exactly what it added). Screen reading is the other,
and it is **data**: a TOML manifest per agent, twenty-two of them (`src/detect/manifests/claude.toml`, `codex.toml`,
`cursor.toml`, `gemini.toml`...), each a list of rules with `id`, `state`, `priority`, a `region` of the screen
(`bottom_non_empty_lines(12)`, `osc_title`, `after_last_horizontal_rule`), `contains`, `regex` and `not` gates. The
Claude manifest says why a rule is shaped as it is, for example that an activity line "renders at column zero, and
keeping that shape prevents user prompt text from impersonating this signal." A user can override a manifest
locally, and a catalog can be fetched from the vendor's site (`src/detect/manifest_update.rs`, capped at 256 KiB,
dotted numeric versions, a `min_engine_version`). `agent.explain` returns which rule matched and which were evaluated.

**Persistence, traced.** Layout is saved and restored, and resume commands are validated before being typed:
control characters, argument counts and byte limits are refused, and a comment says the executable must be a bare name
because shells disagree on quoting (`src/agent_resume.rs`). A "live handoff" swaps the server binary without killing
panes by passing file descriptors over `SCM_RIGHTS`, and the whole module is `#[cfg(unix)]`
(`src/server/handoff.rs`), so it does not exist on Windows.

**Extension, traced.** Plugins are out of process: a plugin is a pane running a command, installed from a git
repository, with a registry file guarded by a lock and symlink aware writes, a marketplace that "publishes their
versions and exact default-branch commits" (`CHANGELOG.md`), and `[[startup]]` hooks (`src/persist/plugin_registry.rs`,
`src/plugin_command.rs`). Only the registry and the changelog were read, not a plugin.

**Overlap with atrium.** Very high on the mechanism: pty ownership, detach and reattach, a socket API for agents to
drive, hooks written into the agent's settings, resume, notifications. Low on the model: herdr has panes, tabs and
workspaces and no cards, no durable rules, no review, no gate.

**Ideas to steal.**
1. *Screen detection as data.* Atrium's version is Go code for one runner: `internal/daemon/idleframe.go` matches the
   Claude prompt between two rules and the "esc to interrupt" footer, and `looksidle.go` turns that into an activity
   badge after 25 seconds of silence. A card on any other harness has no such badge unless hooks exist. Sketch: a
   harness row (`internal/store/harness.go`) gets an optional `detect` field naming a rules file in herdr's shape
   (`id`, `state`, `priority`, `region`, `contains`, `regex`, `not`). `looksidle.go` evaluates it on the screen the
   daemon already keeps (`screen.go`) and the result feeds the **activity badge only**, never a column, which is the
   rule `docs/runtime/activity-design.md` already states. Add an explain call so an operator can see the rule that fired.
   Do not copy the vendor fetched catalog: a daemon that fetches rules from a public host on start is a call out this
   project has ruled out elsewhere, and a local override directory gives the same benefit without it.
2. *Validate a resume command before it is typed.* Sketch: `ResumeArgs` (`internal/store/harness.go`) already
   substitutes `{resume}`. Add herdr's checks (no control characters, bounded count and bytes, bare executable name) at
   the point where the argv is built. Small.
3. *Plugins as a pane, installed by git and commit.* Same shape as the pack in section 3.4. Read as confirmation
   that an out of process, commit pinned plugin story works, not as new work.

**Where atrium is ahead.** A gate: herdr reports `blocked` and does not decide. Durable state (herdr restores layout
and can resume supported sessions, but says "the original processes do not survive"). A board with columns for human
attention, the work ledger, review, shelving, overlays. On Windows atrium's supervision works today and herdr's live
handoff does not exist.

**Not traced.** The plugin runtime, the remote machine code (`src/remote`), the ConPTY backend (the `pty/backend`
directory has no Windows match in a grep for `windows` or `conpty`, which was not followed up), and how hook and
screen authority are reconciled (`PaneClearAgentAuthority`).

### 2.4 oh-my-claudecode: 39,403 stars, MIT, created 2026-01-09

**What it is.** A Claude Code plugin (marketplace install, skills, agents, hooks) that adds "teams" of agents, and a
CLI (`omc team N:codex "..."`) that launches real `claude`, `codex`, `gemini`, `agy`, `grok` or `cursor-agent` panes in
tmux workers that "die when their task completes" (`README.md`). The README distinguishes the two: `/team` is the
in-session native workflow and `omc team` is tmux.

**Architecture.** Two. Inside a session it is prompts, skills and hooks. Outside it is a tmux wrapper with a
mailbox and a monitor (`src/team/`, about sixty files: `dispatch-queue.ts`, `mailbox-outstanding.ts`, `heartbeat.ts`,
`idle-nudge.ts`, `merge-coordinator.ts`, `recovery-saga.ts`, `governance.ts`). Only the headers of `permissions.ts`,
`governance.ts`, `sentinel-gate.ts` and the factcheck hook were read.

**What is honest in it.** `src/team/permissions.ts` opens with: "This is an advisory layer only. MCP workers run in
full-auto mode and cannot be mechanically restricted. Permissions are injected into prompts as instructions for the
LLM to follow." Its glob matcher is written character by character "to avoid ReDoS risk". `governance.ts` sets a
default worker cap of twenty with a hard ceiling that a lower configured value wins over (`resolveMaxWorkers`).

**Overlap with atrium.** A director and workers, a mailbox, a cap, a merge step. Nothing on a permission gate.

**Ideas to steal.** One, and it is already in atrium: `src/hooks/factcheck` validates a worker's claims payload, but
the read found only shape checks and path existence, with no `git` call anywhere in the directory. Atrium already does
the stronger thing: `SetReportSHA` (`internal/store/a2a.go`) records the commit a `done` report names and whether it
was found in the card's worktree, and `report_unverified` is stored. Nothing to take.

**Where atrium is ahead.** A real gate where OMC says its own permissions are advice. A real terminal a human owns.
Durable state.

**Not traced.** Almost all of `src/team`, the `dist/` bundle, and the fifty other directories under `src/`.

### 2.5 vibe-kanban: 28,216 stars, Apache 2.0, created 2025-06-14

**What it is.** A kanban board, a Rust server (`crates/`: `server`, `services`, `executors`, `db`, `mcp`, `git`,
`workspace-manager`, twenty-nine crates in all) and a web and Tauri front end. An issue becomes a workspace (a branch, a
terminal, a dev server), an agent runs in it, you review the diff and open a PR. **The README's first heading is
"Vibe Kanban is sunsetting"** with a link to an announcement. Last push 2026-09-19. It is a finished lesson, not a
growing competitor.

**Architecture.** Headless process with stdio control, not an SDK library and not a pty. Claude is started as `claude
-p` with `--output-format=stream-json`, `--input-format=stream-json` and `--permission-prompt-tool=stdio`
(`crates/executors/src/executors/claude.rs:168-191`), and a `control_request` of subtype `can_use_tool` is answered
over the same pipe. Nine other executors (`codex.rs`, `gemini.rs`, `cursor`, `opencode`, `amp`, `copilot`, `droid`,
`qwen`, and `acp/`) follow their own protocols.

**Approvals, traced.** `crates/services/src/services/approvals.rs`: pending approvals in a `DashMap`, a `timeout_at`,
a shared future that resolves to `TimedOut` if nobody answers, `is_question` for the question form of a request, an
`AlreadyCompleted` refusal, and a broadcast patch to the front end. Nothing is persisted, so a restart loses every
pending approval. A follow-up message queue exists (`queued_message.rs`) with one message per session, replaced by
a newer one, in memory. The Claude control request carries `permission_suggestions` with an `addRules` body naming
the exact rule prefix the CLI would offer ("Always allow `./gradlew :web:testApi:`", in a test fixture at
`claude.rs:3280`).

**Extension, traced by listing.** A stdio MCP server in `crates/mcp` exposes workspaces, sessions, repos, issues, tags
and relationships as tools. That is the agent facing surface, and it is broad on issues and narrow on control.

**Overlap with atrium.** A board of cards over worktrees, MCP tools for agents, an approval surface, diffs and PRs.

**Ideas to steal.**
1. *Offer the rule the agent's own CLI would offer.* Sketch: when the board asks a human about a `Bash` request,
   the Approve control has a second button "Approve and always allow `<prefix>`" that writes a standing rule
   (`internal/store/rules.go`, prefix, glob or folder) for exactly that prefix. Whether Claude's `PreToolUse` hook
   input carries suggestions was not traced. If not, the daemon proposes a prefix from the command itself. Small.
   Check the board first, it may already do this (`docs/runtime/auto-mode.md`).
2. *A question form of approval.* Not new for atrium (`internal/store/ask.go` is a question table). Not a steal.

**Where atrium is ahead.** Durable approvals and rules, and a gate that survives a restart. A real terminal. The
product is also alive.

**Not traced.** The remote and relay crates (about ten of the twenty-nine), the Tauri app, review and the PR monitor.

### 2.6 Symphony: 27,473 stars, Apache 2.0, created 2026-02-26

**What it is.** OpenAI's "low-key engineering preview": a `SPEC.md` (about 1,800 lines) that any coding agent is meant
to implement, plus an Elixir reference (`elixir/`). It polls a Linear board, makes an isolated workspace per issue,
runs a Codex app server in it, and lands the PR. The README says to test it in trusted environments.

**Architecture.** Neither a pty nor tmux. The reference runs `codex ... app-server` and speaks its protocol. No human
types into a session. Read: `README.md`, `elixir/WORKFLOW.md`, and sections 5 to 8, 10.5, 14 and 15 of the spec by
heading and in part.

**What is worth reading.** The contract is one file in the repository, `WORKFLOW.md`, YAML front matter (tracker,
polling, workspace root, hooks `after_create` and `before_remove`, `agent.max_concurrent_agents`, `max_turns`, the
Codex command and sandbox policy) and a prompt template below it. Spec 6.2 requires dynamic reload: "Invalid reloads
MUST NOT crash the service. Keep operating with the last known good effective configuration and emit an
operator-visible error", and 6.3 re-validates before every dispatch tick, skipping dispatch and keeping reconciliation
running if validation fails. Spec 10.5: "Approval requests and user-input-required events MUST NOT leave a run stalled
indefinitely", with a documented choice among satisfy, surface, auto-resolve or fail. Spec 10.5 also says the runtime
"MUST NOT require the coding-agent child process to read raw tracker tokens from disk or environment".

**Overlap with atrium.** Sources (poll a tracker), workspaces and hooks, a concurrency cap, retry with backoff, a
run reconciliation loop. Atrium's sources and recognisers cover the intake half in a different way.

**Ideas to steal.**
1. *A repository-held contract with last known good reload.* Sketch: this is where a role file (`docs/backlog-2.md`
   r-003) lives and how it behaves. A `.atrium/roles/*.md` (front matter plus brief) read when a card launches into
   that directory, cached with its content hash, and re-read on the next launch. An unparsable edit keeps the previous
   version and posts a notice on the card and on the board. No restart, no watcher needed. Medium.
2. *Preflight validation before each dispatch.* Sketch: a source-queued launch (`internal/daemon/sources.go`) checks
   that its harness row still resolves and its directory exists before spawning, and a failure is a row error and not a
   spawn. Small, and part of what `runnersetup` already does.

**Where atrium is ahead.** An operator in the loop. Symphony's own example sets `approval_policy: never` and the
sandbox to workspace write, on purpose, because "engineers do not need to supervise Codex". Atrium is the tool for the
case where they do.

**Not traced.** The Elixir code, the retry and reconciliation algorithms in section 8, and how the spec's optional HTTP
extension compares to the board.

### 2.7 Gas Town: 18,209 stars, MIT, created 2025-12-16

**What it is.** A Go workspace manager (`gt`) with its own vocabulary: a **Mayor** (a Claude Code instance you talk to),
**rigs** (a repository plus its agents), **polecats** (workers with a persistent identity and ephemeral sessions),
**hooks** (git worktree backed persistent work), **convoys**, **molecules** (workflow templates from TOML "formulas"), a
three tier watchdog (**Witness**, **Deacon**, **Dogs**), a **Refinery** (a Bors style merge queue per rig), a
**scheduler**, an **estop**, and a **seance** command that lets an agent question a predecessor's session from its
event log. Work state lives in "Beads", a git backed issue store. Twenty to thirty agents is the stated scale.

**Architecture.** tmux, driven from Go (`internal/tmux`, with `_windows.go` files for process groups and flock),
with health checks on a three minute daemon heartbeat. Agents are Claude, Copilot, Codex, Gemini and others.

**Convergence, and it is the most useful finding in this file.** `internal/nudge/queue.go` says: instead of sending
text to a tmux session, "which cancels in-flight tool calls", messages "are written to a queue directory and picked up
by the agent's UserPromptSubmit hook at the next natural turn boundary". That is the design atrium's peer bus started
with (`internal/store/messages.go`: queued, delivered by the permission hook or the `Stop` hook). It is the design
`internal/daemon/peers.go` then *reversed* for a terminal atrium owns (section 3.1). Gas Town keeps it because with
tmux it has no record of what a human has typed. Its queue also has things atrium's does not:

- a time to live per priority (30 minutes normal, 2 hours urgent, `DefaultNormalTTL`, `DefaultUrgentTTL`),
- a depth of 50 per session (`MaxQueueDepth`),
- a `DeliverAfter` time, so a nudge can be held without being discarded,
- stale claim recovery, so a drainer that crashed mid-claim does not orphan a message (`staleClaimThreshold`).

`internal/store/messages.go` has none of those four. A queued message for a card that never comes back waits forever,
and the row count for one card is not bounded there (the sender is rate limited, 20 a minute, in `peers.go`).

**E-stop, traced.** A sentinel file `ESTOP` in the town root, with `manual` or `auto` triggers and a reason.
"When present, all agents should be frozen (SIGTSTP) and the daemon should not restart them. The Mayor is exempt from
E-stop so it can coordinate recovery." Per rig, `ESTOP.<rig>`. An auto stop can be cleared by the daemon, a manual one
only by a person (`internal/estop/estop.go`).

**Scheduler, traced by design doc.** `docs/design/scheduler.md`: with `scheduler.max_polecats = N` a dispatch is
deferred into a "sling context" and the daemon starts them under the cap, batch size and a pause switch. With it unset
dispatch is direct. The doc says the reason is API rate limits and memory.

**Plugins.** `docs/design/plugin-system.md` is headed "Design proposal, not yet implemented", and a `plugin.md` with
TOML front matter and a gate (`cooldown`) is the format proposed. `internal/plugin` has a scanner and a recorder. Not
traced beyond that.

**Overlap with atrium.** Director and workers (Mayor and polecats), a mailbox, escalation, a merge step, a cap,
worktrees. This is the closest orchestration model to `runtime/DIRECTOR.md`.

**Ideas to steal.**
1. *A board wide freeze.* Sketch: a setting `board_frozen` holding the reason and who set it. It is a new step in the
   permission chain, after a queued message and after a shelved card and **before** standing rules, because a
   freeze that a rule can override is not one. It answers `block` with "atrium is paused: `<reason>`, nothing can run
   until a person resumes". `atrium_launch` and the board's launch refuse while it is set. Exempt: a card tagged
   `atrium:director`, so the director can coordinate recovery, which is Gas Town's Mayor rule. Trigger: manual from the
   board, and an automatic one from the usage numbers `internal/store/usage.go` already keeps. Small. What it does not
   do: stop a tool already running, and Windows has no `SIGTSTP` to freeze a process, so the gate stops the **next**
   call, not the current one.
2. *Bound, expire and defer the message queue.* Sketch: add `expires_at` and `deliver_after` columns at the end of the
   migration slice, drop expired rows in `PendingMessages`, refuse a queue past a depth per card with the depth in
   the error. The stale claim part is not needed, atrium marks delivery in one statement.
3. *Hold a launch instead of refusing it.* Atrium's cap refuses (section 3.1). Gas Town holds and dispatches under the
   cap. Sketch in section 4, rank 5.

**Where atrium is ahead.** A real board and a real permission gate. Gas Town's model is "agent decides, Go transports"
(`plugin-system.md`, "ZFC"), so a great deal of policy lives in prompts. Atrium enforces what it can in code.
Standing rules, auto mode with review, durable state in one SQLite file (Gas Town's is git and a Dolt server).

**Not traced.** Refinery, Witness, Deacon, molecules, Wasteland federation, the quota rotation code (`internal/quota`),
and what a tmux session under Windows looks like in practice. The `tmux` package has Windows files and no claim about
Windows support was checked.

### 2.8 humanlayer (hld): 11,622 stars, created 2024-08-05, license NOASSERTION

**What it is.** The repository README says: "the code here is pretty much all deprecated". What is in the tree is a
Go daemon, `hld/`, that manages Claude Code sessions, approvals and an event stream over REST, SSE and JSON-RPC on
`127.0.0.1:7777` (the same port and bind as atrium's agent listener by coincidence), backed by SQLite (`hld/store/
sqlite.go`: `sessions`, `conversation_events`, `approvals`, `file_snapshots`, `user_settings`, `mcp_servers`,
`raw_events`, `schema_version`). A Tauri UI is in `humanlayer-wui`.

**Architecture.** Headless `claude` (`claudecode-go` wraps it) with an MCP server hld runs, so a tool call becomes an
approval row (`hld/approval/manager.go`, `hld/mcp/server.go`). `CreateApprovalWithToolUseID` keys an approval to the
runner's tool use id. Last commit 2026-06-18.

**Overlap with atrium.** Nearly total on the daemon idea: a local daemon, a store, approvals, SSE. It is the project
atrium would have become had it stayed with headless sessions.

**Ideas to steal.**
1. *A time boxed skip-permissions.* Already in atrium: `AutoUntil` and `AutoExpired` (`internal/daemon/daemon.go:748`),
   cleared lazily at the one moment it matters. hld does it with a 30 second monitor and an event. Atrium's is the
   better shape. Nothing to take.
2. *File snapshots.* hld keeps `file_snapshots` rows, which would let the board show a diff of what a session changed
   between two tool calls without git. Only the table's existence was read, not what fills it. Not proposed.

**Where atrium is ahead.** Alive, and a terminal a human types into. hld's permission path needs the MCP tool, so a
runner that does not use it is not gated. Atrium's is a hook on every tool call.

**Not traced.** The whole session manager, the sqlite migrations, and the WUI.

### 2.9 ruflo: 73,492 stars, MIT, created 2025-06-02

**What it is.** Formerly claude-flow. "Multi-player swarms" for Claude Code, Codex and others: an npm CLI and MCP
server (`v3/@claude-flow/cli`), a plugin, memory (`agentdb.rvf`), a worker daemon, and a large amount of documentation
and generated data. `README.md` claims "8.1M+ ecosystem downloads" and "106k git clones (14d)". Neither claim was
checked, and the number of files (5,956 at this commit) is a warning about how much was read.

**Architecture.** Not a session supervisor. It is coordination inside the model's own context plus background
workers that spawn `claude` headless with a process pool, a timeout and event emission
(`v3/@claude-flow/cli/src/services/headless-worker-executor.ts`: "invoke Claude Code in headless mode with
configurable sandbox profiles"). Only that file's header and imports were read, and that file's imports show a
**global AI budget** and an **AI job dedup registry** keyed by a hash of the worker configuration
(`global-ai-budget.js`, `ai-job-dedup.js`).

**Overlap with atrium.** Very little. Ruflo runs work for you. Atrium shows you work and gates it.

**Ideas to steal.** A global budget for spend across background jobs. Atrium has usage data
(`internal/store/usage.go`, `session_usage`) and a cache keepalive with its own suspension rule, but nothing that stops
launches when spend crosses a line. That is the automatic trigger for the freeze in 2.7, not a separate feature.

**Where atrium is ahead.** Everything a supervisor does. Ruflo is a different category, and its rank on this list is
the strongest evidence in this file that adoption follows "make the agent smarter" and not "watch the agent".

**Not traced.** Nearly everything. This is a README plus one file.

### 2.10 claude-squad: 8,547 stars, AGPL-3.0, created 2025-03-09

**What it is.** A Go terminal UI over tmux and git worktrees (`session/tmux`, `session/git`, `daemon`, `ui`, `web`).

**Architecture.** A tmux wrapper. It polls `capture-pane`, hashes the content to see if it changed, and looks for
a prompt string per program: for Claude, "No, and tell Claude what to do differently", for aider "(Y)es/(N)o/(D)on't
ask again", for gemini "Yes, allow once" (`session/tmux/tmux.go:246-267`). "Auto yes" is a daemon that, for every
session, presses Enter when that string is on screen (`daemon/daemon.go`). A trust prompt string is dismissed the same
way (`tmux.go:159`).

Read of the daemon loop: it marks every stored session `AutoYes` when it starts and does not check a rule, a command
or a deadline before pressing Enter.

**Overlap with atrium.** Worktree per session, a list of sessions, attach. Almost nothing on permissions.

**Ideas to steal.** None. It is the baseline atrium's gate is measured against.

**Where atrium is ahead.** Auto approval that is recorded, reviewable, bounded by a deadline and beaten by a standing
never rule, against a daemon that presses Enter. A screen string is the whole decision and it is per program, so a
prompt that changed wording is a hang, and one that matched too much is an approval nobody chose.

**Not traced.** The UI, the web directory, storage.

### 2.11 What the read tools agree on

Section 6 covers the earlier pair. Adding this pass: **the tools that own a pty (Orca in terminal mode, herdr, atrium)
have converged on hooks written into each agent's own settings for state, and on a socket or CLI API an agent uses to
drive the tool**. The tools that gate approvals (bb, Charon, vibe-kanban, humanlayer) all do it through an SDK,
stdio or MCP channel they own, and none of them holds a durable rule store. Atrium is the only one found that owns the
terminal **and** gates every tool call.

Where this pass contradicts section 6 of the first version: it said neither bb nor Charon had a durable rule store and
that was true. It also said SDK client architecture was the majority shape. At the top of the adoption list, the
pty-owning tools (Orca, herdr) outrank them by a wide margin, and only one of those two, Orca, also ships an SDK path.
The bb finding stands for bb and Charon. It is not the shape of the field.

## 3. Extensibility: is atrium in a good position?

The question, as clint put it: "atrium would be powerful if people are able to customize it to do things they want,
like the review panel and the director of review. Are we in a good position to support that sort of extension?"

**Short answer: better than the first version of this file said, and the gap is narrower. Atrium already has a good set
of small extension points, a whole-machine export with a dry-run import, and a launch request that is nearly a role.
It has no way to name, subset, version, attribute or fetch a set of them. A user can build a director by hand and
cannot yet hand it to anyone else.**

### 3.1 What atrium has today, re-traced in code

The first version of this section came from `CLAUDE.md` and design docs. Every row below was read in `internal/`
on 2026-09-29. Corrections to that version are marked **Changed**.

| Extension point | What a user can put in it | Code |
| --- | --- | --- |
| **Runners / harness rows** | Command, args, env, cwd, `bin_path`, launch mode, and per runner argument shapes for resume, prompt, model, effort, each refused for a runner that has none (`{resume}`, `{prompt}`, `{model}`, `{effort}`). Never a list of models | `internal/store/harness.go`, `internal/daemon/launch.go` |
| **Runner setup adapters** | A step run before a launch, currently claude trust and gemini trust. Never fails a launch | `internal/runnersetup/` (`claude.go`, `claudetrust.go`, `gemini.go`), `internal/daemon/runnersetup.go` |
| **Sources** | Argv and interval, floor 30 seconds, switched off after 3 consecutive failures, no field for a credential | `internal/store/sources.go`, `internal/daemon/sources.go` |
| **Recognisers** (**new to this file**) | A row of Go regex with named groups plus templates for kind, title, tags, cwd, prompt, branch, window and theme, and an optional `fetch` argv. A URL becomes a filled-in launch dialog. No built-in rows | `internal/store/recognisers.go`, `internal/api/recognisers.go` |
| **Providers** (**new**) | A declared root with layout `root/org/repo`, discovered under bounds (4,000 directories, 30 second deadline), adds only and never deletes | `internal/store/providers.go`, `internal/api/providers.go` |
| **Actions** | A named prompt, `After` keep or exit, filtered by a tag and by a runner | `internal/store/actions.go` |
| **Fixtures** | Terminals that come up with the daemon | `internal/store/fixtures.go` |
| **Hooks** | Ten Claude events wired by a table: `SessionStart`, `SessionEnd`, `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `SubagentStart`, `SubagentStop`, `Notification`, `PreCompact`, `Stop`. Claude only. A `codex_test.go` sits in `claudeconf`, so some codex handling exists and was not traced | `internal/claudeconf/hooks.go`, `internal/api/hooks.go` |
| **Control MCP tools** | Eleven: `atrium_status`, `restart_atrium`, `atrium_peers`, `atrium_say`, `atrium_report`, `atrium_launch`, `atrium_task`, `atrium_exit` in `internal/cli/`, and `atrium_alias`, `atrium_cull`, `atrium_wake_after_restart` in `internal/link/control_mcp.go` | as listed |
| **Peer bus** | `tell`, `ask --peer`, `answer`. **Changed**, see below | `internal/daemon/peers.go` |
| **Tags** | Free labels. **Changed**: tags do drive behaviour in code, see below | `internal/store`, `internal/daemon` |
| **The launch request** | Harness, cwd, title, prompt, **brief** (written to `BRIEF.md` in the directory before the runner starts), model, effort, args, env, tags, window, theme, lean and MCP list, `if_running` (skip, adopt), `spawned_by` lineage, repo, org, host, branch, source and url | `LaunchRequest` in `internal/daemon/launch.go:21` |
| **Whole-machine export and import** | **Changed**, see below | `internal/daemon/export.go`, `import.go`, `internal/api/export.go` |
| **Events out** | An `EventSink` interface: hot database sink plus a cold write-only file sink of one JSON line per event, chosen by the `event_sink` setting, best effort, drops and counts under pressure. Also SSE. Compile time only, nothing user supplied | `internal/store/eventsink.go`, `filesink.go` |
| **Terminal themes** | A palette somebody brought (52 ship, imports from Windows Terminal), colours only, validated at the store so "a colour cannot become code" | `internal/store/termtheme.go` |
| **Skins** | **Not an extension point.** A fixed list of 22 names in `internal/api/skins.go`, duplicated from the stylesheet and checked by `scripts/check-skins.sh`. Unknown names are refused on the way in | `internal/api/skins.go` |
| **Grouping expressions** | The one place a user writes code: two functions the browser compiles with `new Function`. They live in `localStorage` only, and the settings API refuses `group_by` and `group_order` because storing them daemon side "is a stored XSS with a friendly name" | `internal/api/settings.go:285` |
| **Overlays** | How the board is reached from elsewhere | `internal/daemon/overlay*.go` |
| **Permission rules and auto mode** | Standing rules, and auto mode with a deadline (`AutoUntil`) or board wide. Order confirmed: replay, message, shelved, rule, auto, ask | `internal/daemon/daemon.go:620-776` |
| **The work ledger** | Items with states `open`, `reported`, `reopened`, `ended-without-report`, `accepted`, `abandoned`, `superseded`. A `done` report's commit is checked against the worktree | `internal/store/ledger.go`, `a2a.go` |
| **Rooms and dispatch** | A hub queues a launch for a named room. The hub never dials. A claim is a conditional update, a lease, a hand-out cap of 3 | `internal/store/dispatch.go`, `internal/daemon/rooms.go` |
| **The director pattern** | A brief, tags and a script. Not a thing atrium knows about (r-003 in `docs/backlog-2.md`) | `runtime/DIRECTOR.md` |

**Changed: the peer bus is typed, not only queued.** `CLAUDE.md` says a peer message is "QUEUED, never typed". The
code says the opposite, in its own header: a peer message "IS TYPED INTO THE TERMINAL WHEN THE TERMINAL IS FREE, and
queued when it is not", reversed on the operator's instruction, made safe by `runner.howBusy`, which reads a record of
every keystroke atrium has seen so a half written line is never typed into (`internal/daemon/peers.go:34-76`,
`deliverPeer` at `:443`). The file adds that `CLAUDE.md` "is a symlink into another repository and is not ours to
edit, so it disagrees with this file". Limits: 8,000 characters and 20 sends a minute per sender. Read the peer bus
comparisons in `docs/rnd/charon.md` and in 2.7 with that in mind.

**Changed: tags drive behaviour.** `atrium:lean` changes how a launch is built (`internal/daemon/lean.go:46`),
`atrium:subagent` is what `atrium_cull` acts on and what the launch cap counts (`internal/daemon/cull.go:54`,
`internal/link/control_mcp.go:983`), `origin:agent` marks a launched card and changes launch behaviour
(`a2a.go:99`, `launch.go:908`), and an action's `Tag` decides which cards are offered it. `dept:<name>` is convention
only, no code reads it.

**Changed: a launch cap exists, and it is narrower than it sounds.** `DefaultLaunchCap = 10`, overridden by
`ATRIUM_LAUNCH_CAP`, counting live supervised cards tagged `atrium:subagent` across every room, with a 60 second
reservation so two launches cannot both read the same count (`internal/link/control_mcp.go:998-1089`). It refuses and
does not hold. It lives in the control MCP server, so it applies to `atrium_launch` calls, and a grep of
`internal/daemon` for a cap found none, so a launch from the board or the raw HTTP API is not counted against it (not
traced further). The director's "two workers" is a policy in prose above a hard cap of ten.

**Changed: the outbound configuration is built, and the inbound half is partial.** `docs/runtime/scm-design.md` says nothing
is built. `internal/daemon/export.go` writes settings, harnesses, fixtures, sources, actions, rules, recognisers,
overlay options and brought themes into one versioned document. It copies fields across by name into types declared in
that file, so a field added later is absent until somebody adds a line, and it then scans the finished document for
credential shapes and refuses the whole export on a hit. `import.go` is a dry run by default and reports each thing as
`add`, `replace` or `keep`, never overwrites without `force`, refuses a version it does not know, and merges overlay
options without touching the stored account token. It is on the board's HTTP API (`?apply=1&force=1`) and no CLI
command was found. **Read in code: import applies settings, harnesses, sources, recognisers, themes and overlays. It
has no branch for fixtures, actions or rules**, although its own `Kind` comment lists them. So a role that carries an
action, or a pack that carries a standing rule, cannot be imported today. That is a safe default and a limit.

The design principle underneath is unchanged and worth stating because it is the opposite of bb's: **nothing a user
writes shares an address space with the permission gate.** A source is a command with bounded output. A hook posts and
forgets. An MCP tool goes through the same HTTP surface a human would. A failing extension parks or switches itself
off with the reason on its row. The one piece of user code, the grouping expression, is kept in the browser that typed
it, with a reason written into the code (`settings.go:285`) for why it will not move.

The director is still the telling example. `runtime/DIRECTOR.md` is a page of prose, and the launch request above
holds almost every field a role needs, one at a time. A **role is an assembly job**: a stored launch request, its tags,
a brief file, a cap per role, and who it reports to. Very little is new machinery.

### 3.2 What atrium lacks

Every item is a gap between "a user can do this by hand" and "a user can define and share this". Items marked
**Revised** changed after the code was read.

1. **A named, versioned bundle. Revised.** Whole-machine export and a dry-run import exist (3.1). What does not
   exist is a *subset with a name*: "these harness rows, this source, these actions, this brief are `review-panel`
   1.2". Import also skips three of the kinds that a role needs.
2. **A role as data. Revised.** Not a new schema so much as a stored `LaunchRequest` plus the parts `docs/backlog-2.md`
   r-003 names (reports-to, cap per role, resident or on demand).
3. **An admission point a user can own. Revised.** A hard cap of ten exists on the `atrium_launch` path. "Hold anything
   tagged `dept:review` until the panel has three verdicts" and "reject a launch without a brief" have no home, and the
   permission chain is about tools.
4. **A way to be told when things happen. Revised.** Events already leave atrium as SSE and as a JSON lines file that
   a process can follow. Nothing *calls a user's command* on an event.
5. **A trust story for someone else's extension. Revised.** Import never applies rules (no branch for them), which
   answers the worst case by omission. It does not answer hooks, runner args or a source's argv in a file a stranger
   sent, and `replace` under `force` overwrites a harness row whole.
6. **A place to test one.** No fake daemon was found. The daemon has many in-process tests
   (`internal/daemon/*_test.go`), and none is published for an author. Not searched further.
7. **A source of extensions.** No index, no install command, no version resolution. A `git clone` and an import is the
   whole path.
8. **Provenance.** No row records where it came from. Export and import do not tag rows, so removing what a pack added
   cannot be exact.

### 3.3 How the tools that were read compare

| | bb | Charon | herdr | atrium |
| --- | --- | --- | --- | --- |
| Unit of extension | Plugin: a `package.json` `bb` key and a `server.ts` | None found | A plugin is a pane running a command, installed from git | Loose rows, one export file |
| Runs where | In the server process | Not extensible | Out of process, in a pane | Out of process, as a bounded command |
| A veto point on work | `message.dispatch`, fail closed, ten second box | None found | None found | The permission chain (tools), a launch cap (count) |
| Roles / templates | Task presets, workflow scripts | Not found | Not found | A launch request, a brief file |
| Sharing | Marketplace manifest, npm or git, semver over tags | None | Marketplace with commits published | Export and import of one machine |
| Trust | Reviewed at listing, then trusted, in process | Single user | Not read | Out of process by construction, import dry run |
| Detection rules as data | No | No | Yes, 22 TOML manifests, overridable | No, Go code for one runner |
| Test kit | Fake host, published | Internal | Not read | None for authors |

The row that matters most is still the veto point. It is what would let a user *enforce* a director's rules and not
merely write them down, and bb's fencing of it (a question and not an event, fail closed, time boxed, human override)
is the reusable part. The row that matters second, new in this pass, is detection rules as data.

### 3.4 Recommendation: the smallest next step

**Do not build a plugin API.** bb's is 5,500 lines of contract for the backend and frontend together, still marked
experimental in most of its interesting members, and its cost is mostly the in-process trust that atrium has ruled out.
herdr reached a working plugin story with panes and git, and that is the shape to copy.

**Build a role first, and let a pack be a role plus its neighbours.** `docs/backlog-2.md` r-003 asks for a role, and
that is right. A role is one file, and a pack is a directory of them with the rows they need.

```
review-panel/
  atrium-pack.json      name, version, description, requires.atrium, and the parts below
  roles/review.md       front matter (harness, model, effort, tags, lean, mcp, cwd recipe, reports_to, cap) + brief
  actions/              named prompts, one file each
  sources/              source definitions: command, interval, bounds
  runners/              harness rows
  detect/               screen detection rules per runner (idea 2 in section 4)
```

`atrium pack install <path-or-git-url>[@ref]` reads the manifest and **shows a diff of what it would add**. That is
the half of the work that already exists: `ApplyImport`'s add, replace and keep report, the version refusal and the
secret scan. What has to be added, and why it is a smaller job than the first version of this file guessed:

- import branches for fixtures, actions and rules (with rules **off by default** in a pack and needing an explicit
  line in the diff),
- an `origin` column on each imported row, at the END of the migration slice, holding the pack name, version and
  resolved commit, so removal is exact and a later update that finds a different commit behind the same ref refuses
  (bb's moved tag lesson),
- a subset selector, so an import can apply one pack's part of a document,
- a CLI verb, since import is HTTP only.

An operator's own edit to an imported row is never overwritten. Every row keeps observed versus overrides, which is a
rule `CLAUDE.md` already states for machines and applies here to packs.

**Second, after packs: a gate.** The `message.dispatch` idea, as an out-of-process command with the same bounds and
switch-off rule as a source (`docs/rnd/bb.md` section 5, item 1). That is what turns a director's written limits into
enforced ones. The launch cap is its first, blunt instance.

**Third, if asked for: an event-called command.** A source is a command on a timer. The same row with a trigger on an
event instead of an interval reuses the whole runner. The event sink gives it its input.

Cost is a guess from the shape of the code, and is smaller than the first estimate because the diff and refusal
machinery exist: the pack manifest and subset import about a week, the gate about the same again. Not an estimate from
tracing the work.

## 4. Ideas worth stealing, ranked

Across everything read: bb, Charon (its list in `docs/rnd/charon.md` section 5 is not repeated), and the tools in
section 2. One list. Cost is a guess from the shape of the code and is labelled as one. Items removed from the earlier
list or from a draft of this one because atrium already has them are named after the table.

| Rank | Idea | From | Cost | What it needs |
| --- | --- | --- | --- | --- |
| 1 | **Packs and roles**: a named, versioned directory of atrium's own rows and a role file, imported by the existing dry-run diff, with provenance on every row | bb manifest and moved-tag refusal, herdr git plugins, Symphony `WORKFLOW.md` | Medium | Import branches for fixtures, actions and rules, an `origin` column at the END of the migration slice, a subset selector, a CLI verb, a role file format (r-003) |
| 2 | **Screen detection as data**: an optional rules file per harness that feeds the activity badge for runners with no hook, with an explain call | herdr `src/detect` | Medium | A `detect` field on the harness row, an evaluator beside `looksidle.go` on the screen already kept, badge only and never a column |
| 3 | **A dispatch gate**: an out-of-process command asked "proceed, wait or reject" before a launch, prompt or peer message, fail closed | bb `message.dispatch` | Medium | A gate table shaped like sources, a runner beside `sources.go`, a human bypass, three-strikes switch-off |
| 4 | **A board wide freeze**: a setting that answers every permission with a block and refuses launches, exempting the director, set by hand or by a spend trigger | Gas Town `estop` | Small | A chain step after shelved and before rules, one setting, a launch refusal. It stops the next call only |
| 5 | **Hold a launch behind the cap and give the cap a role**: a capped launch waits and starts when a slot frees, and a role can carry its own cap | Gas Town scheduler | Small to medium | A held state on the launch, the cap counted in the daemon and not only in `link/control_mcp.go`, a per role figure |
| 6 | **Dependencies between work items, and a gate only the board resolves** | Orca coordinator and decision gates | Medium | `blocked_by` on `work_item`, a held state, a report kind for a gate |
| 7 | **A repository held role file with last known good reload** | Symphony 6.2 and 6.3 | Medium | Part of idea 1. Cache by content hash, keep the previous version on a bad edit, post a notice |
| 8 | **Bound and expire the message queue**: `expires_at`, `deliver_after` and a depth per card | Gas Town `nudge/queue.go` | Small | Two columns at the end of the slice, a filter in `PendingMessages`, a depth check |
| 9 | **Per-context brief selection**: choose the launch brief and skill set by tag and runner, quoting card attributes as untrusted | bb `agents.configure` | Small | Now part of a role (idea 1). The brief today is a file per launch with no template store |
| 10 | **A permission ceiling per host or room**, clamping auto mode and rule application, settable from the board only | bb `maxPermissionMode` | Small | One setting and a check at step 5 of the chain. Does not reorder it |
| 11 | **Batched child outcomes to a parent**, with a truncation marker and "this is not the final result" guidance | bb `child-thread-notifications.ts` | Small | A 2 second batch in the message queue keyed by the receiving card |
| 12 | **Offer the rule the agent's own CLI would offer** when approving | vibe-kanban `permission_suggestions` | Small | An "approve and always allow this prefix" control writing a rule. Check the board and `auto-mode.md` first |
| 13 | **Trust adapters for codex, cursor and copilot** | Orca `agent-trust-presets.ts` | Small | Three files beside `claudetrust.go` in `internal/runnersetup` |
| 14 | **Validate a resume argv before it is typed** | herdr `agent_resume.rs` | Small | Checks where `ResumeArgs` is expanded |
| 15 | **A fake daemon for extension authors** | bb `plugin-sdk/testing` | Medium | Nothing until packs exist. Do not start it earlier |
| 16 | **Orchestration patterns as text**: adversarial verify, judge panel, loop-until-dry, completeness critic, "no silent caps" | bb `orchestration.md` | None | Copy into the review director's brief and the panel's instructions |
| 17 | **Archive with an undo grace** | bb `ARCHIVE_UNDO_GRACE_MS` | Small | `sweep.go` already separates archiving from deleting |
| 18 | **A guide printed by the binary** so agent instructions cannot drift from it | Orca `skills get` | Small | Check `internal/daemon/help.go` first, it may do this |

**Checked and dropped because atrium already has it.** A time boxed auto mode (humanlayer's expiry monitor: atrium's
`AutoUntil`, `daemon.go:748`). Verifying a worker's report against the tree (OMC factcheck: atrium's `SetReportSHA`).
Trust pre-marking for claude and gemini (Orca: `internal/runnersetup`). Typing a peer message when the target is
settled (Orca's pointer delivery: `peers.go`). A question form of approval (vibe-kanban: `store/ask.go`). URL to launch
dialog (in bb and Symphony terms, an intake recogniser: `store/recognisers.go`).

**Explicitly refused.** An in-process plugin runtime, a JavaScript runtime for scripted orchestration, and a cloud
pairing service (reasons in `docs/rnd/bb.md` section 6). A vendor fetched catalog of detection rules (herdr does it, and
atrium would not call out on start). Auto approval by pressing Enter on a screen string (claude-squad). "Agent
decides, Go transports" as a design rule (Gas Town), since atrium enforces in code what it can. A live server handoff
by passing file descriptors (herdr), which is Unix only and would not exist on the platform atrium leads on.

## 5. Where atrium is ahead

Against bb, in `docs/rnd/bb.md` section 4. Against Charon, in `docs/rnd/charon.md` section 6. Against the tools in section 2,
each has its own paragraph. Across all of them, the same three points hold:

1. **A gate that answers before a tool runs, backed by durable rules and an audit log.** Nobody read has this.
   Orca and herdr report a blocked state, vibe-kanban and humanlayer keep approvals that are gone or scoped to one
   channel, claude-squad and OMC do not gate, Symphony ships `approval_policy: never`.
2. **A real terminal a human types into, on Windows, with supervision.** Only herdr shares the shape, and its handoff
   is Unix only and its Windows build is labelled beta.
3. **Storage failure halts and does not degrade.** No other tool read has a rule for it.

## 6. What the tools agree on

The first two, bb and Charon, both drive the Claude Agent SDK in-process and both built a permission surface on
`canUseTool`. Neither has a matchable durable rule store in the provider path, and both document that an approval card
is not a security boundary. Two independent projects arriving at SDK-client architecture, and neither at supervising a
terminal, was read in the first version of this file as evidence that atrium's shape is the uncommon one.

**Revised.** The adoption ranking in section 1 says the opposite for the top of the field. Orca (81,433) and herdr
(41,412) both own ptys, and they are the two most adopted tools that meet the criterion. The SDK-client tools read
(bb 3,990, Charon, vibe-kanban 28,216 as headless stdio, humanlayer 11,622) are smaller or shut down or deprecated. So
the honest reading is that owning the terminal is a common shape at the top and is not the rare one. **What is rare is
gating every tool call while owning it.** The question for clint is no longer "is anybody else supervising a
terminal" but "is the gate the thing we sell".

## 7. Not covered yet

- **Tools ranked in section 1 and not read: cmux (27,499), happy (23,946), t3code (23,882), paseo (18,994), superset
  (14,733) and agent-orchestrator (12,522).** Not read, because the brief said five to eight and nine were read. Three of
  the
  six (cmux, happy, paseo) are by their descriptions a terminal app or a phone client, which are the surfaces atrium
  is least likely to learn from. superset and agent-orchestrator are worktree-per-agent managers of the same kind as
  claude-squad and Orca. None of the six is verified beyond a repository description.
- **ruflo, oh-my-claudecode and Gas Town were read shallowly.** Sections 2.4, 2.7 and 2.9 say what was and was not
  opened. Gas Town's Refinery merge queue and Witness watchdog in particular were not opened, and may hold ideas
  the freeze and cap items above do not.
- **Claude Code's own features and Codex's app server and cloud surface.** A web search named Agent View, Agent Teams
  and Dynamic Workflows as built in Claude Code features. Nothing was fetched or read for them.
- **The unranked tail of the awesome list.** 238 repositories were looked up and only the top of the sort was
  considered. Anything with fewer than about 2,000 stars was not read, including `standardagents/dmux` (1,790),
  `awslabs/cli-agent-orchestrator` (1,358) and `preset-io/agor` (1,412), which are in the same category.
- **Whether Claude's `PreToolUse` hook input carries permission suggestions.** Idea 12 depends on it.
- **Whether the board's approve control already offers a prefix rule.** Not looked at in `web/`.
- **Whether a launch from the board or the HTTP API is capped anywhere.** A grep of `internal/daemon` found nothing.
- **Whether `internal/daemon/help.go` serves a whole guide.** Idea 18 depends on it.
- **The `internal/claudeconf/codex_test.go` finding.** What codex hook handling exists was not traced.
- **bb's items in `docs/rnd/bb.md` section 7,** three of five still open: workflow replay, `concurrency-limit` counting and
  safe mode, and the non-Claude bridges. Closed in this pass: **session grants do not persist as far as
  the code shows.** `sessionPermissionGrants` is initialised to an empty array when the thread attachment is created
  (`plugins/provider-claude-code/src/bridge/bridge.ts:930`), appended to in memory when a grant is cached (`:2502`),
  and the name appears in no other file in the clone.
- **Nothing was executed.** No tool in this file, including atrium's own code paths, was run for this pass.
