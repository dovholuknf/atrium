# Reviews that remember (backlog-2 item 16)

A review panel reads the same files once per reviewer, and forgets all of it before the next PR in the same repo.
This is the design for both halves. The facts it rests on are in `docs/research/item16-notes.md` (fb05), cited here
as "notes (a)" to "notes (e)".

Revised 2026-09-29 around the director of software review (@review), from clint's answers of the same day, which
are kept verbatim at the end. Where an earlier recommendation and an answer differ, the answer won and this text
says so at the place it changed.

## What is true today

- **The cost is turns, not size.** Item 16 quotes 104k to 133k tokens per reviewer on openziti/ziti PR #4480. That is
  peak context. Summed over turns, the four reviewers read 21.8M cached tokens and the whole panel of seven 28.7M,
  across 43 to 95 turns each (notes (a)). Every turn re-reads a context that only grows, so a reviewer that takes half
  the turns costs about half, whatever it started with.
- **The panel already hands over the diff, and the reviewers read it again anyway.** Six of seven agents opened the
  diff file the skill had pasted into their prompt. All four read the same core: `router/env/ctrls.go`,
  `router/env/ctrl.go`, `common/ctrlchan/channel.go`, and `channel/v4 multi.go` from the module cache, at two different
  versions (notes (a)).
- **Nothing sizes the panel to the change.** Reviewers are picked by file type and change shape. A small Go backport
  with behaviour in it gets four (notes (a)).
- **Every reviewer is a fresh background subagent.** No forks, so no shared cached prefix (notes (a)).
- **Memory exists in three places, and none of them holds knowledge about one repo.**
  - Each persona has `memory: user` at `~/.claude/agent-memory/<persona>/`, told to keep entries general and never to
    save paths or function names. So it holds almost nothing about any one repo (notes (a)). In practice some of it
    is repo facts anyway: see "From the parked design" below.
  - `mercurius.yaml` is per repo, human-edited, symlinked from dotagents and re-read every round. It holds policy, what
    not to flag, and never facts about the code. Mercurius keeps no state across rounds on purpose (notes (c)).
  - clint's direction of 2026-09-23 in `far-backlog.md`: one file per persona per repo, the agent edits it after every
    job, and the git diff in dotagents is the review (notes (b)). clint's answers of 09-29 keep the file and move the
    editing to the review director.
- **dotagents sweeps its working tree.** `scripts/agent-sync.ps1` offers `add -A`, commit and push on the main
  checkout, and clint says yes to it routinely (every recent commit is "sync agent files"). Anything left uncommitted
  there is pushed at the next sync without a separate look. This is why reviewer-file changes live on branches in
  their own worktree and never in the main checkout's working tree.

## Decision 1: read once, in the skill

All of this is a change to dotfiles, not to atrium: the `review-panel` skill AND the four personas. The skill alone
is not enough, because each persona's own method tells it to get the diff itself and read widely, and the steward's
says "read-heavy is the point" (notes (a)). A review-manager that changes and personas that do not keep every duplicated
read.

1. **A digest, built once.** After capturing the diff the review-manager writes `review-digest.md` beside it:
   - the changed files and hunks
   - the core files, found as the files the diff's callers and callees live in, with the line ranges that matter
   - every dependency the diff touches, AT THE VERSION `go.mod` (or its equivalent) PINS, with its module-cache path
   - the repo's reviewer file, when there is one. Stage 2 moves this part out of the shared digest: a reviewer file
     is per persona, so each reviewer's own file goes in that reviewer's own prompt (decision 2).

   Each reviewer gets the digest in its prompt, is told not to re-open the diff, and opens source only to verify a
   finding or follow a lead the digest does not cover. The pinned version ends the two-versions problem outright.

   **Each persona gains one conditional paragraph at the top of its method:** when the prompt carries a
   `review-digest.md`, that digest IS the review snapshot. Do not recapture or re-open the diff, start from the core
   files it names, and open source or dependency files only to verify a finding or to fill a gap the digest leaves,
   saying which gap. Without a digest the persona works as it does today, so a persona run on its own is unchanged.
2. **Size the panel to the change.** Up to about 150 changed lines, or a backport of a change already reviewed
   upstream: the steward plus the one specialist the file types call for. Larger: today's rule. The review-manager
   prints which rule picked the panel, so a panel that should have been bigger is visible. This is the skill's
   default. A review started through the director arrives with the panel already chosen (decision 3), and the skill's
   existing
   rule "if the user named specific agents, use exactly those" is what honours it.
3. **Batch reads, cap turns.** Each reviewer is told to issue the reads it already knows it needs in ONE turn (parallel
   tool calls), and is given a turn budget of 40, which ends in "report what you have" rather than a hard stop.
4. **Forks, as a measured experiment only.** A fork shares the review-manager's cached prefix, digest included, which
   is the cheapest possible start. Whether a fork can carry a named persona is not known (notes (e)), so it is tried
   on one replay and kept only if it wins.

**Proving it.** Replay PR #4480 at the same commit with the new skill. Compare the summed cached reads, turns and wall
time per reviewer against the notes (a) table, AND the review-manager's own tokens and wall time, since building the
digest is new cost on the review-manager's side. The original run's review-manager cost is read from its parent
transcript first, which fb05 did not parse (notes (e)). It passes at 40% fewer cached tokens for the whole run,
review-manager included, with every blocking or high finding from the original run found again. A finding lost is a
failure whatever the saving. The replay records reviewer wall time, review-manager wall time and total elapsed time
apart, so a saving that only moved work into the review-manager shows as that.

## Decision 2: the knowledge is a file, and the director is its only writer

- **The memory is the file.** `personas/<id>/repos/<host>/<org>/<repo>.md` in dotagents, clint's layout (answer 1),
  with `<host>/<org>/<repo>` spelled the way dotagents already spells it, so the steward's openziti/ziti file is
  `personas/codebase-steward/repos/github/openziti/ziti.md`. It survives every restart and every context clear, and
  it is reviewed as a git diff.
- **The director writes it, and nobody else does** (answer 2). Not the review-manager, and not the personas. Each
  reviewer hands back `repo_notes`, the review-manager passes them through untouched, and the director applies them.
  This supersedes both the 09-23 note's "the agent edits it itself" and this design's earlier "the review-manager
  applies them". Three of the four personas have no Write tool anyway, and a reviewer that edits its own memory
  mid-review is a reviewer doing two jobs.
- **There is no resident per repo** (answer 3). The earlier "a resident is a warm cache of the file" and its stage 3
  are gone. Reviewers are subagents, started fresh for every review. The one standing card is the director.

**What goes in a reviewer file.** Knowledge that is expensive to rediscover and cheap to check. Where the core of the
repo is, the invariants a reviewer should test a change against, how the tests are laid out, which dependency versions
matter, and false positives this persona raised before, with the evidence that refuted them. Paths and function names
ARE allowed here, which is the opposite of the persona's user memory, because re-deriving them is exactly the cost
being removed. The user memory keeps the cross-repo rule it has now.

**Keeping it current.** Every entry carries the commit it was true at. A reviewer that relies on an entry checks it
against the code in front of it, and reports any entry it found wrong. The director drops those, and drops entries
whose files no longer exist. The file is capped at about 150 lines. Past that the director prunes the entries with the
oldest commit first, since which entries were used is not tracked. So the answer to "the repo moves under it" is:
every entry is cheap to verify, and a wrong one is removed the first time anybody trips on it.

**The hand-back contract.** A second fenced JSON block, after the findings array and never inside it, so the findings
schema the skill already checks is untouched:

```json
{"repo_notes": {
  "add":  [{"text": "...", "evidence": "path:line", "commit": "<sha it was true at>"}],
  "drop": [{"match": "<the entry's text, or its first line>", "why": "what the code at <sha> says instead"}]
}}
```

The skill's rule today is "one fenced JSON block, the findings array", so step 6 changes with it, in exactly this
way. The FIRST fenced JSON block is the findings array, as now. An optional LATER fenced block whose top level is an
object with the one key `repo_notes` is the hand-back. Anything else after the findings block is prose, and ignored
as prose is now. The persona is implied by who handed it back, so it is not a field. A missing block means no notes. A
block that opens as `repo_notes` and does not parse or does not match the shape is reported by the review-manager's
integrity check (step 6) as that reviewer's error, never silently skipped, and it never invalidates the findings.
In the file, each entry is one bullet ending `(path:line @ sha)`, which is what the director matches `drop` against
and what a reviewer checks before relying on it.

**What the director checks before applying a note.** An `add` whose `evidence` path does not exist at its `commit`,
or that has no commit, is refused and named in the review's commit message. A `drop` whose `match` hits no entry is
refused the same way. A `drop` whose `match` hits more than one entry drops none and is named. The director reads
each `add` against the code only when it contradicts an entry already in the file.

**Notes the reviewers cannot write.** The verify pass (skill step 5) refutes findings after the reviewer has
finished, so a refuted finding never reaches that reviewer's `repo_notes`. The review-manager lists every refuted
`blocking` or `high` with the verifier's reason in its report, and the director turns each into a false-positive
entry in the file of the persona that raised it, with the verifier's evidence. This is where the parked design's
`rejected.md` went (see "From the parked design").

**How a reviewer finds its file.** The director's brief to the review-manager names each persona's file path and the
dotagents commit to read it at. The review-manager reads each file at that commit and puts it in that reviewer's own
prompt. No persona needs the dotagents path, and a lean session is not a problem, because the file travels in the
prompt rather than through memory loading. Reading at a fixed commit means the director can keep writing other
branches while the review runs, and the reviewer still reads exactly what its notes will be applied against.

**The boundary with `mercurius.yaml`.** `settled_decisions` is clint's policy about what a design reviewer must not
flag. A reviewer file is learned fact about the code. The director decides which one a lesson is with the parked
design's test: would a different persona make the same mistake? No, it is that persona's false positive, and it goes
in that persona's reviewer file. Yes, the code is intended and any reviewer would trip on it, so it is a `proposed
guard` in the director's report, and clint decides whether it becomes a `settled_decisions` entry or a line in the
repo's `CLAUDE.md`. Nothing is ever written to `mercurius.yaml` automatically, and Mercurius never reads a reviewer
file (answer 5).

## Decision 3: the director

One resident card, @review, owns code review across clint's repos the way @merge owns `claude/main`. It is where a
review is asked for, where its result lands, and where clint reacts to it. It picks each panel, writes every reviewer
file, keeps the dangerous-change record, and merges reviewer-file branches. It runs no reviewer itself.

### How a review is started

Anyone asks the director: clint on the board, @orchestrator, or any card, with `atrium tell review` or `atrium_say`.
The ask names a repo (a local checkout path or a remote URL) and a target (a PR number or a commit range), and may say
why, and may name a panel. The director then:

1. **Resolves the target.** The repo's remote gives the slug `<host>-<org>-<repo>` (`github-openziti-ziti`) and the
   reviewer-file key `github/openziti/ziti`. The target's head commit gives `<sha7>`. It counts changed lines.
2. **Picks the panel.** It reads the dangerous-change record for that repo and the general one (below), then applies
   the skill's default, 150 changed lines or a backport for steward plus one, unless a record entry matches, in which
   case the entry says what the panel becomes. A panel named in the ask wins over both, and the director says so if
   the record disagrees with it. Answer 4 makes this the director's call, and every call is written down with its
   reason.
3. **Opens the branch.** `claude/review/<slug>/<target>-<sha7>` in dotagents, from the current tip of
   `claude/review/main`, with no commit yet. That tip is the base commit the reviewers read their files at.
4. **Launches a review-manager.** One worker card per review, always, whatever the size (answer 8): title and alias
   `pr-<repo>-<number>` (`pr-ziti-4397`, `pr-zrok-1277`), never `saNN`, and named that way in every report (clint),
   model sonnet, theme active-work, tags `atrium:subagent`, `dept:review`, `review` and `pr`, cwd the target repo's
   checkout. It is
   launched `lean:false` until r-005 exists (filed on claude/main at 5f75d30: a lean launch that keeps the Agent tool
   and a named list of reviewer agents), because a lean launch today has no Agent tool and a review-manager spawns
   its reviewers (settled through @orchestrator, 2026-09-29). A walker is the same card, launched the same way. Its
   brief is minimal: the target, the
   panel and its sizing line, each reviewer's file path and the base commit to read it at, the repo's known consumers
   from its review-director file with their checkouts (standing rule 27), the second-opinion setting (rule 29,
   Mercurius unless clint named a model), and the report path. It
   runs the `review-panel` skill with exactly that panel, writes nothing to dotagents, writes its full report to the
   report path and one file per finding to `<run>/findings/` (skill step 7, standing rule 10), and reports to @review with the verdict, the findings, every `repo_notes` block by persona, and every
   refuted finding with its reason. At most three review-managers run at once to start (answer 13), and a fourth ask
   waits in the director's queue.
5. **Reports to the asker and to clint.** When the review-manager reports, the director posts the verdict line and the
   findings on its own card, tells the asker the same, and applies the notes (below). It does NOT cull the worker.
6. **Hands the walk to the review-manager.** The review-manager stays up as that PR's walker, under the same
   `pr-<repo>-<number>` name, in the PR's worktree or tree. The director tells clint which card to walk on, and hands
   the manager the walk rules (9 to 24, 26 and 27, 31 to 33) and the findings folder it edits in place. For the board's review
   tab (`docs/review-tab-design.md`), the brief also says that a `Walk:` line in Evidence is state the board writes
   and the walker keeps, and that the board may edit a finding, so the walker re-reads a file before every edit.
   `pr.diff` is always in the run folder (skill step 7). The manager held the
   repo at the head, the evidence and the repros, so it can re-check a line, dig in or rewrite a comment on the spot,
   which clint found far more useful than a walk on the director's card. The director culls it only after clint says
   the walk is done (clint, 2026-09-29). A walker reporting `done` is not that: the cull waits for clint's own word.

**The hierarchy** is three levels: the director, then one review-manager per review, and the review-manager spawns
the reviewer agents as its subagents (answers 3 and 13). The director never spawns a reviewer, and a review-manager
never writes a reviewer file.

**The full report** is at `D:/worktrees/claude/reviews/<slug>/<target>-<sha7>.md`, never committed (answer 9), since
dotagents is pushed and a report quotes the code under review. The card carries the verdict and the findings.

The director works through a worker rather than itself because a review-manager carries the digest and seven agents'
output, and the director has to hold the record and every open review across days. A review running inside the
director would be the context cost this item exists to cut, paid on the one card that cannot cycle freely.

clint can still run `/review-panel` by hand in any session. That run applies no notes: its report carries the
`repo_notes` blocks as text, and clint can forward them to @review, which applies them as it would its own.

### The branches, and when they merge

All reviewer-file writing happens in one dotagents worktree the director owns, `D:/worktrees/claude/dotagents/review`.
The director is one card doing one thing at a time, so one worktree is enough: it switches branches there, and the
main checkout clint syncs from is never touched. Every branch the director writes is under `claude/`, because only
clint commits to a main branch and clint's hook allows commits on `claude/*` (answer 15).

- **`claude/review/main`** is the integration branch, the `claude/main` of dotagents. It starts at `main` and is the
  only branch the director merges into.
- **`claude/review/<slug>/<target>-<sha7>`** holds one review's changes, for example
  `claude/review/github-openziti-ziti/pr-4480-3f9c2ab` or
  `claude/review/github-openziti-channel/a1b2c3d..e4f5a6b-e4f5a6b`. The head sha keeps a second review of the same PR
  after a push on its own branch. The branch gets ONE commit, written when the review-manager reports: every applied
  note for every persona on the panel, plus any refuted-finding entries. Its subject says the repo, the target and
  the panel, for example `github/openziti/ziti pr-4480: steward, go-sec, 3 add, 1 drop`. Refused notes and the sizing
  call go in the body, since this repo's commits are the record of what a review taught.
- **`claude/review/director/<yyyy-mm-dd>-<topic>`** holds a change to the dangerous-change record made outside any
  one review, such as a seed or a correction clint asked for.

**When they merge.** A review's branch merges into `claude/review/main` with `--no-ff`, at the first of these:

1. **Before the next review of the same repo starts.** Step 3 above merges every finished branch for that slug first,
   so the second PR reads what the first one taught, before clint has taken it into `main` (answer 7). That is the
   measurement stage 2 exists to make. The check on an entry clint has not read is the director's refusal rules and
   the reviewer that verifies an entry before relying on it. A review inherits only branches that had finished and
   merged before it launched. Two reviews of one repo running at the same time read the same base and are
   independent, and their branches meet at merge. The stage 2 measurement runs its two reviews one after the other
   for that reason.
2. **When clint asks,** for one branch, one repo, or everything.
3. **When five finished branches are waiting,** so a merge is never a pile.

Two branches that edited the same file conflict at merge. The director resolves by hand: both sets of adds, both
sets of drops, the 150-line cap applied after, and the resolution named in the merge commit.

**How clint takes it.** At any merge point the director tells clint, on its card: `claude/review/main` is N commits
ahead of `main`, these files changed, read it with `git diff main...claude/review/main` in dotagents. clint takes it
with `git merge --ff-only claude/review/main` in the main checkout, and the usual sync pushes it. If `main` has moved
(every "sync agent files" commit moves it), the director first merges `main` into `claude/review/main`, so the
fast-forward always works. The director never commits on `main`, never pushes, and never runs `agent-sync.ps1`.

A branch clint rejects is deleted unmerged, or reverted on `claude/review/main` if it was already merged. Either is
one command, and the director does it on clint's word.

### The dangerous-change record

Answer 4 makes the panel the director's call, and the record is how that call is remembered. It is a reviewer file
like any other, owned by a persona id that never runs as a subagent:

- `personas/review-director/general.md` for kinds of change that are dangerous in any repo
- `personas/review-director/repos/<host>/<org>/<repo>.md` for one repo

Each entry is one bullet: the kind of change, stated so a diff can be checked against it, what the panel becomes, and
what taught it. For example:

```
- a backport whose go.mod changes a dependency version -> full panel (a bump can raise the go directive and the
  consumer toolchain floor, parked codebase-steward lesson project_backport_dep_bump_go_floor)
- a change to control-plane or mesh hello headers -> full panel + network-expert (the budget is set by the oldest
  supported peer, parked codebase-steward lesson project_ziti_handshake_header_budget)
```

A repo's file also holds a `## Known consumers` section when other repos build on it: each consumer and its local
checkout, which a brief passes on so the digest can grep them (standing rule 27). tlsuv's is the first.

An entry is added when a small panel missed something that a later panel, the verify pass, clint or production
caught, and when clint says a kind of change is dangerous. It is removed only when clint says so, since a stale
danger entry costs tokens and a missing one costs a bug. It changes through the same branches as every other file,
so clint reads it as a diff like the rest. Every sizing call, whatever decided it, is in that review's commit body.

### The standing rules: how clint wants a review run and handed back

Distilled 2026-09-29 from clint's PR #369 review session (openziti/tlsuv, the applesec engine). They are the
director's standing rules for every review, and they are the first entries of `personas/review-director/general.md`,
written on the seed branch (stage 2, step 5). Until then this section is where they live.

**Testing.** These bind a review-manager whenever the ask offers hardware or asks for repros. Otherwise the skill's
rule stands: a panel reads, and CI builds.

1. Test on real hardware when it is offered (for example `ssh m1mini`). Build, run the full suite, repeat it, and run
   ASan and UBSan and the platform leak checker.
2. Run every repro on three builds: the PR with the new engine, the PR with the existing engine, and main. The result
   decides the cause: introduced by the PR or already there, engine-specific or shared code. A finding is promoted or
   demoted on that evidence, and the report says which.
3. Missing environment details (a controller URL, credentials, which environment is FIPS) are asked for at the start,
   when the director takes the ask, never at the end of the run.
4. A failure caused by an external service (an HTTP 429 from a public echo server) is reported separately, and never
   counted against the PR.

**Leaks are never dropped.**

5. Every leak goes in the findings with its size and stack, even when it sits in a third-party framework. clint:
   "leaks need to be pointed out, that's a big deal". It is left out of the PR comments only if clint says so. The
   verify pass may correct a leak's severity, and never removes one.

**The report.**

6. The whole result is one table (the `tabular` skill), sorted by severity, then file, then line.
7. Every row has a **Cause** column (PR-introduced, pre-existing, or third-party) and a **Test status** column (which
   existing test covers it, or none, and where a test could be added).
8. Line numbers come from the PR head, the right-hand side of the diff. A comment must sit on a line the PR adds or
   changes. When the root cause is in unchanged lines, it is anchored on the nearest changed line that shows it.

**Walking the comments.** The review-manager does this, on its own card, after it has reported, and stays up for it
(launch step 6). The director hands it these rules and says which card to walk on. Where a rule below says "the
director", during a walk it means the walker.

9. One comment at a time, in the table's order. The director waits for "next". "Go back" means the previous one is
   not done.
10. Every finding, leaks, pre-existing and third-party ones included, has its own plain text file, written by the
    review itself (review-panel step 7), never by hand afterwards: `<run>/findings/NN-<sev>-<file>-L<line>.txt`,
    where `NN` is the report table's order, so sorting by name gives the walk. The comment is at the top, then an
    `Evidence` part (cause, test status, traced or run). The report's table names each row's file. During a walk the
    director shows the comment as raw markdown with its file path, and edits the file in place when the comment
    changes. Nothing is written to `c:\temp`, which clint's own sessions use. The comment's shape, as clint writes it:

    ```
    MED src/applesec/context.c line 149: load_ca(ctx, ca, ca_len);

    * LLM review says a bad CA bundle fails open here: the error is ignored and the engine falls back to system trust.
    * Suggested fix: fail closed when a CA was given but didn't load.
    * Add a test to `tests/http_tests.cpp`: malformed CA, GET a public https site, expect failure.
    ```

    The label line is the severity (`BLOCKING`, `HIGH`, `MED`, `LOW`, `NIT`), the file, the line number, and the code
    on that line. The last bullet is "Add a test to <file>: <input>, expect <result>" when a test is needed. Rules 20
    to 22 amend this shape.
11. Each bullet is one sentence: no call chains, traces, evidence dumps or line lists. It is worded as "LLM review
    says", and kept uncertain and human.
12. A comment never says the author built or ran anything. clint posts these under clint's own name, and did not run
    the repros.
13. Nothing is ever posted to GitHub by the director, a review-manager or a reviewer. clint posts.

Rules 14 to 24 come from the second half of the same session. Where one conflicts with rules 1 to 13, it wins. They
are in `general.md` on dotagents branch `claude/review/director/2026-09-29-walk-rules`.

**Walk order and completeness.**

14. Once the sorted walk list has been shown, it is fixed. The director walks it in exactly that order and never skips
    ahead. A new finding goes into its sorted place, and the director says where it went.
15. Leaks are walk items too, each in its own sorted place. When PR code calls the framework that leaks (`make_identity`
    calling `SecCertificateAddToKeychain` in `context.c`), the finding belongs to the PR, anchored on the PR line. A
    leak is never skipped as "not caused by the PR".
16. When clint skips an item, the director accepts it and moves on without arguing.

**Line numbers.**

17. Before the walk, and again whenever clint's view disagrees, the director checks the PR head (`gh pr view <n>
    --json headRefOid`). If it moved since the review, every remaining item is re-anchored on the current head, and
    the director says which commit the numbers come from. Repro results from the old head are softened.
18. Line numbers come from `gh pr diff` or from raw file bytes (`gh api -X GET .../contents/<path>?ref=<sha>` with
    `Accept: application/vnd.github.raw`, piped through `tee`). Never PowerShell `>` redirection, which shifted the
    line count by 6 in that session.
19. When clint's screenshot still disagrees after that, the director asks whether the Files tab shows "All commits"
    rather than one commit or "changes since your last review".

**Comment shape.** These amend rule 10.

20. The severity goes on each item's label line, its first line, as clint writes it: `MED <file> line N: <code>`.
21. The fix bullet is left out when there is no fix, and the test bullet is left out when no test is needed. "No new
    test needed" is never written.
22. Identifiers, functions, constants, enum values and file paths are formatted as `code`.
23. An FYI (cross-repo impact, performance, anything not proven) is ONE question, not a claim followed by
    conclusions. When clint asks "do we care? do we know?", the answer says three things: traced or run, who it hits,
    and who it does not.
24. clint's wording decides. If clint picks a name that does not exist yet, the director says so once, then writes the
    comment that way.

**Naming.**

25. A review-manager is titled and aliased `pr-<repo>-<number>` (`pr-ziti-4397`, `pr-zrok-1277`), never `saNN`, and
    every report names it that way. Any other worker is named exactly its item id, never with an `sa` prefix: title
    `<id>: <what>`, alias `<id>` (`r-004`, `t-001`, `74b`), and its worktree and branch use the same id
    (`claude/r-007`).

**Rating and reach.** From the PR #369 session recap, read again afterwards. They are in `general.md` at dotagents
`a5f82ee`, on the same branch.

26. Before a finding is rated MED or higher, three questions are answered: who actually hits it, how likely it is, and
    whether it is opt-in. The answers go in the finding's Evidence, on an `Exposure:` line. In PR #369 the panel's
    ratings drifted both ways and clint argued several of them: the keychain leak went up to HIGH, and the loopback
    race and the unchecked dup went down.
27. When the repo has known consumers, each one is grepped for the symbols the change touches, as a standard skill
    step. That turns a vague fit finding into a stated impact. A repo's consumers are listed in its review-director
    reviewer file under `## Known consumers`, with the local checkout to grep. For tlsuv: ziti-sdk-c,
    ziti-tunnel-sdk-c and ziti-sdk-nodejs.

**Links, a second opinion, and tags.** In `general.md` at dotagents `c71fa85`.

28. Every report, walker message and finding file starts with the PR URL. Every finding carries a deep link to its
    line in the Files tab, `.../pull/<n>/files#diff-<sha256 of the path>R<line>` (`L` for a removed line), after its
    label line and in the table. GitHub has no URL that opens a comment box, so the link lands on the line and clint
    clicks `+`, said once per walk. One link per review is checked against `gh pr diff`, never guessed.
29. After the panel and the verify pass, before the walk, a different model gives a second opinion on the distilled
    findings: the report without its repo notes, the finding files, the diff and the changed files at the head. Its
    verdict per finding goes in that finding's Evidence, and a new finding goes in its sorted place marked as its own.
    Where it disagrees, the review-manager reads the code, decides, and writes why. It never reads a reviewer file.
    Which model is a setting: Mercurius by default, or a runner card on `codex` or `gemini`, named by clint when he
    asks for a review, and named in the report. Skill step 8 is the procedure. `general.md` lists the room's runners
    and what each is chosen for.
30. Every review-manager and walker is tagged `atrium:subagent`, `dept:review`, `review` and `pr`.

**The walk's shape.** From clint through @orchestrator, 2026-09-29. In `general.md` on the walk-rules branch.

31. Each walk keeps `walk.txt` in the run directory: one line per finding with the file name, a state (`open`, `done`,
    `skipped`, `deferred`) and a short note. The walker creates it at the start and updates it after each answer.
32. The PR URL appears once, at the top of the walk, never inside an item. This amends rule 28 for walker messages
    only: finding files and reports still start with it.
33. An item's header is the severity, file and line, then a colon. Under it, indented, goes the code line with the
    deep link on the same line:

    ```
    HIGH router/posture/mfa.go line 156:
         deadline := MfaExpiresAt(check.GetMfa(), state) https://github.com/openziti/ziti/pull/4397/files#diff-...R156
    ```
34. "LLM review says" appears at most once per item, as a lead-in line above the bullets. It is never repeated on each
    bullet, and each bullet is a plain statement.

**Right the first time.** clint took six rounds to refine pr-ziti-4397's item 01 by hand, so these bind where findings
are written (the panel and the review-manager, skill step 7), and the walker re-checks each item against them before
showing it. From clint through @orchestrator, 2026-09-29.

35. Anchor on the line that is wrong, not a neighbour (item 01 moved from 156, which computes one value, to 157).
36. The suggested fix is one concrete change, never "X, or Y". When it is small, the bullet gives the exact changed
    line inline as edited code. No separate sketch block, and no renames that are not needed.
37. Plain wording that reads once: what the code does, not a paraphrase of its mechanism.
38. Assert only what was proven. Unconfirmed reachability or impact is "Is it possible for <condition>, and if so,
    does <consequence>?", and the fix bullet then starts "Suggested fix, if so:".
39. Look for other paths that would cover the defect anyway, name them in Evidence, and let them decide between
    rule 38's question and an assertion.
40. Every path named in a bullet is checked against the tree and fixed silently. Never a "Correction:" line.
41. Rule 33 again: the deep link sits on the same line as the indented code. The finding file keeps the link on its
    third line, which the board reads, and the walker moves it beside the code when it shows the item.
42. Overrides the tone of the others: too much detail reads as "we know better". Certainty only when a test we wrote
    proves it or the code settles it beyond doubt. Otherwise the comment is humble and short, from the author's side
    ("I may be missing something here, you know this code better than I do, but if <X> happens, it looks like <Y>,
    which seems bad?"). Rule 38's question is one condition and one consequence. Mechanism, the `file:line` trail and
    covering paths go in Evidence, which clint reads and the PR author does not.

The first second opinions, run by the director on the two reviews already filed (Mercurius, codex gpt-5.5):
zrok #1277 went from 14 rows to 15. Its new row 04 was the critic's possible gap 1, the ambiguous commit, and one
medium went to low. ziti #4397 went from 12 rows to 11: four mediums went to low and one was refuted. The director
overruled it three times, each with the reason in the finding's Evidence.

What this changes elsewhere in the design: the review-manager's report to the director carries the table's columns
(Cause, Test status, PR-head line) on every finding, so the director can build the table and walk the comments
without reopening the review. The skill's step 7 produces all of it: one table sorted by severity, file and line, with
Cause, Test status, lines read at the current head, leaks as rows with sizes, and a `Finding file` column naming
`<run>/findings/NN-<sev>-<file>-L<line>.txt` (dotfiles `af3f5a1` on `claude/review-stage2`). Nobody hand-writes the
files afterwards. Rules 26 and 27 are dotfiles `0cbb9ed`: every finding at medium and above carries an `exposure`
answer and step 6 re-rates on it, and when a brief names known consumers the digest gains a fourth part with each
consumer's `grep` hits for the changed symbols, read at a named commit and never built.

## Which side each part lives on

| Part | Where |
|---|---|
| the digest, batched reads, turn budget, the `repo_notes` parse, passing notes through | dotfiles, `review-panel` skill and the four personas |
| the reviewer files and the dangerous-change record | dotagents, `personas/<id>/repos/<host>/<org>/<repo>.md` |
| picking panels, applying notes, branches and merges | the director, @review, one resident card |
| each review's review-manager | a `pr-<repo>-<number>` worker, one per review, culled only after clint says its walk is done |
| proposed guards | the director's report, then clint, then `mercurius.yaml` or `CLAUDE.md` by hand |

Nothing in atrium changes. No migration. The director and its workers are ordinary cards.

## From the parked design

`bc58c32:docs/personas-design.md` and the pack at `D:/tmp/dotagents-personas-parked/` were read for this revision.
clint undid that attempt on 09-23 and never reviewed it (answer 6). What is taken, by name:

- **The trust split, as branches instead of folders.** The pack split `knowledge/` (a human agreed) from `memory/`
  (the model believes). Here `main` is what clint took and `claude/review/main` is what the director applied, which
  is the same split with git doing the bookkeeping.
- **"Would a different persona make the same mistake?"** The pack's test for where a wrong finding goes. It decides
  between a reviewer-file false positive and a proposed guard, under the `mercurius.yaml` boundary above.
- **`rejected.md`, folded in.** The pack had its conductor (the role this design calls the review-manager) append
  refuted findings to a per-persona file. Here they are false-positive entries in that persona's reviewer file,
  written by the director from the verify pass.
- **"What is new" is a git fact.** The pack used a `Lessons-reviewed` trailer so no marker file was needed. Here it is
  simpler still: `main..claude/review/main`.
- **Only the human pushes.** Unchanged, and now also true of `main`: the director stops at `claude/review/main`.
- **The repo facts already written.** The pack's copies of `~/.claude/agent-memory/` hold lessons that are repo facts
  despite the "keep it general" rule: the openziti/channel `MultiListener` registration identity and lock order,
  the ziti CLI's zitified transport, the zitadel `rp` library as the OAuth exemplar, the hello-header budget, nested
  Go modules, the ziti-sdk-c leak patterns and single-threaded loop. They are the seed for the first reviewer files and
  the first dangerous-change entries. None carries the commit it was true at, so a seeded entry ends `@ unverified`
  until a reviewer confirms it, and the seed is its own branch for clint to read.
- **The evals, kept parked.** Nine golden cases and `run-persona-evals.ps1`, with a haiku judge and scored starts of
  8/8, 9/9 and 6/6. They could show whether a reviewer file makes a persona worse, at about 295k tokens a run. They
  are not in stage 2, because none of the cases is on openziti/ziti. Named so they are not rebuilt from nothing.

Dropped: the persona folders, `persona.yaml` selection, the render step and its codex, ollama and gemini adapters,
the symlinked native memory and generated `MEMORY.md`, the run directory, the atrium catalog, nag and lessons view,
and the diversity panel. None of it is needed for a file that travels in a prompt, and a reviewer file is plain
markdown, so it is runner neutral without a render step.

## Staging

1. **Decision 1, then the PR #4480 replay, measured.** Built by sa16, not yet taken by clint (below). This alone may
   be enough for most PRs, which is item 16's third open question, and the replay answers it.
2. **Reviewer files and the director, on one repo (openziti/ziti).** Built as below. The measurement: the second
   review of an openziti/ziti PR, after the first review's branch merged, against the first, on the same numbers
   decision 1 uses.

There is no stage 3. Answer 3 removed the resident per repo.

## Stage 1, as built, for clint

Built by sa16 (card 01a0edae) in the dotfiles worktree `D:/worktrees/claude/dotfiles/sa16`, branch
`claude/sa16-review-panel`, uncommitted for your diff review. The director read it and did not edit it. It is the
eight edits this design listed, as listed:

- `claude/skills/review-panel/SKILL.md`: step 1 writes `review-digest.md` with its four parts, step 2 sizes the panel
  first, step 3 prints the sizing rule, step 4 hands over the digest, the digest rule, one-turn reads and the 40-turn
  budget. Step 6 is untouched.
- `claude/agents/codebase-steward.md`: method step 1 skips recapture when a digest is present, and "Read-heavy is the
  point" counts the digest's files as read.
- `go-security-reviewer.md`, `functional-tester.md`, `nonfunctional-tester.md`: the same "With a review digest"
  paragraph, placed where this design said.

Not in stage 1: the `memory: user` sections, the reviewer files, `repo_notes`, and any fork. The replay that proves it
costs about 17M tokens and is a separate yes. To say yes: "item 16 stage 1: take it", and separately "and run the
replay".

## Stage 2, concretely

Every step is a change clint reviews before it is used. None of it is started until clint approves this revision.

**In dotfiles (on top of sa16's diff, a new worker, uncommitted for clint):**

1. `review-panel` step 1: the digest's fourth part goes. Step 4: each reviewer's prompt gains its own reviewer file,
   read at the base commit a brief names, and the hand-back contract with the `repo_notes` shape. Step 6: the parse
   rule under "The hand-back contract". Step 7: the report ends with every `repo_notes` block by persona, verbatim,
   and every refuted finding with its verifier's reason. Every finding carries Cause, Test status and a PR-head line
   (standing rules 7 and 8), and a leak is never dropped (rule 5). The skill applies nothing.
2. The four personas: one paragraph each, after the digest paragraph. When the prompt carries your reviewer file,
   check any entry you rely on against the code and hand back what you learned and what you found wrong as
   `repo_notes`. Without one, hand back only what you learned.

**In dotagents (clint's repo, one line, a worker, uncommitted, answer 10):**

3. `Get-DotagentsRepos` in `scripts/_common.ps1` walks every top-level folder as `<host>/<org>/<repo>`, so it would
   read `personas/<id>/repos` as host `personas`, org `<id>`, repo `repos`. `personas` joins the exclusion list beside
   `common` and `scripts`. Nothing breaks without it that was seen, but audit and deploy would walk a fake repo.

**The director's own setup (after clint approves):**

4. The dotagents worktree at `D:/worktrees/claude/dotagents/review` on a new `claude/review/main` from `main`, and the
   reports folder `D:/worktrees/claude/reviews/`.
5. A seed branch `claude/review/director/<date>-seed` (answer 11): the openziti/ziti reviewer files for the four
   personas from the parked lessons that are about openziti/ziti, each `@ unverified`, and the first dangerous-change entries,
   general and openziti/ziti, and the thirteen standing rules at the top of `general.md`. The seed copies.
   `~/.claude/agent-memory/` is left exactly as it is (answer 12).
6. The first two openziti/ziti reviews started through the director, the second after the first's branch merged, and
   the numbers compared.

### Stage 2, as built (2026-09-29)

Steps 1 to 5 are built by the director itself, with no workers. Everything is committed on `claude/` branches and
nothing on a main branch (answer 15). Step 6 is the first two reviews clint picked (answer 17).

- **dotfiles**, worktree `D:/worktrees/claude/dotfiles/review-stage2`, branch `claude/review-stage2` from the same
  base as sa16 (`dd58560`), one commit, `1b96a0e`. It carries sa16's stage 1 diff unchanged, with stage 2 on top and
  "conductor" renamed "review-manager" in the skill (answer 16), so it reads as stage 1 plus stage 2 and sa16's
  worktree is untouched. `claude/skills/review-panel/SKILL.md` and the four personas in `claude/agents/`.
- **dotagents**, worktree `D:/worktrees/claude/dotagents/review`. The seed is `1348dd1` on
  `claude/review/director/2026-09-29-seed`, merged into `claude/review/main` as `ec62191`, from dotagents `main` at
  `4c1b5b7`. `scripts/_common.ps1` (the exclusion), and six new files under `personas/`: the four openziti/ziti
  reviewer files, and the director's `general.md` and openziti/ziti danger file.
- **Reports folder** `D:/worktrees/claude/reviews/`.

Two things the build found that the stage 2 steps above did not say:

- **The finding schema gained two fields,** `third_party` and `test_status`, and `line` is defined as the PR head's
  line. Cause is derived from `third_party` and the existing `preexisting`, so no field duplicates another.
- **A reviewer reads its file at a commit,** so the seed had to be committed and merged before the first review. It
  was, once answer 15 put the branches under `claude/`.
- **Until clint takes `claude/review-stage2`, a review runs on the stage 2 skill and personas from that worktree**
  (answer 17). The review-manager's cwd is a run folder holding copies of the four personas under `.claude/agents/`,
  since a project subagent wins over a user-level one of the same name. The skill is not relied on the same way: the
  brief names the stage 2 `SKILL.md` by absolute path and the review-manager follows that file.

## Review

Mercurius session `s_xT8IKRB82yUj`, closed 2026-09-29, reviewed the design before this revision.

- **Round 1, needs_changes.** Three concerns and an advisory, all fixed at `b502849`: the personas change too, the
  `repo_notes` contract, and stage 2 gated on clint.
- **Round 2, needs_changes, stage 1 judged buildable.** C1 (stage 2 is not buildable) deferred, because that is the
  gate on clint the design already states. C2 (a second JSON block breaks the skill's one-block rule) fixed: the step 6
  parse rule is now spelled out under the hand-back contract. The advisory, wall time recorded three ways, is folded
  into "Proving it".

Mercurius session `s_lYUE7G3Jo0Km`, 2026-09-29, reviewed this revision, standing rules included (an earlier
session, `s_s2p8ZbWgUKUf`, was lost by the server before its round finished).

- **Round 1, ready_to_build.** No concerns, no questions. Advisory A1: two same-repo reviews running at once read the
  same pre-merge files. Folded in under merge point 1. clint's read is what decides (answer 14).

## clint's answers, 2026-09-29

They add a role the design did not have: a **director of software review**, one resident card that owns reviews the
way @merge owns `claude/main`. The design is revised around it before stage 2 starts.

1. **Layout.** `personas/<id>/repos/<host>/<org>/<repo>.md`. Reviews run at the same time, so each writes its
   reviewer-file changes on its own branch, and the review director merges them at points.
2. **Who edits.** The review director, from each reviewer's `repo_notes`. This replaces "the conductor" and "each
   persona".
3. **The resident.** No standing card per repo. Reviewers are subagents. The one standing card is the review
   director, and that is where clint sees results and reacts.
4. **Panel size.** 150 lines or a backport stays the default, but it is the review director's call. A small change
   that is dangerous gets the full panel, and the director remembers which kinds of change are.
5. **Mercurius.** Stays cold. It never reads a reviewer file.
6. **The parked pack.** clint never reviewed it. This design is the design. The review director reads the parked one
   before stage 2 and takes anything still useful, naming it.

## clint's answers to the revision, 2026-09-29

The eight questions the first revision asked, answered through @orchestrator, numbered on from the first set.

7. **`claude/review/main` first.** Yes. The next review reads `claude/review/main` before clint takes it into `main`.
8. **A worker per review.** Always, and lean: no unneeded context at start, a lean launch and a minimal brief.
   Superseded in part: the launch is `lean:false` until r-005 exists, since a lean launch has no Agent tool (step 4).
9. **Reports.** At `D:/worktrees/claude/reviews/<slug>/<target>-<sha7>.md`, never committed.
10. **The exclusion.** A worker may make the `personas` exclusion in dotagents `scripts/_common.ps1`, uncommitted.
11. **The seed.** Seed openziti/ziti from the parked lessons. Do not rebuild from empty.
12. **User memory.** Leave `~/.claude/agent-memory` alone.
13. **Concurrency and naming.** Three at once to start. The hierarchy is the director, then its per-review
    subordinates, and those spawn the reviewer agents. "Conductor" is renamed "review-manager" everywhere.
14. **Mercurius.** One round on the revision, anything useful folded in. clint's read is what decides.

## clint's answers to stage 2 as built, 2026-09-29

15. **Branches.** Only clint commits to main branches. The director may commit to any `claude/*` branch, and clint's
    hook enforces that. The dotagents branches are `claude/review/main`,
    `claude/review/<host>-<org>-<repo>/<target>-<sha7>` and `claude/review/director/<date>-<topic>`. The seed is committed and merged into
    `claude/review/main`, and the dotfiles `claude/review-stage2` work is committed.
16. **One word.** "Conductor" is renamed "review-manager" in the `review-panel` skill too.
17. **The first reviews.** openziti/ziti PR #4397, then openziti/zrok PR #1277, which may run beside it and starts
    with no reviewer files. Each through the director's flow with its own review-manager, on the skill and personas
    from the `review-stage2` worktree, handed back as the standing rules say, and the comments walked with clint one
    at a time on the director's card.
