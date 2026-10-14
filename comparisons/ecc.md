# Atrium and ECC

ECC (https://github.com/affaan-m/ECC, package `ecc-universal`, VERSION 2.2.3, read at commit 4eb71d9 of 2026-10-09) is
mostly a content and configuration distribution for coding agents: about 300 skills, 70 agents, 95 commands, rules, a
Node hook runtime and an installer that targets 17 harnesses. It also carries `ecc2/`, an alpha Rust control plane with
a terminal dashboard and a SQLite session store, which is the only part that overlaps atrium's job. This page compares
them from the code of both and ends with what atrium should take.

ECC paths are relative to a checkout of ECC. Atrium paths are relative to this repo. Where a claim rests on the README
or a skill's markdown alone and not on code, it says so. I did not run any ECC installer or hook.

## Summary

| Question | ECC | Atrium |
| --- | --- | --- |
| What it is | Content plus hooks installed into the agent's own config, and an alpha Rust session manager (`ecc2/`) | A task board with live agents in terminals it owns |
| Unit of work | A skill, agent or command file. In `ecc2`, a session row, `ecc2/src/session/store.rs` | A card with a status column, event log, queue and worktree |
| Where agents live | Your own terminal for the plugin. In `ecc2`, a child process with piped stdout, `ecc2/src/session/runtime.rs` | A pty the room owns, optionally in a pty host, `internal/ptyhost`, `internal/daemon` |
| Can a human type into the agent | Not through `ecc2`. It starts `claude --print`, so there is no interactive terminal, `ecc2/src/session/manager.rs` around line 3098 | Yes, the board attaches to the pty |
| Permission control | PreToolUse hooks that deny with a reason or warn, `hooks/hooks.json`, `scripts/hooks/gateguard-fact-force.js` | Every tool call asks the room, a chain of rules, auto mode and the human, `internal/daemon/daemon.go`, `docs/runtime/auto-mode.md` |
| Command analysis | Regex and token checks in JS, `scripts/lib/shell-substitution.js` | A real shell parser, `internal/guard/bash.go`, `internal/guard/rules.json` |
| Fail behaviour of hooks | Mixed. Some hooks fail closed on oversize input, `hooks/README.md`, `scripts/hooks/run-with-flags.js` | A hook never fails a session, `internal/cli/hook.go` |
| Durable state | JSONL and JSON files under `~/.claude`. SQLite in `ecc2` with about 20 tables | One SQLite file per room, a smaller one on the hub, `internal/store` |
| Agents messaging each other | A `messages` table with typed payloads, `ecc2/src/comms/mod.rs`. Delivery into a live agent not found | `atrium_say`, queue, typed or hook carried, `docs/runtime/agent-messaging.md` |
| Many machines | A remote dispatch table in `ecc2`, `remote_dispatch_requests`. Not read in depth | A hub with one room per machine over mutual TLS, `docs/fabric/hub-and-rooms.md` |
| Runners | 17 install targets, `scripts/lib/install-targets`. `ecc2` launches claude, codex and configurable others, `HarnessRunnerConfig` in `ecc2/src/config/mod.rs` | A runner table, `internal/runnerprofile/profile.go` |
| Cost and context signals | A metrics bridge fed by hooks, with warnings injected into the agent, `scripts/hooks/ecc-metrics-bridge.js`, `ecc-context-monitor.js` | Statusline telemetry drawn on the card, `internal/daemon/telemetry.go`. Nothing is said to the agent |
| Learning across sessions | Observation hooks, instincts with a confidence score, session summaries loaded at start, `skills/continuous-learning-v2`, `scripts/hooks/session-start.js` | The work ledger and handoff for a new context, `docs/runtime/context-cycle-design.md` |
| Install | Manifest driven installer with profiles, modules, state file, uninstall, `manifests/`, `scripts/lib/install` | Board writes the missing hooks, service installers, `docs/runtime/hooks.md` |
| Stack | Node, a Rust alpha, Python dashboard, markdown | Go single binary, vanilla JS front end |
| Tests | 391 test files, a CI chain of validators, an 80 percent coverage gate, `package.json` | Go tests, see `docs/test-plan.md` |

## Architecture

**ECC** is two products sharing a repo. The big one is a distribution. `manifests/install-modules.json` and
`install-profiles.json` name modules (rules, agents, commands, hooks runtime, orchestration, security), the installer
copies them into each harness's config root and records what it wrote in an install state file
(`schemas/install-state.schema.json`), so uninstall and repair know what ECC owns. Hooks are Node scripts invoked as
`node -e` one liners from `hooks/hooks.json`, resolved against `CLAUDE_PLUGIN_ROOT`, and gated by `ECC_HOOK_PROFILE`
(minimal, standard, strict) and `ECC_DISABLED_HOOKS` (`scripts/hooks/run-with-flags.js`, `scripts/lib/hook-flags.js`).
State between hooks lives in files: a per-session bridge file in the temp dir (`scripts/lib/session-bridge.js`, written
atomically with rename retries for Windows), JSONL metrics under `~/.claude/metrics`, and a 30 minute session state file
for the gate.

The small one is `ecc2/`, which its own README calls alpha scaffolding (`ecc2/README.md`). It is a Rust binary
(clap, ratatui, rusqlite) with a daemon loop (`ecc2/src/session/daemon.rs`) that, each tick, marks sessions whose pid
died as failed, runs scheduled tasks, dispatches remote and backlog work, rebalances, auto merges ready worktrees and
prunes idle ones. A session is a child process started with `claude --print` and its stdout and stderr piped into a
`session_output` table by a dedicated DB writer thread, with a heartbeat (`ecc2/src/session/runtime.rs`). The dashboard
(`ecc2/src/tui/dashboard.rs`, 15,000 lines) reads that table. So `ecc2` supervises headless runs and shows their text.
It does not own an interactive terminal. There is no step where a human attaches and types.

**Atrium** is a hub and rooms, with the room owning ptys and agents (`docs/how-atrium-works.md`). The structural
difference: ECC reaches an agent through the agent's own extension points (hooks, skills, rules) and trusts them to be
installed. Atrium owns the process, so it can type, restart, read the screen and attach a human, and it uses hooks only
to learn and to gate.

## Feature by feature

### Permission control: deny with a lesson against ask the human

This is the area with the most to learn from.

**ECC.** The PreToolUse hooks return a deny with a reason, and the agent reads the reason and tries again. The best
example is GateGuard (`scripts/hooks/gateguard-fact-force.js`, about 2,000 lines). Its premise, in the file header, is
that asking an LLM "are you sure" always gets yes, so the gate demands facts instead. The first Edit or Write on a path
is denied with a request to list importers, the public API touched, data schemas and to quote the user's instruction.
The second attempt on that path passes, tracked in a per session state file (30 minute expiry, 500 entry cap).
Destructive Bash (a list that includes SQL phrases, `dd`, and a PowerShell classifier) asks for targets and a rollback
plan. Routine Bash asks once per session for the instruction. There are narrow escapes (`GATEGUARD_EXEMPT_GLOBS`,
`GATEGUARD_BASH_ROUTINE_DISABLED`). Oversize hook input fails closed for the gate and MCP health hooks
(`hooks/README.md`, `scripts/hooks/run-with-flags.js`). Whether forcing the agent to write facts improves its edits is a
claim of the skill text and the author's experience. I found no measurement of it in the repo.

Other denials in the same style: a dev server blocker that refuses `npm run dev` outside tmux, a commit quality check
that blocks on secrets, `--no-verify` blocking (`scripts/hooks/block-no-verify.js`), and config protection
(`scripts/hooks/config-protection.js`) that stops the agent weakening linter and formatter configs. Hookify
(`scripts/hooks/hookify-runtime.js`) lets a project define warn or block rules in files.

`ecc2` has a risk score per tool call (`ecc2/src/observability/mod.rs`): a base by tool, plus file sensitivity (`.env`,
tokens), blast radius and irreversibility, clamped to 0 to 1 and mapped by thresholds to allow, review, require
confirmation or block. It is substring matching on a summary and it is computed after the fact for the log. Nothing
in it stops a call in the code I read.

**Atrium.** The call blocks in the room until a chain answers: replay, queued message, shelf, deploy hold, standing rule,
auto mode, then the human (`docs/how-atrium-works.md`). Commands are parsed with a real shell grammar
(`internal/guard/bash.go`), so quoting and substitution are read as the shell reads them. ECC's own gate file shows what
the regex route costs: a comment on the `dd if=` case says a word boundary decided the verdict instead of command
position, fixed only in issue #2642. Atrium's decision is a yes or no from a person or a rule. It never asks the agent
for evidence.

**Difference.** ECC's gate changes what the agent does before it acts. Atrium's gate changes whether the act happens.
They compose. A reason string already rides atrium's block path (a queued message is delivered as the block reason), so
a rule outcome of "deny, and tell the agent to first do X" fits the existing chain with no new hook.

### Signals pushed into the agent: context, cost, loops

**ECC.** `ecc-metrics-bridge.js` keeps a running aggregate per session (cost, files touched, a ring of the last five
tool call hashes), and `ecc-context-monitor.js` reads it after every tool call and returns `additionalContext` when
context remaining falls under 35 and 25 percent, cost crosses 5, 10 and 50 dollars, more than 20 files were touched, or
the last five calls are identical (`LOOP_THRESHOLD`). Warnings are debounced. The loop threshold was raised from 3 to 5
after false positives, a comment says so. The cost tracker prefers the harness's own cost number over a transcript sum
when the statusline wrote one, and its header records that an earlier version produced zero filled rows for 52 days
(2,340 rows) because it read a field the payload does not carry. That is a useful warning about telemetry.

**Atrium.** The same inputs exist and go to the board only. Context used and window arrive from the statusline
(`internal/daemon/telemetry.go`, `ContextUsed`, `ContextWindow`), and the card shows how close it is to compacting
(`docs/runtime/statusline-telemetry.md`). Atrium has the harder half, since it can tell the operator and ECC can only
tell the agent. It does not tell the agent. I found no tool loop detector in `internal/daemon`.

### Liveness and what to do about a stuck session

**ECC.** Two mechanisms. `ecc2` daemon marks a Running session failed at startup when its pid is gone, and a heartbeat
timestamp is written while output is quiet, so a quiet process is not mistaken for a dead one
(`ecc2/src/session/daemon.rs`, `runtime.rs`). Separately `scripts/loop-status.js` reads Claude's transcript JSONL and
flags a Bash call with no result after 30 minutes and an overdue `ScheduleWakeup`, with an exit code for scripts.
This is a read of a file Claude writes, and it finds a stall that no hook reports because the missing event is the
symptom.

**Atrium.** Hooks give activity, and the screen is the fallback (`internal/daemon/idleframe.go`). A tool call that never
returns leaves the badge on "running a tool" with nothing to say it is old. I did not find an age on that badge.

### Two agents on one file

**ECC.** `ecc2` records a `conflict_incidents` row when two sessions touch the same path, picks one to continue and pauses
the other by a configurable strategy (`enforce_conflict_resolution` in `ecc2/src/session/manager.rs`, table at
`store.rs` line 415). It rests on a `tool_log` with `file_paths_json` per call.

**Atrium.** Worktree per card means two cards rarely share a file on disk, and the conflict shows at merge. I found no
live overlap warning between cards. Where cards share one checkout on purpose it would matter.

### Messaging between agents

**ECC.** `ecc2/src/comms/mod.rs` defines five typed messages: TaskHandoff with a priority, Query, Response, Completed
with files changed, and Conflict. They are rows in a `messages` table with a read flag. In the code I read, handoffs are
consumed by the lead and delegate scheduling paths in `manager.rs` (around lines 915 and 1094), which feed the next
session's task. I did not find them typed or injected into a running agent, so this is a work queue between
sessions and not a chat between live ones. Treat that as my reading of a 8,000 line file.

**Atrium.** Messages reach a running session, typed when the line is quiet or carried on the next hook, with a held
line gate and cross room delivery (`docs/runtime/agent-messaging.md`). Atrium has no typed envelope. A message is text.
ECC's `Completed` with a file list is the one shape worth looking at, since `atrium finish` recaps are text too.

### Memory across sessions

**ECC.** A SessionStart hook loads the latest session summary, learned skills and instincts above a confidence threshold
(0.7, at most 6 injected, summaries capped at 220 characters), with an optional relevance rank against the project's
stack keywords (`scripts/hooks/session-start.js`, `scripts/lib/instinct-relevance.js`). A Stop hook
(`skills/continuous-learning-v2/hooks`) and `instinct-cli.py` build the instincts from observed tool use. The injected
context is capped (`ECC_SESSION_START_MAX_CHARS`, default 8000). Whether instincts help is not measured in the repo that
I could find.

**Atrium.** The card's recap, event log and the handoff written before a context clear, with the work ledger
(`docs/runtime/work-ledger-design.md`). That is per task memory. Atrium has nothing that learns a habit across tasks,
by choice so far.

### Distribution and install

**ECC.** The installer is the strongest engineering in the repo. Modules and profiles are validated JSON with schemas,
installs are planned before applied (`scripts/install-plan.js`, `install-apply.js`), every write goes through a
guarded, contained writer (`scripts/lib/install/guarded-write.js`, `contained-fs.js`, `ownership-guard.js`), Claude's
`settings.json` is edited under a lock, ECC owned hook entries are tracked by stable id so reinstall is idempotent and
uninstall is exact (`hooks/hooks.metadata.json`, with a fingerprint per entry so drift is caught), and `doctor.js` and
`repair.js` exist. A `--dry-run` sandbox exists for Claude (`scripts/lib/claude-dry-run-sandbox.js`).

**Atrium.** The board writes the missing hooks into `settings.json` (`docs/runtime/hooks.md`). I did not trace whether it
tracks ownership by id the way ECC does.

### Content: skills, agents, rules

ECC ships 302 skill directories, 71 agent files, 95 commands and 122 rule files (counts from `git ls-files`). The
quality varies, and a catalog check (`scripts/ci/catalog.js`) keeps counts in sync. The pi, opencode, cursor and codex
variants are generated or copied by build scripts. This is the half of ECC with no atrium counterpart, because atrium
does not distribute prompts. Whether it helps an agent is the README's claim. I did not evaluate any skill's effect.

`ecc2/src/harness_eval.rs` and the `harness_*` tables are a content addressed store for harness configurations with
evaluations and an audit log, so a configuration can be compared with a baseline. It is a store and a comparison policy.
I found no runner that produces the samples.

## Where ECC is ahead

- **Gates that teach.** Deny with a required investigation, and narrow escape hatches per gate.
- **Signals told to the agent.** Context, cost, files and loop warnings returned as context on the next call.
- **Stall detection from the transcript.** A call with no result for 30 minutes, an overdue wakeup.
- **Install lifecycle.** Plan, guarded apply, ownership by stable id, doctor, repair, uninstall.
- **A live file conflict check** across sessions, with a recorded incident.
- **Test hygiene.** Validators for every content type in the test chain, and a coverage gate.
- **Breadth.** 17 targets, and a large body of reusable skills.

## Where atrium is ahead

- **It owns the terminal.** ECC's session manager runs `--print` and shows text. Atrium attaches a human, types, restarts
  and survives a room restart with the pty host.
- **A real decision, not a nudge.** ECC's hooks can deny, but nothing in them waits for a person. There is no board of
  waiting decisions, no standing rules by specificity and no audit of who answered. Atrium has all three.
- **Parsed commands.** A shell grammar against regex.
- **Hooks never fail the session.** ECC has several fail closed paths and a switch to turn each off when a trusted call
  trips them, which is the recovery path its own README describes.
- **Many machines under mutual TLS** with a git mirror and cross room messages.
- **Messages that reach a live agent.**
- **Status as human attention.** A card says what needs a person. ECC's dashboard lists sessions and risk.

## What atrium should take

Ranked by value.

1. **Deny with an instruction as a rule outcome.** Let a standing rule answer "deny, with this text for the agent", so
   an operator can say "before editing under `internal/store`, list who calls it". The text rides the existing block
   reason path that queued messages already use. ECC: `scripts/hooks/gateguard-fact-force.js` (the per path first touch
   state and `denyResult`). Atrium: `internal/daemon/daemon.go` (`onPermRequest`, the rule step) and `internal/mcprule`.
   Small to medium. Keep it a rule the operator opts into, since the benefit is unmeasured.
2. **Tell the agent when its context or tool loop is going wrong.** Atrium already holds context used and window per
   card. Add a debounced note delivered on the next permission call, through the same carrier as a queued message, at
   two thresholds, plus a loop note when the last five calls are identical in tool and input digest. ECC:
   `scripts/hooks/ecc-context-monitor.js` and `ecc-metrics-bridge.js` (`hashToolCall`, `LOOP_THRESHOLD`, with the
   note that 3 was too noisy). Atrium: `internal/daemon/telemetry.go`, `internal/daemon/daemon.go`, and a small loop
   ring beside the card's activity state. Small. Make it off by default and per card, since it spends the agent's
   context.
3. **Age on a tool call that never returns.** A card whose last tool started more than N minutes ago with no end event
   should show it, and notify. ECC: `scripts/loop-status.js` (`pending_bash_tool_result`, 30 minute default). Atrium:
   `internal/daemon/idleframe.go` and the activity design in `docs/runtime/activity-design.md`. Atrium has the hook
   events already, so unlike ECC it needs no transcript read. Small.
4. **Live overlap warning between cards.** When two live cards in the same repo edit the same path, say so on both and
   to the director. Atrium would take the path list from PostToolUse inputs it already receives. ECC:
   `ecc2/src/session/manager.rs` (`enforce_conflict_resolution`) and the `conflict_incidents` table. Atrium:
   the activity path in `internal/daemon` and the review tab (`docs/rnd/review-tab-design.md`). Take the warning only.
   Do not take the automatic pause. Medium.
5. **Install ownership by stable id and fingerprint.** Mark each hook entry atrium writes with an id and a fingerprint of
   its command, so a board repair rewrites only atrium's entries and a hand edit is reported. ECC:
   `hooks/hooks.metadata.json` and `scripts/lib/install/ownership-guard.js`. Atrium: the code behind
   `docs/runtime/hooks.md` and `internal/claudeconf`. Small, and only if I am right that atrium does not do it already.
6. **A typed completion message.** Give `atrium finish` an optional list of files changed so a director can act on it
   without parsing prose. ECC: `ecc2/src/comms/mod.rs` (`Completed`). Atrium: `internal/cli` finish and
   `docs/runtime/agent-messaging.md`. Small. Take only the completion shape, not the five type taxonomy.
7. **Heartbeat during quiet output.** Distinguish a quiet live process from a dead one by a timestamp written on a timer.
   ECC: `ecc2/src/session/runtime.rs`. Atrium probably covers this through the pty and the pid, so check
   `internal/daemon` before doing anything. Low.

## What atrium should not take

- **The skill, agent and command library.** It is prompt content with no measured effect, 300 skills deep. Atrium's
  value is the board and the gate. A content store would add a catalog to keep honest.
- **Instincts and automatic learning.** Observation hooks that write habits back into every future session add hidden
  state to the prompt. Atrium's cards are meant to be readable from their event log. The README claims are unverified.
- **Regex command classification.** Atrium's parser is better. ECC's own history in the gate file (issue #2642) shows the
  failure mode.
- **Fail closed hooks.** Atrium's rule that a hook never fails a session is the right one for a tool whose job is
  keeping many agents running. ECC's per hook escape variables exist because its gates sometimes stop trusted work.
- **Headless `--print` supervision as the session model.** It gives up the attached terminal, which is the reason to run
  agents under atrium at all.
- **Automatic worktree merge and prune in a daemon tick.** `ecc2/src/session/daemon.rs` merges ready worktrees and
  prunes idle ones unattended. Atrium keeps merges behind the done handoff and the operator.
- **Risk score thresholds on substrings.** A number from keyword hits invites trust it has not earned, and atrium's rules
  and parsed commands answer the same question with something checkable.
- **Compute sponsorship and marketplace links in the package.** The `welcome` script and the README promote a compute
  vendor. Not a product concern for atrium.
