# Review on atrium: one call runs the reviewer panel as quiet child cards and returns one merged report

Status: design by @rnd, 2026-10-02. Nothing built. Asked through the orchestrator: make review through atrium the
easiest path. One call should launch the specialist reviewers (c-systems-reviewer, functional-tester,
codebase-steward and the others) as atrium cards that are children of the caller, quiet, watchable and with their
history kept. It should collect their findings and return one merged report, in the shape the review-panel skill
returns today.

**Amended 2026-10-02 evening (section 7):** the reviewers are standing personas with stable handles
(`atrium:qa-reviewer` and the others). Any session can address one, a message wakes it, its memory carries across
reviews, and the panel fans out to them.

Read for this design, 2026-10-02:
- `docs/rnd/pr-review-story.md` (on `claude/pr-review-story`; @rnd's review 4ad931af on `claude/rnd-pr-review-story`,
  not landed);
- `docs/review/item16-notes.md` (the review-panel skill and the personas, read on sg4 on 2026-09-29);
- `docs/review/review-memory-design.md`;
- `docs/backlog/runtime/17.md` (a panel's false "waiting on you" alerts);
- `internal/link/control_mcp.go:1183` and `:1295` to `:1352` (the launch cap, `launchInput`, `leanLaunch`);
- `internal/daemon/prrunner.go:770-790` (the PR runner's prime, as reviewed in 4ad931af MED 2);
- `~/.claude/hooks/atrium-perm-hook.ps1:101` on m1mini.

The dotfiles repo, the `review-panel` skill and the persona files are on sg4, not on m1mini. They are cited here from
`docs/review/item16-notes.md`, not read again.

## 0. The answer

- **Standing reviewers (section 7).** Each persona has a stable handle such as `atrium:qa-reviewer`, which any
  session on any room can address. A message opens a job, and atrium queues it and wakes the persona, never
  refusing it. Each job is a fresh, isolated session that reads the persona's notes, the repo's reviewer file and
  the change record. A follow-up resumes that job's session. The panel below is built on these personas.
  - **The knowledge base is the point (7.4).** Each persona keeps what it learned per repo, per author, per kind of
    bug, and the false positives it was told off for. It lives in dotagents, mirrored on the hub so every room has
    it, and grows after every job by checks with no model in the write.
  - **The hub serves the code (7.7).** Our branches on any room, and outside PR heads, are mirrored on the hub and
    fetched read-only by the persona's room. A reviewer never asks for a paste.
- **One call: `atrium_review`** (MCP), and `atrium review` (CLI). It takes a target and an optional recipe. The target
  is a commit range on a branch, a path, or a PR run.
  - The room runs the whole panel. That means the reviewers, the checks on their output, the verifiers, the
    completeness critic and the merge.
  - It wakes the caller **once**, with one report.
  - No reviewer's hand-back wakes the caller, so a five-reviewer panel costs the caller one turn, not five or more.
    Those extra wake-ups were behind `docs/backlog/runtime/17.md`, and each one re-reads the caller's whole context
    (`docs/rnd/operator-focus.md` section 4).
- **Each reviewer is a card**:
  - a child of the caller;
  - lean, with only its persona;
  - read-only tools;
  - quiet: no bell, no "waiting on you", no say to clint;
  - shown under the caller on the board, with a watch link;
  - kept after the run as an ordinary done card, until the normal cull archives it.

  clint can open any reviewer and type into it mid-run, which a subagent never allowed.
- **The steps between reviewers are plain code, not a model:**
  - parsing each JSON array and checking it against its own prose;
  - dropping `preexisting` findings and those with an empty `prod_survival`;
  - de-duplicating on file and line;
  - the two lanes;
  - the merge.

  Only the verifiers and the critic are model calls, and they are cards too.
- **An outside PR keeps the PR runner's no-settings rule.** A target is **outside** unless it is proven ours, by a
  mechanical rule: it is a `pr_run`, or its checkout's origin owner is not on the room's list of clint's owners
  (`dovholuknf` and the orgs he names). A target that cannot be resolved counts as outside. Every panel card on an
  outside target launches with:
  - empty setting sources;
  - `Read`, `Grep` and `Glob` only;
  - the source added as a directory, never as its cwd.

  This is the rule @review's PR-hooks HIGH set for `prrunner.go` (8c9b17d7, a2a1909d). Without it, an ordinary card
  launch picks up the PR's own project settings and hooks.
- **The PR runner keeps its forks.** Forks of one prime read the diff from cache, so they are cheaper than cards.
  The PR runner already has them built and hardened. `atrium_review` is the same engine with a second way to run the
  reviewers: forks for a PR run (the story's 2.3), and cards for everything else, or for a PR run when clint asks to
  watch. One recipe format, one verify step and one report shape cover both. Section 6 says how this folds into the
  story.
- **Cause one, the redirect hook, is a dotfiles fix.** The hook that turns a subagent call into an atrium launch
  matches the tool name `Task`. Claude Code now calls that tool `Agent`, so the hook never fires, and review-panel's
  reviewers run as invisible in-process subagents again. The fix is to match both names. The same stale name is in
  m1mini's permission hook (`atrium-perm-hook.ps1:101`, whose skip list has `Task` and not `Agent`). There, every
  subagent call is sent to the permission gate, which `Task` never was.

## 1. What is there today

- **The review-panel skill** (dotfiles, 203 lines, per item16-notes) is a conductor. In order, it:
  1. captures the diff once;
  2. sizes the panel by file type and change shape;
  3. launches every reviewer in one message as `Agent` subagents;
  4. pre-filters, and sends one refuting verifier per blocking or high finding;
  5. checks each JSON array against its prose, de-duplicates on file and line, and runs a completeness critic;
  6. writes one report, with no fix offered.

  The hand-back is a fenced JSON array with these fields: severity, file, line, category, claim, evidence,
  preexisting, prod_survival, fix, confidence.
- **The personas** (`claude/agents/*.md` in dotfiles) carry `model:` in their frontmatter: opus for the steward,
  sonnet for the others. They are read-heavy by design, and the steward also has Bash, Write and Edit.
- **Subagents are not cards.** They do not show on the board, cannot be typed into, and leave only the parent's
  transcript. Their hand-backs woke the parent once each, which item 17 found. Item 17's fix keeps the card silent
  while subagents run, but each hand-back is still a parent turn.
- **The redirect hook** in dotfiles is the soft nudge named at `control_mcp.go:1183`. `DefaultLaunchCap` (10) is the
  hard backstop under it. With the matcher stale, only the cap is left.
- **`atrium_launch`** already takes `brief`, `runner`, `tags`, `lean`, `lean_agents`, `lean_skills`, `model`,
  `effort`, `args`, `env` and `room`. It sets the caller as `spawned_by`. It has no "run as this persona" field, no
  per-launch tool list, and no setting-sources override.
- **The PR runner** (`prrunner.go`) runs a prime in `<run>/work` with `--setting-sources ""`,
  `--tools Read,Grep,Glob` and `src/` through `--add-dir`. Its reviewers are forks of that prime. Verify and the
  second opinion are story stage 4, not built.
- **The room cap is 5 workers, shared by every director** (`docs/backlog/rnd/QUEUE.md`, "Where a worker runs"). A
  four-reviewer panel plus verifiers would take all of it.

## 2. The call

```
atrium_review {
  target:  { range: "claude/main..claude/fabric" } | { path: "docs/rnd/x.md" } | { pr_run: "<id>" },
  repo:    "<path to the checkout>",          // omitted for pr_run
  recipe:  "default",                         // the stored pr_recipe, or a named one
  panel:   ["codebase-steward", "go-security-reviewer"],   // optional, overrides the recipe's choice
  backend: "cards" | "forks",                 // default: forks for pr_run, cards otherwise
  room:    "<room>"                           // default: the caller's
}
-> { run: "<id>", cards: [...], watch: "<board link>", status: "running" }
```

The call returns at once. The report arrives later as one `atrium_report` to the caller, which carries:
- the path of `report.md` and `report.json` in the run folder;
- the counts by severity;
- one line per blocking or high finding.

The caller's turn can end, and nothing wakes it until then.

**Who may call it.** Any card, for its own work, and clint from the board (a "review this" action on a card's branch,
or a path). A card's call is a launch, so it is under the same caps and the same gate as `atrium_launch`.

## 3. The run

The room drives it, with a run record like the PR runner's, so a room restart resumes the run.

1. **Bundle.** The room writes `<run>/bundle.md`: the diff text of the range (or the file, for a path), the range so
   reviewers can widen context, the recipe's standing rules, and the reviewer files of review-memory. It is the same
   bundle the PR runner writes, from the same code.
2. **Panel.** The recipe picks the reviewers by the paths touched (story 2.2). The room reads each persona file's
   frontmatter for its model, and its body for its mandate.
   - **Where they are read from.** Personas come only from the room's own `~/.claude/agents/`, the source
     `lean_agents` already uses. Recipes come only from the store (`pr_recipe`).
   - **Nothing is read from the target.** Claude also loads a project's `.claude/agents/`, so an outside PR shipping
     `.claude/agents/codebase-steward.md` would otherwise write the reviewer's mandate. The room refuses a persona
     or recipe name whose path resolves inside the target's checkout. On an outside target, the empty setting
     sources keep claude from loading the project's agents at all.
3. **Launch, wave 1.** One job per reviewer, opened for that reviewer's standing persona with the run as its caller
   (section 7.6). The job's session is a card launched as follows:
   - `spawned_by` the caller;
   - tags `atrium:panel` and `panel:<run>`;
   - lean, with only that persona;
   - the persona's model;
   - its brief: the persona's body, the bundle's path, the hand-back schema, and "write your JSON to
     `<run>/findings/<reviewer>.json`, then `atrium_report` once, and exit";
   - tools `Read`, `Grep` and `Glob`.

   For an outside target, it also gets empty setting sources and the source added as a directory (section 0).
   `add_dir` takes only two paths, the run folder and the target's source, and refuses any other. The
   steward's Bash, Write and Edit are not given to a panel card. A reviewer that needs a build asks for `prove`,
   which is the walker's (story question 3) and not the panel's.
4. **Collect.** The room waits for every reviewer's report, up to a timeout (default 20 minutes). A reviewer that
   times out is recorded as `failed` in the report, never silently dropped. A reviewer's report goes to the run, not
   to the caller.
5. **Check, with no model.**
   - Each JSON file is parsed and checked against its prose. A finding whose prose and JSON disagree is marked.
   - Findings marked `preexisting`, or with an empty `prod_survival`, go to their own section.
   - What is left is de-duplicated on file and line, and the two lanes are formed.
6. **Verify, wave 2.** One verifier card per blocking or high finding. It is a card of the same persona as the
   reviewer that raised it, with the finding and "refute it or say what holds". It answers `holds`, `does not hold,
   because`, or `holds at <sev>`.
7. **Critic, wave 3.** One `general-purpose` card reads the merged list and the bundle, and names what the panel did
   not look at.
8. **Merge and report.** The room writes:
   - `report.json`: the review-panel hand-back array, plus `verified`, `reviewer` and `runner` per finding;
   - `report.md`: the skill's step 7 prose.

   It then sends the one `atrium_report` to the caller. The panel cards exit, and the run is marked done.

**The room cap.** Panel cards count against the room's shared cap of 5. To leave room for other directors, a run
never takes every free slot. It has its own **concurrency ceiling**, a recipe field `max_parallel` with a default
of 2 and a maximum of 3, and it queues the rest of its cards inside the run. It launches a card only when the run is
under its ceiling **and** at least one room slot stays free after the launch. So a panel never causes another
director's launch to be refused. When neither holds, the run waits, and the call's status says
`queued: room busy`. Waves 2 and 3 use the same ceiling.

**A queued run times out.** A run that has not started its first card within 30 minutes (a recipe field) ends as
`failed: no slot`. It sends one report to the caller saying so, and launches nothing later. Each reviewer still has
its own 20-minute limit once it starts.

**Quiet.** A card tagged `atrium:panel` raises no bell and no "waiting on you", and its reports do not reach clint.
The board shows it under its caller, collapsed into one "panel: 3 of 4 done" row with a link to each card. A panel
card's question (`ask`) goes to the caller, never to clint. **When the caller has exited or is parked**, the
question is not escalated to clint and does not wake the caller. The card is told "no one can answer; answer with
what you have, and name the open question in your findings". The run's report lists that question under "asked,
unanswered". The caller reads it when it next reads the report. A caller that is parked gets the report as an
ordinary queued report, without a wake.

**History.** Panel cards end as done cards, with their transcripts, and are culled with the run
(`docs/rnd/merged-cull-design.md`). The run folder keeps `bundle.md`, `findings/`, `report.*` and a `cards.json`
listing each card id. A review read a month later still opens each reviewer's conversation.

## 4. What it costs, against subagents and forks

- **Subagents (today):** each reviewer reads the diff and its sources in its own context, and the parent pays one
  turn per hand-back. On an Opus director at 200k to 336k context, that is $0.40 to $0.78 a hand-back
  (operator-focus 4.2), so $2 to $4 of the caller's turns on a five-hand-back panel, before the reviewers themselves.
- **Cards:** the reviewers cost the same as subagents. They are the same models doing the same reading. The caller
  pays **one** turn. So cards are cheaper than subagents by about the caller's turns, $1.50 to $3 a panel on a
  director.
- **Forks (the PR runner):** the reviewers read the diff from the prime's cache, so each saves its own read of the
  bundle, about $0.17 on 378's size (story question 5). That makes forks the cheapest. They are not watchable live
  and cannot be typed into, which is why they stay the PR runner's default and are not the general one.

## 5. Stages

Each is useful alone. All are held by the pause except D0.

| stage | what | owner | size | acceptance |
| --- | --- | --- | --- | --- |
| D0 | **The redirect fix.** Match `Agent` as well as `Task` in the dotfiles redirect hook, and add `Agent` beside `Task` in m1mini's `atrium-perm-hook.ps1` skip list, so a subagent call is handled as it was before the rename. Skipping `Agent` is right for the same reason as `Task`: the subagent's own tool calls are still gated at `PreToolUse` | clint or the orchestrator, in dotfiles | an hour | a review-panel run on any card shows its reviewers as atrium launches again (the nudge fires). On m1mini an `Agent` call raises no permission prompt, **and a `Bash` call made inside that subagent still prompts**, on the claude version installed that day |
| D1 | **The Go gate agrees.** `permSkipTools` in `internal/cli/hook_permission.go:33` lists `Task` and not `Agent`. Add `Agent`, with a test, so the binary and the dotfiles skip the same tools | @runtime | one line and a test | the hook test passes for `Agent` as for `Task`, and a test that a subagent's `Bash` is still gated passes |
| V1 | `atrium_launch` gains `tools`, `setting_sources`, `add_dir` and `persona`. `persona` reads an agent file for the model and brief. On an outside target (section 0's rule) the launch refuses any `setting_sources` other than empty, refuses an `add_dir` other than the run folder and the source, and refuses a `persona` that does not resolve under the room's `~/.claude/agents/` | @runtime | 1 day | a card launched with `tools: [Read,Grep,Glob]` and `setting_sources: ""` in a checkout with a `.claude/settings.json` hook does not run the hook, and a `Bash` call is refused. A checkout carrying `.claude/agents/codebase-steward.md` gets the room's persona, not its own. A `persona` path inside the checkout is refused |
| V2 | `atrium_review` with the cards backend: the bundle (reusing the PR runner's code), the panel from the recipe, waves 1 to 3, the checks of step 5, the merge, the one report, the run's ceiling and queue, the queued-run timeout, and quiet | @runtime | 3 days | a four-reviewer review of a real branch range ends with one report to the caller and **one** caller turn, as counted in `session_usage`. Its `report.json` parses in the review-panel shape, each panel card is done with its transcript, and the board showed one panel row. With 3 room slots free and `max_parallel` 2, at most 2 reviewers run at once and one room slot stays free throughout. A run started with no free slot ends `failed: no slot` after its timeout and launches nothing later. A panel ask after the caller has exited is listed under "asked, unanswered" and reaches no one else |
| V3 | The board: the panel row under its caller, the "review this" action on a card's branch and on a path, and the run's report view (the pulls-view drawer's findings list, without the walk) | @ui | 2 days | clint starts a review from a card's branch, watches one reviewer live and types into it, and reads the merged report on the phone |
| V4 | The forks backend behind the same call: `pr_run` targets call the PR runner. Verify and the critic come from story stage 4, with the merge shared with V2 | @runtime, with story stage 4 | 1 day on top of stage 4 | `atrium_review {pr_run}` gives the same `report.json` shape as a cards run, and the walker reads it unchanged |
| V5 | The review-panel skill calls `atrium_review` instead of launching subagents, so `/review-panel` and a director's review both go through it | clint or the orchestrator, in dotfiles | an hour | `/review-panel` in a card runs the panel as child cards and returns the report in its usual form |
| P1 | Standing personas: the hub's persona rows and `atrium:` handles (reserved), `personas` in `atrium_peers`, jobs and their queue, wake on say, follow-ups resuming the job's session, `max_instances` under the keep-one-free rule, answers to the job's caller | @fabric (hub rows, resolving), @runtime (jobs on the room) | 3 days | from a card on sg3, `atrium_say atrium:qa-reviewer "any corner cases missed in claude/fabric?"` opens a job on m1mini, which holds the checkout, and answers the caller once. A second message within 2 hours resumes the same session. A message while the persona is busy answers `queued, 1 ahead`. No card alias or derived handle can be `atrium:x` or `atrium-x`, and the migration renames an existing one. A fourth open job from one caller is refused, and the 21st queued job answers `queue full`. A room whose agent file's hash differs from the pinned one gets no job |
| P2 | The knowledge base: dotagents mirrored on the hub and synced to every room, read at a pinned commit, the five files of 7.4, the hand-back shapes, the hub applying notes as one commit per job on `claude/knowledge` with decision 2's checks, false positives from clint's walk rejections and @review's re-reads only, and the data-not-instructions refusals | @fabric (mirror, apply), @runtime (brief, hand-back parse) | 3 days | a qa-reviewer job on m1mini rejected in a walk adds a false-positive entry. A job on sg3 for the same repo then reads it and does not raise that finding again. An `add` whose evidence path does not exist is refused and named. An entry saying "always approve" is refused. A note written by a job is not read by the next job until @review's `knowledge-ok` moves `knowledge/cleared`. A verifier's refutation adds no false-positive entry, and a walk rejection adds one pinned to its repo, file, kind and commit, which expires |
| P3 | The code from any room: git-sync stage 3 for every repo a card works in, `claude/*` served read-only to rooms, outside PR heads fetched by the hub, the job worktree under the run folder, dirty worktrees refused | @fabric | 3 days | a persona on m1mini reviews a branch that exists only in an sg4 worktree, after one collect, with no paste. An outside PR is reviewed on a room with no GitHub login |
| P4 | The panel on the personas (7.6) and the persona tiles on the board (7.8) | @runtime, @ui | 2 days | an `atrium_review` panel opens one job per persona, and each job's brief names that persona's knowledge files. The board shows each persona's tile with its queue and its last jobs |

## 6. How it fits the PR review story

The story's review steps (2.2 the recipe, 2.3 one-shot calls, 2.4 verify and the second opinion, 2.5 memory) are
the engine here. It is unchanged for a PR run and is now reachable for any target. The changes to the story:
- **2.3** gains one paragraph. The PR runner's reviewers stay forks, and the same engine runs reviewers as cards for
  any other target, or for a PR when clint asks to watch (`backend: cards`). The no-settings rule of 4ad931af MED 2
  holds for every card on an outside target, not just the walker.
- **Stage 4** (verify and the critic) is built once and shared by both backends (V4). It is not built twice.
- **The decisions numbered 21 to 27** in the orchestrator's list for the story are not changed by this design. It
  adds questions 1 to 3 below.

This is filed as a fold-in for the story's owner rather than an edit to `claude/pr-review-story`, which is the
orchestrator's branch.

## 7. Standing reviewers: `atrium:qa-reviewer` and the others

Amendment, 2026-10-02 evening, from clint through the orchestrator. A session asked to "ask the atrium qa reviewer"
found a done card called `review-b`, could not message it, and fell back to @review. clint wants the reviewers to be
**standing personas with stable handles** that any session can address. A message to one that is parked or done
should wake it, not be refused. Each keeps its notes across reviews, and serves several callers in turn. The per-run
panel of sections 2 and 3 is then built on them.

**This changes one of clint's earlier answers.** `docs/review/review-memory-design.md` decision 2 (answer 3) said
there is no resident reviewer, and reviewers start fresh for every review. Under this amendment the reviewer is
resident as a **handle, a queue and its memory**, and each review is still a fresh session. Decision 2's other rule
stands: no persona edits its own memory. The hub applies its notes after each job with decision 2's mechanical
checks, and @review reads the diff (7.4, question 4).

### 7.1 The handles

- **One persona per agent file** in the room's `~/.claude/agents/`, the trusted source of section 3 step 2.
- **The handle is `atrium:<name>`.**
  - The `atrium:` prefix is reserved. A card's own alias can never start with it, so `atrium:qa-reviewer` can never
    resolve to a stray card the way `review-b` did. The reservation also covers the wire handle atrium derives from
    a title (`atrium-qa-reviewer` and any other spelling of the prefix), so a card titled "atrium: qa reviewer"
    cannot take the name either.
  - A migration renames any existing card alias or handle that starts with the prefix, adding the card's short id,
    and notes the change on that card.
  - This handle namespace is separate from the `atrium:` names `lean_agents` uses for agent and skill files. A persona
    handle never names an agent file, and an agent name never resolves as a persona.
  - The name is the agent file's name unless the persona table gives a short one. The defaults are:

    | handle | agent file |
    | --- | --- |
    | `atrium:qa-reviewer` | functional-tester |
    | `atrium:perf-reviewer` | nonfunctional-tester |
    | `atrium:c-reviewer` | c-systems-reviewer |
    | `atrium:go-security` | go-security-reviewer |
    | `atrium:codebase-steward` | codebase-steward |

- **They are listed.** `atrium_peers` gains a `personas` list: each handle, what it reviews in one line from the
  agent file's description, and its state (idle, working, queued N). So a session told to "ask the qa reviewer" finds
  `atrium:qa-reviewer` by reading the list, not by guessing at card names.

### 7.2 Resolving from any room

A persona is a row on the hub, not a card:
- the handle;
- the agent file;
- the rooms that carry that file, each with the file's hash. Each room reports its agents list with a hash per file,
  as it reports its requirements. The persona row pins the hash the operator accepted, and a job goes only to a room
  whose copy matches it. A room with an edited or older copy is not used, and the board says which;
- `max_instances`;
- its queue;
- its current sessions.

A message to `atrium:qa-reviewer` from any room goes to the hub:
1. The hub opens a **job**: the caller, the question or target, the time, and the change it is about (the change
   record's id when there is one).
2. It picks a room for the job:
   - the one holding the target's checkout, when that room carries a matching agent file;
   - otherwise the matching room with the most free slots.
3. It queues the job there. The answer to the sender is `queued: job <id> on atrium:qa-reviewer, 1 ahead`. A message
   is never refused because the persona is parked, done or busy. It is refused only by the bounds below.

**Bounds, so a looping or injected card cannot fill a persona with paid sessions:**
- **Per caller:** at most 3 open jobs per persona, and 10 across all personas. A fourth answers `refused: 3 open jobs
  on atrium:qa-reviewer, wait for one`.
- **Per persona:** a queue of at most 20. The 21st answers `queue full, 20 ahead`.
- **The say rate limit** (`peerLimit`) applies to messages to a persona as to any card.
- **clint's jobs** from the board or the phone are outside the per-caller cap, and still inside the queue cap.

`atrium:qa-reviewer@m1mini` pins the room. A persona no room carries is refused with the list of rooms and what each
carries.

### 7.3 Wake on say: a session per job, the queue in atrium

- **Each job runs in its own session**, a card of that persona with the job's run folder as its cwd. That is section
  3's launch, unchanged:
  - lean, with its persona;
  - read-only tools;
  - for an outside target, empty setting sources and the source only as `add_dir`.

  An outside target needs a fresh cwd per job, so a single long-lived session could not keep the isolation. It
  would also grow a large context, and today's cost check showed that such contexts are the expensive part
  (operator-focus 4.3).
- **Follow-ups resume the job's session.** A second message from the same caller to the same persona within 2 hours,
  about the same target, resumes that job's card with `resume`, so "and what about the empty-list case?" keeps the
  thread. `new:` at the start of a message forces a new job.
- **Parked or done is never a refusal for a persona.** A follow-up to a job whose card is parked resumes it. One
  whose card is culled starts a fresh session with the job's notes and findings in the brief. A message to a
  persona never needs `wake=true`, because waking is what was asked for.
- **The queue is in atrium, not in a model.** The room feeds a persona's session one job at a time. Waiting jobs are
  rows, so they cost no tokens and survive a restart. A queued job times out after 30 minutes, with one message
  back to its caller, as a queued run does (section 3).
- **Answers go to the job's caller**, not to whoever first started the persona. A standing persona has no single
  launcher. The answer is one `atrium_report`, or for a question its one reply, to the caller of that job. The caller
  is not woken by anything else.

### 7.4 The knowledge base: what each persona learns, kept on the hub

clint: the reviewers should not be throwaway, they should build up a knowledge base. **The memory is the point of a
standing persona.** A fresh session per job (7.3) is how the knowledge is read cleanly each time, not a reset.

**What a persona keeps.** Each is a file per persona, with every entry carrying the evidence and the commit it was
true at (review-memory decision 2's entry format):

| file | holds |
| --- | --- |
| `personas/<id>/NOTES.md` | cross-repo lessons: how clint wants findings worded, which checks pay off |
| `personas/<id>/repos/<host>/<org>/<repo>.md` | per repo: where the core is, the invariants, the test layout, the dependency versions that matter (decision 2's file, unchanged) |
| `personas/<id>/authors/<host>/<login>.md` | per author, about the code only: the patterns their changes tend to get wrong or right, and what they asked reviewers to stop flagging. Nothing personal. Kept only for repos clint names (question 5) |
| `personas/<id>/bugs/<kind>.md` | per kind of bug (a lock order, an unchecked error, a resource leak): what it looked like, where it was found, and the test that would have caught it |
| `personas/<id>/false-positives.md` | findings the persona was told off for by a person or by @review: rejected in clint's walk, or marked "not a bug" in @review's re-read. **Never from a verifier**, whose refutation is model output over PR text. Each entry is pinned to a repo, a file, a kind of finding and a commit, carries the reason and the evidence, and expires after 90 days or when that file changes substantially. So one rejection cannot quiet a whole kind of bug everywhere |

**Where it is kept: on the hub, so a persona woken on any room has it.** The files stay in dotagents, which is
clint's layout (decision 2, answer 1). The hub holds dotagents' mirror, the way it holds atrium's, and every room
syncs it read-only.

**Nothing unread is ever read.** A job reads the knowledge at the last commit @review cleared, never at the tip of
what the hub wrote:
- the hub writes to `claude/knowledge`;
- @review's batch read ends with a review commit carrying `Atrium-Verdict: knowledge-ok <sha>`. The hub accepts it
  only on a commit that touches review files alone (deployready's `reviewFile` rule) and that it collected from
  @review's own `claude/review` branch. So no other card can move `knowledge/cleared`;
- the hub then fast-forwards `knowledge/cleared` to that sha;
- jobs read `knowledge/cleared`, pinned at the job's start.

So a note written by one job reaches later jobs only after @review has read it.

**How it grows, after every job, with no model in the write.**
- A session hands back `repo_notes` (decision 2's contract), plus `persona_notes`, `author_notes` and `bug_notes` in
  the same add/drop shape.
- clint's walk rejections and @review's re-read "not a bug" lines become `false-positives` entries automatically,
  from the run's own files, pinned and expiring as in the table above. Verifier refutations do not. They only change
  that one run's report.
- **The hub applies them** as one commit per job on the mirror's `claude/knowledge` branch, after decision 2's
  checks. All of them are mechanical:
  - an `add` needs an evidence path that exists at its commit;
  - a `drop` must match exactly one entry;
  - a file stays under its line cap (about 150), with the oldest commit pruned first.
- A note that fails a check is refused and listed in the job's answer.
- **@review reads the `claude/knowledge` diff** in a batch, as it reads any commit, reverts what it rejects, and
  clears the rest with `knowledge-ok`. That keeps decision 2's rule: no persona edits its own memory, and nothing
  takes effect until it is a reviewed diff. It does so without making @review a model turn on every job.
- Whether `claude/knowledge` is pushed to dotagents' own remote is clint's (question 6). Until he says, it stays on
  the hub.

**It is data, not instructions.** Notes are written from hand-backs that can quote an outside PR. So:
- the brief marks the knowledge files as data;
- an entry can never widen a persona's tools or change its gate;
- the hub refuses an entry whose text holds a path outside the repo or an instruction-shaped line ("ignore", "always
  approve") and names it in the answer.

### 7.5 Concurrency, under the shared cap

- **One session per persona at a time by default.** `max_instances` (default 1, at most 3) allows more when a
  persona's queue is long.
  - A second session starts only when at least two jobs wait, the oldest has waited 5 minutes, and the room keeps
    one slot free after the launch (section 3's rule).
  - Instances share the handle. The board shows "qa-reviewer: 2 working, 1 queued".
- **Persona sessions count against the room's cap of 5** like every card, under the same keep-one-free rule. A
  persona never causes another director's launch to be refused. It waits instead.
- **Order.** Jobs are taken first in, first out, with one exception: a job from clint (the board or his phone) goes
  to the front.

### 7.6 The panel, rebuilt on the personas

`atrium_review` (section 2) keeps its call and its report. Section 3 step 3 changes. Each reviewer is now **a job
for that persona**, opened by the run, with the run as its caller. The run still:
- caps itself at `max_parallel`;
- collects the findings files;
- runs the checks with no model;
- runs verify (a job for the same persona) and the critic;
- merges, and sends the one report.

What is gained:
- one session type instead of two;
- every panel reviewer reads its persona's notes;
- a session can ask a single persona a single question with no panel at all.

### 7.7 Reaching the code from any room

A reviewer on m1mini could not read a change that lived only in a worktree on sg4 (no route), and asked for a
paste. Three ways to close that:
- (a) the hub mirrors every repo a card works in and serves its `claude/*` branches, plus outside PR heads, to every
  room (`docs/rnd/hub-forge-design.md`, git-sync stage 4);
- (b) a room reads another room's files (room-to-room read, decisions 34 to 37);
- (c) the persona always runs on the room that has the code.

**Default: (a), the hub serves the code.** It is the only one that works for every case: our unpushed branches on
any room, an outside PR, and a room with no GitHub login.
- **Our branches.** The hub already collects every room's `claude/*` branches for the atrium repo (git-sync stage
  1). Stage 3 extends that to every repo a card works in. It adds a mirror on the hub when the first card on a room
  works in a repo, and then lets rooms fetch `claude/*` from it read-only.
- **Outside PRs.** The hub fetches `refs/pull/<n>/head` from the forge into its mirror, with its own forge login.
  The persona's room fetches from the hub. Rooms need no GitHub credentials to review, and a pull, clone or fork is
  always the hub's, which is what clint asked for.
- **PR heads stay under `refs/pull/*`.** Landing and collect never write or take that namespace, so an outside PR's
  head can never become a room's branch or `claude/main`.
- **The job's checkout.** The persona's room makes a worktree from the hub's mirror under the job's run folder, at
  the commit the job names. It is read-only to the session and added only as `add_dir` (section 3 step 3).
  It is removed with the run.
- **Uncommitted work is not served.** A job about a dirty worktree is refused with "commit it first". This is the
  same rule as a move.
- **(c) stays as a preference, not a route.** 7.2 already picks the room that holds the checkout when it carries the
  agent file and has a slot. That saves a fetch. (b) is for files that are not code (logs, run folders), and is not
  needed here.

Until stage 3 is built, a persona job about code on another room runs there (c), or is refused with the room that
has it. It is never answered with a request for a paste.

### 7.8 The board

Each persona is one standing tile, not a card that comes and goes:
- its handle and its one-line description;
- its state;
- its queue, with each job's caller and age;
- its last 20 jobs, each opening that job's session (watch, or type into it while it runs).

A job's session is also shown under its caller, as a panel card is (section 3, "Quiet"). Persona sessions are
quiet: no bell and no "waiting on you". A persona's question goes to the job's caller. When the caller is gone, it
goes into the job's answer as "asked, unanswered".

## 8. Questions for clint, in plain words

1. **The quick fix in your settings repo.** A rename in Claude Code broke the rule that sends a reviewer helper
   through atrium, so reviews run as invisible helpers again. The fix is one word in two files in your dotfiles.
   Make it now? It is a settings change, not an atrium build or deploy. **Suggested: yes, now.**
2. **Reviewers as cards.** For a review of our own branches and docs, run each reviewer as a quiet card you can
   watch and type into. It costs about the same as today's helpers and saves the asking director $1.50 to $3 a
   review. **Suggested: yes.**
3. **PR reviews stay on the cheaper path.** Reviews of outside pull requests keep the faster, cheaper path that can't
   be watched live, with the cards option when you want to watch one. **Suggested: yes.**
4. **Who writes what the reviewers learn.** After each review, atrium files the reviewer's notes by itself, using
   fixed checks, but no reviewer uses them until @review has read and cleared them in its next batch. Notes that
   say "this was not a bug" come only from you or from @review, never from another model. **Suggested: yes.**
5. **Notes about authors.** May a reviewer keep notes on what a particular author's changes tend to get wrong, about
   the code only? That would apply only on repos you name, such as openziti's, and never on people outside them.
   **Suggested: yes, on the openziti repos only.**
6. **Where the knowledge base lives.** It goes in your dotagents repo, on a branch held on the hub. Should that
   branch also be pushed to dotagents' own remote, or stay on the hub? **Suggested: stay on the hub until you have
   read a week of it.**
7. **How a reviewer gets code from another machine.** The hub keeps a copy of every repo a card works in, and
   fetches outside pull requests itself, so any machine can review any change without a paste or its own GitHub
   login. **Suggested: yes.**
