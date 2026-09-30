# Item 16 reading notes: reviews that remember

Read by fb05 on 2026-09-29 for @fabric. Facts with paths and line numbers. The "means for the design" line closes each
section and is the only opinion. `DF` = `D:/git/github/dovholuknf/dotfiles`, `DA` = `.../dotagents`, `TR` = the pr-4480
transcript folder `~/.claude/projects/D--worktrees-github-openziti-ziti-pr-4480/110fe994-.../`.

## (a) The review panel today

**Skill.** `DF/claude/skills/review-panel/SKILL.md`, 203 lines. It is a conductor and says it does not review (l.14).
There is a `.backup/` copy and a `pr-review` skill that also mentions it. Not in dotagents.

**Steps.**

- Step 1 (l.65-76): capture the diff text once. "Every agent reviews the identical snapshot, so you hand them the
  captured diff text (plus the range so they can widen context)" (l.73-75).
- Step 2 (l.78-103): sizing is by file type and change shape, not diff size. `codebase-steward` on almost every
  non-trivial change, `go-security-reviewer` for `*.go`, `functional-tester` for behaviour changes with test stakes,
  `nonfunctional-tester` only with a user-visible latency or load surface. Also c-systems, csharp, windows veterans.
  There is no rule that shrinks the panel for a small diff. A backport touching Go and behaviour gets four.
- Step 3 (l.105-125): invoking the skill is the go-ahead, print one line, dispatch.
- Step 4 (l.127-145): "Launch all selected agents in a SINGLE message with one `Agent` tool call each, so they run
  concurrently in isolated contexts." Each gets the repo path, the captured diff and range, its mandate, "read
  whatever surrounding files or dependency source they need, NOT just the diff", review only, no builds or tests
  (l.138), and the severity scale plus JSON finding schema verbatim (l.142-145).
- Step 5 (l.147-165): pre-filter `preexisting` and empty `prod_survival`, then one refuting verifier per remaining
  blocking/high, "a fork of the same specialist type that raised it (falling back to `general-purpose`)" (l.157-158).
- Step 6 (l.167-188): integrity check of each JSON array against its prose, dedupe on file+line, two lanes, then a
  `general-purpose` completeness critic. Step 7 (l.190-203): one report, no fix offered.

**Hand-back.** A fenced JSON array: severity, file, line, category, claim, evidence, preexisting, prod_survival, fix,
confidence (l.29-45). Dependency-behaviour claims must quote the dependency's own source at the pinned version (l.53-56).

**The personas** are `DF/claude/agents/{go-security-reviewer,codebase-steward,functional-tester,nonfunctional-tester}.md`
(182, 181, 98, 97 lines). Frontmatter: steward `model: opus`, the other three `model: sonnet`. All four have
`memory: user`. Tools: the three sonnet ones have Glob, Grep, Read, Skill, ToolSearch, WebFetch, WebSearch and worktree
enter/exit. The steward also has Bash, Write and Edit (l.4). The steward's mandate (l.56-73): find sibling code, "Open
those files and read them fully", read dependency source in the module cache. "Read-heavy is the point" (l.123). So the
duplicated reading is by design in the persona, and the skill adds "not just the diff".

**Persona memory already exists.** Each persona ends with a "Persistent Agent Memory" section (go-security l.130-182).
Directory `C:\Users\claude\.claude\agent-memory\<persona>\`, user scope, "keep entries general. They apply across all
projects" (l.134). Types user, feedback, project, reference. "What NOT to save: specific bugs... file paths, function
names, which are re-derivable from the repo" (l.143-148). On disk today (`~/.claude/agent-memory/`): codebase-steward 17
project/reference files plus MEMORY.md (3172 bytes, last write 2026-09-27, mostly OpenZiti lessons), c-systems-reviewer 1,
functional-tester 1, go-security-reviewer 1, nonfunctional-tester 2. Empty: csharp-expert, network-expert, others.

**How they were started in pr-4480.** `TR/subagents/*.meta.json`: every one has `"requestShape":"background"`,
`"spawnDepth":1`, a named `agentType`. So background, fresh (not fork), by agentType. Seven
subagents ran: 4 reviewers, 2 go-security verifiers (one `stoppedByUser`), 1 general-purpose critic.

**Cost, from `TR/subagents/*.jsonl` (usage fields summed by assistant turn).**

| agent | secs | turns | out tok | cache read | cache create | peak context | reads / greps |
|---|---|---|---|---|---|---|---|
| codebase-steward | 561 | 95 | 18.3k | 9.10M | 307k | 165k | 19 Read, 21 Grep, 23 Bash |
| go-security-reviewer | 555 | 60 | 26.6k | 5.65M | 337k | 149k | 12 Read, 14 Grep, 8 Glob |
| functional-tester | 492 | 43 | 25.8k | 3.48M | 306k | 128k | 6 Read, 3 Grep, 13 Glob |
| nonfunctional-tester | 326 | 51 | 17.5k | 3.57M | 216k | 103k | 15 Read, 12 Grep |
| verify: race | 462 | 63 | 10.7k | 3.34M | 121k | 81k | 9 Read, 22 Glob |
| verify: stale file | 156 | 34 | 7.1k | 1.64M | 131k | 69k | 11 Read |
| coverage critic | 170 | 31 | 6.5k | 1.91M | 130k | 89k | 6 Read, 8 Grep |

Item 16 quotes "104k to 133k tokens" per reviewer. That matches peak context, not spend. Summed across turns the four
reviewers cost 21.8M cache-read tokens and about 1.17M cache-create, and the whole panel 28.7M cache-read. The
cost is turns times a growing context, so fewer turns matters as much as a smaller start.

**Files read (Read tool, repo-relative).** Three of four reviewers (not the steward) and the critic and both verifiers
read the diff file `build.claude/pr4480.diff` once, even though the skill hands them the text. Shared: `router/env/ctrls.go` (all 7 agents, up to 5 times in a verifier),
`common/ctrlchan/channel.go` (4 of 4 reviewers), `router/env/ctrl.go` (4 of 4), `router/router.go` (3), `tests/CLAUDE.md`
(functional, steward), and module-cache `channel/v4 multi.go` (all four, 4 reads by the security reviewer and 3 by the
resilience one). Only the steward read `tests/context.go` (twice), the sibling handlers and
`controller/handler_ctrl/*`. The backlog's `context.go, controller.go, multi.go, router.go` list is close but the
transcripts show `ctrls.go`, `channel.go`, `ctrl.go` and `multi.go` as the shared core. Reviewers differ by version:
security read `channel/v4@v4.3.11`, steward and resilience `v4.3.13`. Not verified which one go.mod pins.

**Also.** The panel's own reads are not counted above: the parent is `TR/110fe994-....jsonl`, 780 KB.

*Means for design:* fix 1 has two levers already present in the skill text, hand over the diff (done, reviewers still
re-read it) and shrink the panel (no size rule exists). A shared digest must name the core files and the pinned
dependency version. The persona already has a memory mechanism, but user scope and "keep it general" is why it holds
so little repo knowledge.

## (b) "Personas that learn per repo"

`D:/git/github/dovholuknf/atrium/docs/far-backlog.md` lines 36-46, untracked, main checkout only. Quote:

> The persona pack in the private dotagents repo holds each specialist agent's instructions and lessons. clint's
> direction (2026-09-23): the pack matters more than individually named sessions, and each persona should carry one
> agent-specific CLAUDE.md per repo, `personas/<id>/repos/<host>/<org>/<repo>.md`. It loads when the agent works in
> that repo, and the agent edits it itself after every job, so each run leaves the agent better at that repo. The git
> diff is the review, because only clint commits the pack. Company and context files stay in dotagents, never here.
> The current pack (fresh subagent per review, lesson files, a separate lessons review) is not what he wanted. Write a
> one-page design for his approval before building anything. The first attempt (pack, render scripts, evals) was
> undone on 2026-09-23 and parked at `D:\tmp\dotagents-personas-parked` as a starting point. The design doc is in git
> history: `git show bc58c32:docs/personas-design.md`.

Parked folder exists: `D:/tmp/dotagents-personas-parked/` holds `BRIEF.md`, `gitignore.diff`, `personas/`, `scripts/`.
I did not read inside it. The design doc at `bc58c32` is in some repo's history, not read (which repo is not stated).

*Means for design:* the stated shape is a per-persona per-repo file, agent-edited, reviewed as a git diff. Item 16
fix 2 instead says one resident reviewer per repo. Those differ (four persona files per repo versus one session).

## (c) Mercurius as a memory already

**mercurius.yaml.** Real file `DA/github/dovholuknf/atrium/mercurius.yaml` (123 lines). The main checkout, the ui
worktree and this worktree each have `mercurius.yaml` as a symlink to it (`ls -la` confirmed for all three). Fields:

- `max_findings: 6` (l.16).
- `review_context` (l.23-39): calibration, "true in round one and still true in round fourteen" (l.20-21). For atrium:
  personal tool, single operator, pre-1.0, Windows 11, artifact is a v2 design doc, judge buildability.
- `settled_decisions` (l.50-84): list of `{id, do_not_flag}` guards. Seven now, for example `no-auth-single-user`,
  `storage-sqlite-chosen`. l.42-47 says earn a guard by re-litigating a finding, and warns of bloat. `id` is never shown
  to the reviewer.
- `review_focus` (l.90-118): three surfaces, resilience invariants, layering, process supervision, plus staging.
- `reviewer` (l.120-123): `name: codex, impl: codex, model: gpt-5.5`.
- Also `log_destination` defaults to `.mercurius`. Item 30 (`docs/backlog-2.md` l.780-799) records that a `claude`
  reviewer exists too (`mercurius/internal/reviewer/claude/README.md`, runs `claude -p ... --permission-mode plan
  --no-session-persistence`, l.10, loads the project CLAUDE.md by walking up from the round dir, l.19).

**Lookup.** `mercurius/internal/config/config.go:69-103` (`LoadWithRaw`): default `./mercurius.yaml`, else the `--config`
path, made absolute, read once. There is no upward search. The MCP registration passes an absolute path
(`docs/current/user-guide.md:57`). The project name is the directory holding the file (`config.go:94`).

**Re-read per round.** `internal/mcpserver/mcpServer.go:205-213` defines `CalibrationProvider`, "Production wiring
re-reads mercurius.yaml on each call so edits between rounds" take effect. `ConfigCalibrationProvider` (l.260-265) reads
the file at the given path every call, used by `start_review_round` (l.352-399, message "reread mercurius.yaml"). The
YAML comment at l.48-49 agrees: takes effect next round, no session reopen. `broker/types.go:12,31,60` says the broker
holds no calibration, and `RawConfig` is the exact bytes read.

**On disk under `.mercurius/`.** Per session `s_<id>/` with `status.json` and `round-NN/`. Example ui worktree
`s_ajiZDcEq7DfD`: `status.json` (session id, state, opened_at, max_findings, `review_context_present`, reviewer,
`rounds[]` with `log_path`, `has_notes`, `decision_count`), and `round-02/` holding `_config.yaml` (6.6 KB, the raw yaml
that round read), `_prompt.md` (28.8 KB, the exact prompt), `_round.md` (the log), plus the artifacts copied in
(`item52.md`, `setpinorder.go` and others). A decision notes file `_notes.md` sits beside `_round.md` when written.
The main checkout has 19 session folders, ui has 4. `.gitignore` covers it and `dotagents/README.md` l.56 says run
artifacts are per-checkout and not synced.

**State across rounds and sessions.** None by design. `docs/current/architecture.md:32`: "Nothing flows between rounds
within a session... run another round... or close the session and open a new one." `user-guide.md:133`: "Decisions do
not carry forward into other rounds in the same session - each round starts cold." `AGENTS.md:11` says the decisions
log carry-forward was removed. The only carry-over is what a human edits into `mercurius.yaml`, which is symlinked from
dotagents and so is shared by every worktree of the repo. The reviewer never sees prior rounds.

**Mercurius has its own memory rule** (`AGENTS.md:71-86`): project knowledge goes in `docs/journal/YYYY-MM-DD.md`, not
in harness-local memory, read on arrival. Source is at `D:/git/github/michaelquigley/mercurius` (a git repo).

*Means for design:* the config already is a per-repo, human-edited, symlinked, re-read-every-round memory with a bloat
warning. The gap is what it cannot hold: learned facts about the code, and anything from earlier rounds. A resident
reviewer's knowledge and the settled_decisions guard list overlap and need a stated boundary.

## (d) What atrium already has for a resident session

- **Handoff / new context**: `docs/backlog-2.md` item 66 (l.1459-1473), a feature not yet described as built. Daemon
  types a fixed capture prompt (commit, write `HANDOFF.md` in the cwd), waits for the turn to end, types `/clear`,
  waits for the new session, types "Read HANDOFF.md and continue from it." The daemon holds the resume because a
  self-queued message could land before the clear and be wiped. Only for cards atrium owns the terminal of.
- **Lean launch**: item 29 (l.767-778), built. Claude worker starts with the user settings source dropped, filtered
  user settings, atrium-control and mercurius as the only MCP servers, short worker system prompt, auto-memory off.
  35,970 to 11,029 tokens on a `-p` probe. `docs/runtime/lean-workers-design.md` has numbers. Consequence: a lean worker has
  no user CLAUDE.md, no memory, no skills and no agents. So it would not load `~/.claude/agent-memory/<persona>`.
  `mcp: [...]` adds servers, `lean: false` gives the whole setup.
- **BRIEF.md**: `atrium_launch` `brief` is written to BRIEF.md in the new session's cwd and read first, "survives
  compaction". That is per-launch, not per-repo.
- **CLAUDE.md into a worktree**: `scripts/new-worktree.ps1` l.56-83 finds every CLAUDE.md in the main checkout that is
  itself a symlink (skipping `.git`, `node_modules`, `.mercurius`, l.56) and makes a symlink at the same relative path,
  and fails if CLAUDE.md is absent. This worktree confirms it: `CLAUDE.md -> .../dotagents/github/dovholuknf/atrium/
  CLAUDE.md`. `mercurius.yaml` is also symlinked here though the script text I read only names CLAUDE.md (l.57), so
  something else, probably dotagents `deploy.ps1`, links it. Not confirmed.
- **How dotagents reaches a repo**: `DA/README.md` l.3-4: symlinks, two managed names, `CLAUDE.md` and `mercurius.yaml`.
  Layout `DA/<host>/<org>/<repo>/`, with subfolders for nested CLAUDE.md (atrium has `internal/{agent,api,claudeconf,
  daemon,safepath,store}` there). `scripts/deploy.ps1` links into clones and worktrees, `capture.ps1` imports a file and
  leaves a symlink. Only the owner commits dotagents.
- **Where a resident's state could live** (candidates only): a per-repo folder in dotagents beside `mercurius.yaml`
  (symlinked, synced, git diff is the review, matches `far-backlog.md`), a `.mercurius/`-style ignored folder in the
  worktree (per-checkout, not synced, dies with it), or atrium's own store. The scm layout `<host>/<org>/<repo>` in the
  item text is exactly dotagents' layout. `DF/claude` agents' memory is user scope and cross-repo.

*Means for design:* a resident needs a non-lean launch or a lean one that names its state file explicitly, plus item
66's handoff to survive its own context growing. A dotagents state file reaches it only if deploy links it.

## (e) Open facts, not found or not checked

- The `pr-4480` parent transcript was not parsed. Parent-side cost of digest building is unknown.
- Whether dotagents or any repo holds `bc58c32:docs/personas-design.md` was not searched, and the parked folder
  `D:/tmp/dotagents-personas-parked/personas/` was not opened.
- Which mechanism links `mercurius.yaml` into a new worktree is not confirmed. `new-worktree.ps1` names CLAUDE.md only.
- The pinned `channel/v4` version in the ziti go.mod. Reviewers read two different module-cache versions.
- Whether a `memory: user` persona is loaded at all under `Agent` with a background subagent: the transcripts show the
  MEMORY.md path in the persona text, but I did not check whether any reviewer actually read or wrote it (the tool
  tallies show no Read of `agent-memory`, but the steward's MEMORY.md was last written 2026-09-27, after the run).
- Wall time per reviewer is first to last timestamp in its jsonl. It includes the tool waits.
- No Mercurius source change was read for a resident or `atrium` reviewer (item 30 says the `atrium` reviewer is
  proposed, and `prompt.BuildCodeReview` is uncommitted upstream).
