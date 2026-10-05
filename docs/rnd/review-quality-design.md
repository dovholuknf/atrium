# Review quality: fewer, better findings, and a measure of whether they were worth raising

Written by @rnd on 2026-10-05, for `docs/backlog/rnd/rnd-new-review-quality.md`. Design only. Nothing here is built.
It builds on the PR review workflow in `docs/rnd/pr-review-workflow.md`, which is built and on claude/main c9d65765.

## 0. The answer

The PR runner raises every finding a model thinks of and keeps every finding a verifier does not reject. It has no
cut, no memory of an earlier round and no count of what clint did with what it raised. Four changes, in this order:

1. **Q1, the bar and the cut.** The rules every reviewer reads say what a finding is and that "none" is a good
   answer. The verifier gives each finding a confidence. Go, not a model, moves a finding under the recipe's cut out of
   the walk into `dropped.json`, which the walk drawer shows folded.
2. **Q2, the outcome count.** Each finding's end (worth raising, not worth raising, or undecided) is stored per
   reviewer. The pulls view shows each reviewer's addressed rate over 30 days.
3. **Q3, round two reviews only what is new.** A paste at a new head of a PR that was reviewed before reviews the
   commits since, and carries the earlier findings whose hunks did not change, with their walk state.
4. **Tools wait.** Running a repo's own linters runs code the PR controls. That needs the sandbox the change
   lifecycle's tested gate will have, and a base tree the runner does not fetch. Section 6 says what lands then.

The backlog's "later" items 6 to 12 are placed in section 7. Two of them wait on Q2 or Q3 and one is rejected.

## 1. What is there

| backlog item | today, on claude/main c9d65765 |
| --- | --- |
| 1, a bar | Half there. `prRules` in `internal/daemon/prrunner.go` says "a defect only on a line the PR adds or changes" and "name the line". Nothing says "the author would fix it" or "no more rigour than the codebase", and nothing says an empty list is a good answer. Every finding carries `cause: introduced or pre-existing`, and nothing reads it |
| 2, addressed rate | Missing. The walk writes each finding's state to `walk.txt` in the run folder (open, accepted, posted, dismissed, deferred) and the finding file says `Raised by:`. Nothing joins the two or keeps them past the run folder |
| 3, tools | Missing. The fetch step is a blobless shallow fetch of the head into `src/`. There is no base tree |
| 4, confidence | Missing. Verify answers `holds`, `does_not_hold` or `holds_at` with a severity, plus `proven`. Verify runs only on findings at or above the recipe's `verify_at`, and a failed verify fork leaves its findings unverified |
| 5, incremental | Missing. The workflow design says a new head is a new paste and a new row, reviewed in full |

## 2. Q1: the bar and the cut

### 2.1 The bar, in the rules every reviewer and critic reads

`prRules` gains four lines, after the line about changed lines:

```
A finding must be introduced by this change, be one discrete problem on one named line, be something the author
would fix if told, and ask for no more rigour than the code around it already has. Do not report style, formatting
or anything a linter would catch. "findings": [] is a good answer when nothing meets this bar. Rate sev by the
rubric: high, a user loses data, money, access or a process. med, a user sees a wrong result or an error.
low, a maintainer is misled or a later change is harder. nit, nothing breaks.
```

The rubric is fixed and written down because the survey found a model's own sense of severity does little better
than chance. Severity stays a label. It orders the walk and never removes a finding.

### 2.2 Code that enforces what it can

The renderer (`internal/prreview/render`) gains one rule, `RuleCause`: a finding whose `cause` is `pre-existing`
leaves the walk and goes to `dropped.json` with the reason `pre-existing`. A leak is the one exception, as every rule
already makes it: a pre-existing leak stays, because rule 5 never drops a leak.

### 2.3 Confidence and the cut

- **Verify answers a confidence** from 0 to 100 on each verdict, as `"confidence": 85`, with one line added to the
  verify prompt: "confidence is how sure you are that the finding holds and the author would fix it".
- **Verify runs on every finding** when the recipe has a cut, not only on those at or above `verify_at`. A finding
  with no verdict cannot be cut, so a cut that only verify can set needs verify to see everything. `verify_at`
  keeps its meaning when the cut is 0. The cost is one or two more verify forks on a PR with many lows, on the shared
  cached prime.
- **The merge step keeps the confidence** it was given and is told never to change it. When two findings are merged,
  the higher one is kept.
- **Go applies the cut** after merge, in `render`, before the files are written. A finding under the cut goes to
  `dropped.json` with its confidence and the reason `under the cut`. A finding with no verdict, because a verify fork
  failed, is not cut. It stays in the walk with "not verified" on its header, since dropping it would hide a verify
  failure as a quiet review.
- **The cut is on the recipe.** A migration at the END of `internal/store/schema.go` adds
  `ALTER TABLE pr_recipe ADD COLUMN confidence_cut INTEGER NOT NULL DEFAULT 80`. 0 turns the cut off. It is edited in
  the recipe settings with the other fields.

### 2.4 What was dropped stays one tap away

- `dropped.json` sits in the run folder beside `findings/`, as a list of the full finding objects plus `reason` and
  `confidence`.
- `GET /v1/prs/{id}/findings` gains `dropped`, the same list, so the hub's raw pass-through carries it untouched.
- The walk drawer shows a folded "dropped (n)" row after the last finding. Opening it lists each one with its reason
  and confidence. A dropped finding has one action, "bring back", which writes it to `findings/` as an open finding
  and records `revived` in the outcome count (section 3). That is the signal that the cut is too high.

### 2.5 Acceptance for Q1

- A run on a fixture PR with a pre-existing finding and a finding verified at 60 writes both to `dropped.json` and
  neither to `findings/`. A pre-existing leak stays in `findings/`.
- With `confidence_cut` 0, nothing is cut and `verify_at` decides what is verified, as today.
- A failed verify fork leaves its findings in the walk, marked "not verified".
- The drawer shows "dropped (2)", and "bring back" makes an open finding the walk can post.
- A run whose panel returns no findings ends `done` with an empty walk, and the pulls row says "no findings".

## 3. Q2: the outcome count

### 3.1 What counts

| walk state, or event | outcome |
| --- | --- |
| accepted ("fix it") or posted | addressed |
| a later round finds the finding's hunk changed (section 4) while it was open or accepted | addressed |
| dismissed | not addressed |
| deferred, or open when the PR is archived | undecided, left out of the rate |
| brought back from `dropped.json` | revived, counted against the cut and not against a reviewer |

**The rate** for a reviewer is addressed divided by addressed plus not addressed, over findings whose outcome was set
in the last 30 days. Critics count under their own names, `critic:coverage` and `critic:consumers`. A merged finding
counts for the reviewer named in its `raised_by`, which is the one the merge built on.

### 3.2 Where it is kept

The run folder is not enough: it is per head, a moved review archives its old row, and 30 days spans many folders. A
migration at the END of `internal/store/schema.go` adds a room-local table:

```
CREATE TABLE pr_finding_outcome (
  review_id TEXT NOT NULL, finding_key TEXT NOT NULL, raised_by TEXT NOT NULL,
  sev TEXT NOT NULL, confidence INTEGER NOT NULL DEFAULT -1,
  outcome TEXT NOT NULL, via TEXT NOT NULL, at TEXT NOT NULL,
  PRIMARY KEY (review_id, finding_key))
```

- The walk route writes the row when a state changes, in the same handler that writes `walk.txt`. A state changed
  back to open deletes the row.
- `via` is `walk`, `commit` (section 4) or `revive`.
- The PR move export from `f-pr-review-move` carries the rows with the review, so a moved review keeps its counts.

### 3.3 Where it is shown

- `GET /v1/prs/rates` answers, per reviewer, `addressed`, `not_addressed`, `revived` and `undecided` for 30 days. The
  hub merges it across rooms by summing the counts, beside the merged `GET /v1/prs` in `internal/link/pulls.go`.
  Counts are summed, never rates.
- The pulls view's recipe settings, where the panel is edited, list each reviewer with "7 of 23 addressed (30%)".
- A reviewer with at least 20 decided findings and a rate under 25% is marked "below the floor". Nothing is sent to
  anyone. @review reads the same view, and the floor is a mark, not an action.

### 3.4 Acceptance for Q2

- Accept, post, dismiss and defer four findings by two reviewers, and `GET /v1/prs/rates` answers 2 of 3 and 1 of 1
  with one undecided. Setting one back to open removes it.
- A move to another room keeps the counts on the new room and none on the old.
- Two rooms with counts for the same reviewer sum on the hub.
- The settings list shows the rate and the floor mark in a headless test with mocked rates.

## 4. Q3: round two reviews only what is new

### 4.1 When a paste is round two

A paste for a PR (`host`, `org`, `repo`, `number`) that has an earlier row with a finished run at another head is
round two. The earlier row is the newest one done, live or archived. The new row records `prior_review` and
`prior_head` (two columns, at the END of `schema.go`).

### 4.2 What the run does differently

- **Fetch** fetches `prior_head` as well, by sha, through the same forge fetch path. When the prior head is gone,
  after a force push that GitHub no longer serves, the run is a full review and the pulls row says "full review, the
  earlier head is gone".
- **Prime** adds a section to `bundle.md`: "Changed since the last review (<prior_head>..<head>)" with that diff.
- **The panel and critics** are told to report only on lines in that section. The renderer's line rule (rule 8)
  checks against the section's diff, not the whole PR's, so a finding on an old line goes back to merge as any wrong
  line does today.
- **Carry.** Before the walk is written, each finding of the prior run is matched by its hunk hash: the hunk around
  its line from `hunkAround`, with line numbers stripped, hashed as change-lifecycle 2.1 says.

| prior finding's hunk at the new head | what happens |
| --- | --- |
| the same hash | carried into the new run with its walk state, its line moved to where the hunk is now |
| changed or gone, and the finding was open or accepted | not carried, outcome `addressed` with `via commit` |
| changed or gone, and the finding was dismissed, posted or deferred | not carried, its outcome stands |

A carried finding is marked "from round 1" on its header. The walk opens on the first open finding, carried or new.

### 4.3 Acceptance for Q3

- A fixture PR reviewed at head A, then pasted at head B where one hunk changed and one did not: the round two
  bundle has the A..B section, the unchanged hunk's finding is carried with its state, the changed hunk's open
  finding records `addressed` by commit, and a new finding on an A-era line is sent back to merge.
- A prior head that cannot be fetched gives a full review and the row says why.

## 5. Order and owners

| item | what | owner | size | waits on |
| --- | --- | --- | --- | --- |
| Q1 | sections 2.1 to 2.5 | @runtime, with @ui for the drawer fold | one worker, small | nothing |
| Q2 | section 3 | @runtime, with @ui for the settings list | one worker, small | Q1, for `confidence` |
| Q3 | section 4 | @runtime | one worker, medium | Q2, for the `commit` outcome |
| T | section 6 | @runtime | not sized | the tested gate's sandbox |

Q1 alone is most of the gain the survey describes, and it is the one to build first.

## 6. Tools wait, and why

Running the repo's linters on changed lines against a baseline is the right shape, and the run folder is the right
place. It waits for two reasons:

- **A repo's linter config is code.** `eslint.config.js` is a program, golangci-lint can load plugins and
  clang-tidy needs `compile_commands.json`, which only a configured build makes. A paste of anyone's PR would then run
  that PR's code on clint's room with clint's session in reach. The change lifecycle's tested gate is designed to run
  untrusted tests with no credentials, and the tools step should run in that same box rather than in a second one.
- **There is no base.** The fetch is a shallow head. A baseline needs the base tree too, and for Go or C, the
  dependencies.

When the sandbox exists, the step is: run each configured tool at base and head inside it, keep what is new at head
and on changed lines, and add it to `bundle.md` as `tools.sarif`. A tool finding is shown to reviewers as context, and
rule 2.1's "anything a linter would catch" keeps the panel from repeating it.

## 7. The later items

| backlog item | where it goes |
| --- | --- |
| 6, nearest CLAUDE.md and AGENTS.md | Small and safe, they are data. Into Q3's bundle work, or a later Q, scoped to each changed file's directory, under the bundle's size limit |
| 7, never-report list in code | Q1 does the code half for pre-existing. The persona's told-off list needs review-on-atrium 7.4's persona memory, which is not built |
| 8, a packer for big diffs | Waits on a PR that overflows today's `prBundleFiles` limit. The current fallback ("read it from src/") holds until then |
| 9, cheap triage | Rejected for now. Every run already shares one cached prime, so a cheap model saves little and adds a second harness to the runner |
| 10, blast radius | Waits on the PR review story landing, which is the orchestrator's branch |
| 11, replay set | After Q1, and it is how the cut is tuned (Open 2). Needs a stored past run, PR 378 first, with its known findings |
| 12, lesson receipt | Waits on persona memory, as item 7 |

## Open for clint

These are the backlog's two held questions, each with the default this design is built to.

1. **Who sees the addressed rate?** Default: clint and @review both, in the pulls view's recipe settings, because
   that is where the panel is edited and a low rate is a reason to edit it.
2. **Is 80 the right cut?** Default: 80, with "dropped (n)" one tap away and every "bring back" counted, so a cut that
   is too high shows up as revived findings. The replay set (item 11) replaces the guess when it exists.

## State

- 2026-10-05: design written. Nothing built, as the backlog item says.
