# Review on atrium: one call runs the reviewer panel as quiet child cards and returns one merged report

Status: design by @rnd, 2026-10-02. Nothing built. Asked through the orchestrator: make review through atrium the
easiest path. One call should launch the specialist reviewers (c-systems-reviewer, functional-tester,
codebase-steward and the others) as atrium cards that are children of the caller, quiet, watchable and with their
history kept. It should collect their findings and return one merged report, in the shape the review-panel skill
returns today.

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
- **An outside PR keeps the PR runner's no-settings rule.** For a target whose code is not ours (a PR run, or any
  repo outside clint's own), every panel card launches with:
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
3. **Launch, wave 1.** One card per reviewer:
   - `spawned_by` the caller;
   - tags `atrium:panel` and `panel:<run>`;
   - lean, with only that persona;
   - the persona's model;
   - its brief: the persona's body, the bundle's path, the hand-back schema, and "write your JSON to
     `<run>/findings/<reviewer>.json`, then `atrium_report` once, and exit";
   - tools `Read`, `Grep` and `Glob`.

   For an outside target, it also gets empty setting sources and the source added as a directory (section 0). The
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

**The room cap.** Panel cards count against the room's cap. The run launches as many as there are free slots and
queues the rest, so a panel never refuses another director's launch. When no slot is free, the call says
`queued: room at cap` and starts when one frees. Waves 2 and 3 reuse the slots wave 1 frees.

**Quiet.** A card tagged `atrium:panel` raises no bell and no "waiting on you", and its reports do not reach clint.
The board shows it under its caller, collapsed into one "panel: 3 of 4 done" row with a link to each card. A panel
card's question (`ask`) goes to the caller, never to clint.

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
| D0 | **The redirect fix.** Match `Agent` as well as `Task` in the dotfiles redirect hook, and add `Agent` beside `Task` in m1mini's `atrium-perm-hook.ps1` skip list, so a subagent call is handled as it was before the rename | clint or the orchestrator, in dotfiles | an hour | a review-panel run on any card shows its reviewers as atrium launches again (the nudge fires), and on m1mini an `Agent` call raises no permission prompt |
| V1 | `atrium_launch` gains `tools`, `setting_sources`, `add_dir` and `persona`. `persona` reads an agent file for the model and brief. The launch refuses `setting_sources` other than empty for a card whose cwd is outside clint's repos | @runtime | 1 day | a card launched with `tools: [Read,Grep,Glob]` and `setting_sources: ""` in a checkout with a `.claude/settings.json` hook does not run the hook, and a `Bash` call is refused |
| V2 | `atrium_review` with the cards backend: the bundle (reusing the PR runner's code), the panel from the recipe, waves 1 to 3, the checks of step 5, the merge, the one report, the cap queue and quiet | @runtime | 3 days | a four-reviewer review of a real branch range ends with one report to the caller and **one** caller turn, as counted in `session_usage`. Its `report.json` parses in the review-panel shape, each panel card is done with its transcript, and the board showed one panel row. With 3 slots free, the fourth reviewer starts when the first ends |
| V3 | The board: the panel row under its caller, the "review this" action on a card's branch and on a path, and the run's report view (the pulls-view drawer's findings list, without the walk) | @ui | 2 days | clint starts a review from a card's branch, watches one reviewer live and types into it, and reads the merged report on the phone |
| V4 | The forks backend behind the same call: `pr_run` targets call the PR runner. Verify and the critic come from story stage 4, with the merge shared with V2 | @runtime, with story stage 4 | 1 day on top of stage 4 | `atrium_review {pr_run}` gives the same `report.json` shape as a cards run, and the walker reads it unchanged |
| V5 | The review-panel skill calls `atrium_review` instead of launching subagents, so `/review-panel` and a director's review both go through it | clint or the orchestrator, in dotfiles | an hour | `/review-panel` in a card runs the panel as child cards and returns the report in its usual form |

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

## 7. Questions for clint, in plain words

1. **The quick fix in your settings repo.** A rename in Claude Code broke the rule that sends a reviewer helper
   through atrium, so reviews run as invisible helpers again. The fix is one word in two files in your dotfiles.
   Make it now? It is a settings change, not an atrium build or deploy. **Suggested: yes, now.**
2. **Reviewers as cards.** For a review of our own branches and docs, run each reviewer as a quiet card you can
   watch and type into. It costs about the same as today's helpers and saves the asking director $1.50 to $3 a
   review. **Suggested: yes.**
3. **PR reviews stay on the cheaper path.** Reviews of outside pull requests keep the faster, cheaper path that can't
   be watched live, with the cards option when you want to watch one. **Suggested: yes.**
