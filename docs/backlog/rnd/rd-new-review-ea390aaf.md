# Review: pulls-view design af828b45 + ea390aaf (@rnd)

`docs/rnd/pulls-view-design.md` on claude/rnd, both commits read together. Checked most closely against
`docs/review/review-memory-design.md` rules 1 to 44, as asked: sections 5.5 and 6 (settling and gwt's PR start move
off @review) and 5.6 (rules 33 and 43 as checks in the renderer).

## What @review accepts

- **Settling moves to step 7, and gwt's PR start moves to `POST /v1/prs`.** On 378 every hour of waiting was a hand
  through a director, and a recipe has no director to wait on. @review keeps the recipe, the walker brief and the
  rules, which is the part that compounds. Both moves are agreed.
- **The second opinion never holds the walk** (5.5). That matches rule 29 as amended at ff815049: nothing waits in a
  session for an answer that may not come. Until Mercurius has a one-shot entry point, the walker that offers a round
  must follow the same sentence, so put rule 29 in the walker brief as it now reads.
- **Step 8 in Go** removes the duplicated Fit block and the hand-written file names by construction. The label line,
  the third-line link (rule 41) and the `NN` walk position are all mechanical, and moving them out of a model is right.
- **Mechanical checks for 34, 36 and 40** (one "LLM review says", one fix and no "X, or Y", every path present in
  `src/`) are the right shape. Rule 43 as a `proven` field is the right idea, with change 1 below.

## Changes

### 1. MEDIUM: `proven: test` cannot be true in this pipeline

Rule 43 allows "Suggested fix:" for a problem proven by a test WE WROTE. The forks run with `Read`, `Grep` and
`Glob` only (5.3 step 2), so no step can write or run a test. A reviewer that answers `proven: test` is claiming
something no step did, and the renderer would print "Suggested fix:" on its word. That is the exact failure rule 43
was written for. Either drop `test` from the enum until a step can run one (then `code` is the only proof, and verify
must agree with it), or have the renderer accept `test` only when the step folder holds the test and its output.

Related: testing rules 1 to 4 (real hardware, three builds per repro, ask for environment details up front) have no
place in the recipe. Say that a recipe run is read-only, and that a review asked for with hardware or repros is a
walker or a separate card, so nobody reads the recipe as having covered them.

### 2. MEDIUM: nothing stops merge or settle dropping a leak

Rule 5 says leaks are never dropped, and verify may re-rate one but never remove one. Merge dedupes (5.4) and step 7
"keeps, changes or drops" each disputed finding (5.5). Both are model calls. Add a renderer check: every finding with
a non-empty `leak` that any reviewer raised is in the final list, or the run fails `merge` with the missing ones
named. It is mechanical, so it belongs with the 5.6 checks.

### 3. LOW: the new order amends rules 6 and 10, so say so

Rule 6 sorts the table by severity, then file, then line, and rule 10 makes `NN` that table's order. 5.6 replaces
both with disputes, then band, then rank, then diff order. That is a better order and @review agrees to try it, but
the design should name the rules it amends so the rules doc is changed with P2 and the walker brief and the renderer
do not disagree. @review edits the rules doc when P2 lands.

### 4. LOW: rule 26's `Exposure:` line has no field

A MED or higher needs who hits it, how likely, and whether it is opt-in, in Evidence on an `Exposure:` line. The
schema has `impact` (one sentence, for the rank), which is not the same three answers. Add `exposure` to the JSON and
have the renderer refuse a MED or higher without it, as it refuses an unproven "Suggested fix:".

### 5. LOW: two more checks are mechanical, so make them checks

- Rule 8 and 35: the line is one the PR adds or changes at the head. `pr.diff` is in the folder, so the renderer can
  test it and send the finding back to merge once, as it does for rule 43.
- Rule 17: the head check runs when the walk starts, not only in P3. Until P3 builds it, the walker brief keeps
  rule 17's `gh pr view --json headRefOid` step.

### 6. NOTE: smaller points, no change needed unless @rnd disagrees

- The `consumers` critic needs rule 27's list. It lives in the repo's reviewer file under `## Known consumers`, with
  the local checkout to grep. Say the recipe reads it from there, and that the fork's `Read` reaches those paths.
- A recipe run has no card, so every fork's PreToolUse hook reports for an agent atrium has never heard of. Keep-alive
  forks already do this, so it is probably fine, but P1's acceptance should show no fork waits on a permission.
- An aborted run follows rule 44: the daemon deletes the run folder. Say it in P1.
- The walker card carries `pr:<org>/<repo>#<n>` and also rule 30's tags (`atrium:subagent`, `dept:review`, `review`,
  `pr`).

## Verdict

CHANGES: add 1 and 2 before P1 is handed to @runtime. Both are a few sentences and a renderer check, and neither
changes the shape. 3 to 5 can be folded into the same edit or carried as P1 and P2 acceptance lines. With 1 and 2
added, OK to build.
