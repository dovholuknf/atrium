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
says "read-heavy is the point" (notes (a)). A conductor that changes and personas that do not keep every duplicated
read.

1. **A digest, built once.** After capturing the diff the conductor writes `review-digest.md` beside it:
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
   upstream: the steward plus the one specialist the file types call for. Larger: today's rule. The conductor prints
   which rule picked the panel, so a panel that should have been bigger is visible. This is the skill's default. A
   review started through the director arrives with the panel already chosen (decision 3), and the skill's existing
   rule "if the user named specific agents, use exactly those" is what honours it.
3. **Batch reads, cap turns.** Each reviewer is told to issue the reads it already knows it needs in ONE turn (parallel
   tool calls), and is given a turn budget of 40, which ends in "report what you have" rather than a hard stop.
4. **Forks, as a measured experiment only.** A fork shares the conductor's cached prefix, digest included, which is
   the cheapest possible start. Whether a fork can carry a named persona is not known (notes (e)), so it is tried on
   one replay and kept only if it wins.

**Proving it.** Replay PR #4480 at the same commit with the new skill. Compare the summed cached reads, turns and wall
time per reviewer against the notes (a) table, AND the conductor's own tokens and wall time, since building the digest
is new cost on the conductor's side. The original run's conductor cost is read from its parent transcript first, which
fb05 did not parse (notes (e)). It passes at 40% fewer cached tokens for the whole run, conductor included, with every
blocking or high finding from the original run found again. A finding lost is a failure whatever the saving. The
replay records reviewer wall time, conductor wall time and total elapsed time apart, so a saving that only moved work
into the conductor shows as that.

## Decision 2: the knowledge is a file, and the director is its only writer

- **The memory is the file.** `personas/<id>/repos/<host>/<org>/<repo>.md` in dotagents, clint's layout (answer 1),
  with `<host>/<org>/<repo>` spelled the way dotagents already spells it, so the steward's openziti/ziti file is
  `personas/codebase-steward/repos/github/openziti/ziti.md`. It survives every restart and every context clear, and it is reviewed as a git diff.
- **The director writes it, and nobody else does** (answer 2). Not the conductor, and not the personas. Each
  reviewer hands back `repo_notes`, the conductor passes them through untouched, and the director applies them.
  This supersedes both the 09-23 note's "the agent edits it itself" and this design's earlier "the conductor applies
  them". Three of the four personas have no Write tool anyway, and a reviewer that edits its own memory mid-review is
  a reviewer doing two jobs.
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
block that opens as `repo_notes` and does not parse or does not match the shape is reported by the conductor's
integrity check (step 6) as that reviewer's error, never silently skipped, and it never invalidates the findings.
In the file, each entry is one bullet ending `(path:line @ sha)`, which is what the director matches `drop` against
and what a reviewer checks before relying on it.

**What the director checks before applying a note.** An `add` whose `evidence` path does not exist at its `commit`,
or that has no commit, is refused and named in the review's commit message. A `drop` whose `match` hits no entry is
refused the same way. A `drop` whose `match` hits more than one entry drops none and is named. The director reads
each `add` against the code only when it contradicts an entry already in the file.

**Notes the reviewers cannot write.** The verify pass (skill step 5) refutes findings after the reviewer has
finished, so a refuted finding never reaches that reviewer's `repo_notes`. The conductor lists every refuted
`blocking` or `high` with the verifier's reason in its report, and the director turns each into a false-positive
entry in the file of the persona that raised it, with the verifier's evidence. This is where the parked design's
`rejected.md` went (see "From the parked design").

**How a reviewer finds its file.** The director's brief to the conductor names each persona's file path and the
dotagents commit to read it at. The conductor reads each file at that commit and puts it in that reviewer's own
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
3. **Opens the branch.** `review/<slug>/<target>-<sha7>` in dotagents, from the current tip of `review/main`, with no
   commit yet. That tip is the base commit the reviewers read their files at.
4. **Launches a conductor.** One worker card per review: title `saNN`, model sonnet, theme active-work, tags
   `atrium:subagent` and `dept:review`, cwd the target repo's checkout. Its brief says: run the `review-panel` skill on
   this target with exactly this panel and this sizing line, read each reviewer's file from dotagents at this base
   commit, write nothing to dotagents, and report to @review with the full report, every `repo_notes` block by
   persona, and every refuted finding with its reason. Reviewers are that worker's subagents (answer 3). At most three
   conductors run at once, and a fourth ask waits in the director's queue.
5. **Reports to the asker and to clint.** When the conductor reports, the director posts the verdict line and the
   findings on its own card, tells the asker the same, and culls the worker. Then it applies the notes (below).

The director conducts through a worker rather than itself because a panel's conductor carries the digest and seven
agents' output, and the director has to hold the record and every open review across days. A review running inside
the director would be the context cost this item exists to cut, paid on the one card that cannot cycle freely.

clint can still run `/review-panel` by hand in any session. That run applies no notes: its report carries the
`repo_notes` blocks as text, and clint can forward them to @review, which applies them as it would its own.

### The branches, and when they merge

All reviewer-file writing happens in one dotagents worktree the director owns, `D:/worktrees/claude/dotagents/review`.
The director is one card doing one thing at a time, so one worktree is enough: it switches branches there, and the
main checkout clint syncs from is never touched.

- **`review/main`** is the integration branch, the `claude/main` of dotagents. It starts at `main` and is the only
  branch the director merges into.
- **`review/<slug>/<target>-<sha7>`** holds one review's changes, for example
  `review/github-openziti-ziti/pr-4480-3f9c2ab` or `review/github-openziti-channel/a1b2c3d..e4f5a6b-e4f5a6b`. The head
  sha keeps a second review of the same PR after a push on its own branch. The branch gets ONE commit, written when
  the conductor reports: every applied note for every persona on the panel, plus any refuted-finding entries. Its
  subject says the repo, the target and the panel, for example `github/openziti/ziti pr-4480: steward, go-sec, 3 add,
  1 drop`. Refused notes and the sizing call go in the body, since this repo's commits are the record of what a
  review taught.
- **`review/director/<yyyy-mm-dd>-<topic>`** holds a change to the dangerous-change record made outside any one
  review, such as a seed or a correction clint asked for.

**When they merge.** A review's branch merges into `review/main` with `--no-ff`, at the first of these:

1. **Before the next review of the same repo starts.** Step 3 above merges every finished branch for that slug first,
   so the second PR reads what the first one taught. That is the measurement stage 2 exists to make.
2. **When clint asks,** for one branch, one repo, or everything.
3. **When five finished branches are waiting,** so a merge is never a pile.

Two branches that edited the same file conflict at merge. The director resolves by hand: both sets of adds, both
sets of drops, the 150-line cap applied after, and the resolution named in the merge commit.

**How clint takes it.** At any merge point the director tells clint, on its card: `review/main` is N commits ahead of
`main`, these files changed, read it with `git diff main...review/main` in dotagents. clint takes it with
`git merge --ff-only review/main` in the main checkout, and the usual sync pushes it. If `main` has moved (every "sync
agent files" commit moves it), the director first merges `main` into `review/main`, so the fast-forward always works.
The director never commits on `main`, never pushes, and never runs `agent-sync.ps1`.

A branch clint rejects is deleted unmerged, or reverted on `review/main` if it was already merged. Either is one
command, and the director does it on clint's word.

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

An entry is added when a small panel missed something that a later panel, the verify pass, clint or production
caught, and when clint says a kind of change is dangerous. It is removed only when clint says so, since a stale
danger entry costs tokens and a missing one costs a bug. It changes through the same branches as every other file,
so clint reads it as a diff like the rest. Every sizing call, whatever decided it, is in that review's commit body.

## Which side each part lives on

| Part | Where |
|---|---|
| the digest, batched reads, turn budget, the `repo_notes` parse, passing notes through | dotfiles, `review-panel` skill and the four personas |
| the reviewer files and the dangerous-change record | dotagents, `personas/<id>/repos/<host>/<org>/<repo>.md` |
| picking panels, applying notes, branches and merges | the director, @review, one resident card |
| each review's conductor | an saNN worker, one per review, culled when it reports |
| proposed guards | the director's report, then clint, then `mercurius.yaml` or `CLAUDE.md` by hand |

Nothing in atrium changes. No migration. The director and its workers are ordinary cards.

## From the parked design

`bc58c32:docs/personas-design.md` and the pack at `D:/tmp/dotagents-personas-parked/` were read for this revision.
clint undid that attempt on 09-23 and never reviewed it (answer 6). What is taken, by name:

- **The trust split, as branches instead of folders.** The pack split `knowledge/` (a human agreed) from `memory/`
  (the model believes). Here `main` is what clint took and `review/main` is what the director applied, which is the
  same split with git doing the bookkeeping.
- **"Would a different persona make the same mistake?"** The pack's test for where a wrong finding goes. It decides
  between a reviewer-file false positive and a proposed guard, under the `mercurius.yaml` boundary above.
- **`rejected.md`, folded in.** The pack had the conductor append refuted findings to a per-persona file. Here they
  are false-positive entries in that persona's reviewer file, written by the director from the verify pass.
- **"What is new" is a git fact.** The pack used a `Lessons-reviewed` trailer so no marker file was needed. Here it is
  simpler still: `main..review/main`.
- **Only the human pushes.** Unchanged, and now also true of `main`: the director stops at `review/main`.
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
   and every refuted finding with its verifier's reason. The skill applies nothing.
2. The four personas: one paragraph each, after the digest paragraph. When the prompt carries your reviewer file,
   check any entry you rely on against the code and hand back what you learned and what you found wrong as
   `repo_notes`. Without one, hand back only what you learned.

**In dotagents (clint's repo, one line):**

3. `Get-DotagentsRepos` in `scripts/_common.ps1` walks every top-level folder as `<host>/<org>/<repo>`, so it would
   read `personas/<id>/repos` as host `personas`, org `<id>`, repo `repos`. `personas` joins the exclusion list beside
   `common` and `scripts`. Nothing breaks without it that was seen, but audit and deploy would walk a fake repo.

**The director's own setup (after clint approves):**

4. The dotagents worktree at `D:/worktrees/claude/dotagents/review` on a new `review/main` from `main`.
5. A seed branch `review/director/<date>-seed`: the openziti/ziti reviewer files for the four personas from the parked
   lessons that are about openziti/ziti, each `@ unverified`, and the first dangerous-change entries, general and
   openziti/ziti.
6. The first two openziti/ziti reviews started through the director, the second after the first's branch merged, and
   the numbers compared.

## Review

Mercurius session `s_xT8IKRB82yUj`, closed 2026-09-29, reviewed the design before this revision.

- **Round 1, needs_changes.** Three concerns and an advisory, all fixed at `b502849`: the personas change too, the
  `repo_notes` contract, and stage 2 gated on clint.
- **Round 2, needs_changes, stage 1 judged buildable.** C1 (stage 2 is not buildable) deferred, because that is the
  gate on clint the design already states. C2 (a second JSON block breaks the skill's one-block rule) fixed: the step 6
  parse rule is now spelled out under the hand-back contract. The advisory, wall time recorded three ways, is folded
  into "Proving it".

This revision has not been through Mercurius. Decision 3 and stage 2 are new.

## Open questions for clint

1. **Does the next review read `review/main` before you take it?** Merge point 1 makes the second PR read what the
   first taught, before you have read it. Recommended yes, since the director refuses unevidenced notes and every
   entry is checked by the reviewer that relies on it, and a no means stage 2 cannot measure anything until you merge.
2. **A worker per review.** Is an saNN conductor per review the right shape, or should small reviews run inside the
   director? Recommended a worker always, for the context reason under "How a review is started".
3. **Where the full review reports live.** The director posts the verdict and findings on its card. The full report,
   with seven agents' output, is not in dotagents, since that is pushed. Recommended a local folder,
   `D:/worktrees/claude/reviews/<slug>/<target>-<sha7>.md`, never committed. Or is the card enough?
4. **The `_common.ps1` exclusion.** May a worker make that one-line change in dotagents, uncommitted, or do you?
5. **The seed.** Seed openziti/ziti from the parked lessons now, marked unverified, or start from empty files and let
   the first reviews write them?
6. **Repo facts in user memory.** The lessons that are repo facts stay in `~/.claude/agent-memory/` too. Should a
   seeded fact leave user memory once it is in a reviewer file, or is user memory left alone?
7. **Three conductors at once.** Is that the right ceiling?
8. **A Mercurius round on this revision** before stage 2 is built, or is your read enough?

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
