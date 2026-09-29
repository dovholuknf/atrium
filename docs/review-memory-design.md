# Reviews that remember (backlog-2 item 16)

A review panel reads the same files once per reviewer, and forgets all of it before the next PR in the same repo.
This is the design for both halves. The facts it rests on are in `docs/research/item16-notes.md` (fb05), cited here
as "notes (a)" to "notes (e)".

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
    save paths or function names. So it holds almost nothing about any one repo (notes (a)).
  - `mercurius.yaml` is per repo, human-edited, symlinked from dotagents and re-read every round. It holds policy, what
    not to flag, and never facts about the code. Mercurius keeps no state across rounds on purpose (notes (c)).
  - clint's direction of 2026-09-23 in `far-backlog.md`: one file per persona per repo, the agent edits it after every
    job, and the git diff in dotagents is the review (notes (b)).

## Decision 1: read once, in the skill

All of this is a change to dotfiles, not to atrium: the `review-panel` skill AND the four personas. The skill alone
is not enough, because each persona's own method tells it to get the diff itself and read widely, and the steward's
says "read-heavy is the point" (notes (a)). A conductor that changes and personas that do not keep every duplicated
read.

1. **A digest, built once.** After capturing the diff the conductor writes `review-digest.md` beside it:
   - the changed files and hunks
   - the core files, found as the files the diff's callers and callees live in, with the line ranges that matter
   - every dependency the diff touches, AT THE VERSION `go.mod` (or its equivalent) PINS, with its module-cache path
   - the repo's reviewer file from decision 2, when there is one

   Each reviewer gets the digest in its prompt, is told not to re-open the diff, and opens source only to verify a
   finding or follow a lead the digest does not cover. The pinned version ends the two-versions problem outright.

   **Each persona gains one conditional paragraph at the top of its method:** when the prompt carries a
   `review-digest.md`, that digest IS the review snapshot. Do not recapture or re-open the diff, start from the core
   files it names, and open source or dependency files only to verify a finding or to fill a gap the digest leaves,
   saying which gap. Without a digest the persona works as it does today, so a persona run on its own is unchanged.
2. **Size the panel to the change.** Up to about 150 changed lines, or a backport of a change already reviewed
   upstream: the steward plus the one specialist the file types call for. Larger: today's rule. The conductor prints
   which rule picked the panel, so a panel that should have been bigger is visible.
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

## Decision 2: the knowledge is a file, and a resident session is only a cache of it

Item 16 asks for "a resident reviewer per repo". clint's 09-23 direction asks for a file per persona per repo. They
are one design once each is given its job:

- **The memory is the file.** `personas/<id>/repos/<host>/<org>/<repo>.md` in dotagents, clint's layout. It survives
  every restart and every context clear, it is reviewed as a git diff, and only clint commits it.
- **A resident is a warm cache of the file, not a second memory.** A standing atrium card per repo, if it exists,
  conducts that repo's panels and is kept bounded with item 66's new-context. Everything it learns goes into the files
  before its context is cleared, so killing it loses nothing. That is why it is optional, and why it comes second.

**What goes in a reviewer file.** Knowledge that is expensive to rediscover and cheap to check. Where the core of the
repo is, the invariants a reviewer should test a change against, how the tests are laid out, which dependency versions
matter, and false positives this persona raised before, with the evidence that refuted them. Paths and function names
ARE allowed here, which is the opposite of the persona's user memory, because re-deriving them is exactly the cost
being removed. The user memory keeps the cross-repo rule it has now.

**Keeping it current.** Every entry carries the commit it was true at. A reviewer that relies on an entry checks it
against the code in front of it, and reports any entry it found wrong. The conductor drops those, and drops entries
whose files no longer exist. The file is capped at about 150 lines, and the conductor prunes the oldest unused entries
first. So the answer to "the repo moves under it" is: every entry is cheap to verify, and a wrong one is removed the
first time anybody trips on it.

**Who writes it: the conductor, from what the reviewers hand back.** Each reviewer's report gains a `repo_notes`
block: entries to add, and entries it found wrong. The conductor applies them, one edit per persona file, and leaves
them uncommitted in dotagents for clint's diff review. Three of the four personas have no Write tool, and a reviewer
that edits its own memory mid-review is a reviewer doing two jobs. If clint agrees (open question 2), this SUPERSEDES
the 09-23 note's "the agent edits it itself".

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
In the file, each entry is one bullet ending `(path:line @ sha)`, which is what the conductor matches `drop` against
and what a reviewer checks before relying on it.

**How a reviewer finds its file.** The conductor reads the repo's remote, derives `<host>/<org>/<repo>`, and puts the
file in the digest. No persona needs the dotagents path, and a lean session is not a problem, because the file travels
in the prompt rather than through memory loading.

**The boundary with `mercurius.yaml`.** `settled_decisions` is clint's policy about what a design reviewer must not
flag. A reviewer file is learned fact about the code. A reviewer that concludes "stop flagging X" puts that under
`proposed guards` in the panel's report, and clint decides whether it becomes a `settled_decisions` entry. Nothing is
ever written to `mercurius.yaml` automatically, and Mercurius does not read the reviewer files (open question 5).

## Which side each part lives on

| Part | Where |
|---|---|
| the digest, panel sizing, batched reads, turn budget, `repo_notes`, applying them | dotfiles, `review-panel` skill and the four personas |
| the reviewer files | dotagents, `personas/<id>/repos/<host>/<org>/<repo>.md` |
| a resident reviewer card, later | atrium: nothing new. `atrium_launch` with `lean: false`, item 66's new-context, a tag |
| proposed guards | the panel's report, then clint, then `mercurius.yaml` by hand |

Nothing in atrium changes for either decision. No migration.

## Staging

1. Decision 1, then the PR #4480 replay, measured. This alone may be enough for most PRs, which is item 16's third open
   question, and the replay answers it. **Buildable now:** it needs none of the open questions answered except 4,
   which has a stated default.
2. Reviewer files on one repo (openziti/ziti), with `repo_notes` and the conductor applying them. Measure the second
   PR on that repo against the first. **Not buildable until clint answers questions 1, 2 and 6.** They decide where the
   files are made, who may write them, and whether the 09-23 direction still holds, and a wrong answer there fails
   silently. The recommended answers become this design's decisions when he gives them, and the design is revised
   before stage 2 starts.
3. Only if 2 shows the files work and the panel is still slow: a resident card for that repo.

## Stage 1, file by file, for clint

Everything below is in `D:/git/github/dovholuknf/dotfiles/claude/`, your repo. By the standing rule for dotfiles it
is left uncommitted for your diff review, and nothing here touches atrium. Nothing is built yet. The worker (fb06)
was held so this is your call. The replay that proves it costs about 17M tokens, and it is a separate yes.

**`skills/review-panel/SKILL.md`**

1. **Step 1, "Determine the review target".** A new last bullet: after capturing the diff, write
   `review-digest.md` beside it, built once by the conductor. Its four parts are the list in decision 1: the changed
   files and hunks, the core files with the line ranges that matter (the files the diff's callers and callees live
   in), every dependency the diff touches at the version `go.mod` or its equivalent pins with its module-cache path,
   and the repo's reviewer file when stage 2 has made one.
2. **Step 2, "Select the relevant agents".** A new paragraph ahead of the default mapping: up to about 150 changed
   lines, or a backport of a change already reviewed upstream, the panel is `codebase-steward` plus the ONE
   specialist the file types call for. Larger changes use the mapping as it is. Open question 4 is this line, and
   150 is the stated default.
3. **Step 3, "Report the panel".** The one-line print gains which sizing rule picked the panel (`small: steward +
   go-security-reviewer` or `full mapping`), so a panel that should have been bigger shows.
4. **Step 4, "Dispatch in parallel".** The bullet "the captured diff text and its range" becomes "the digest, and the
   diff range". The bullet "read whatever surrounding files or dependency source they need, NOT just the diff" is
   replaced with the digest rule: the digest is the snapshot, do not re-open the diff, open source only to verify a
   finding or fill a gap the digest leaves and name the gap. Two new bullets: issue the reads you already know you
   need in ONE turn as parallel tool calls, and a budget of 40 turns, after which you report what you have.
5. **Step 6, "Merge and triage".** Nothing in stage 1. The `repo_notes` parse rule under decision 2 goes in with
   stage 2.

**`agents/codebase-steward.md`**

6. **"Mandatory method", step 1 ("Get the diff").** A conditional in front of it: when the prompt carries a
   `review-digest.md`, that is the snapshot, so skip this step and start step 3 from the core files it names.
   Without one, step 1 stands as it is.
7. **"Operating notes", the "Read-heavy is the point" bullet.** It gains one sentence: with a digest, the neighbour
   files and dependency source it already quotes count as read, and opening them again is the waste this change
   removes. The failure it guards against, reviewing only the diff, is unchanged.

**`agents/go-security-reviewer.md`, `agents/functional-tester.md`, `agents/nonfunctional-tester.md`**

8. **The same conditional paragraph in each.** In the two testers it goes after "Operational constraints", and in
   the security reviewer ahead of "When you find an issue". The text is the paragraph in decision 1: the digest is
   the snapshot, do not recapture or re-open the diff, start from its core files, open source or dependency files
   only to verify a finding or to fill a gap the digest leaves and say which gap. Without a digest each persona is
   unchanged, so running one on its own behaves as it does today.

**Not in stage 1:** the `memory: user` sections, the reviewer files, `repo_notes`, and any fork. Forks are tried only
inside the replay, on one reviewer, and kept only if they win (decision 1, item 4).

**Then the replay.** PR #4480 at the original commit, the numbers compared with notes (a) as "Proving it" says. It
passes at 40% fewer cached tokens for the whole run, conductor included, with every blocking or high finding found
again.

**To say yes in one line:** "item 16 stage 1: build it" (fb06 writes items 1 to 8, uncommitted in dotfiles), and
separately "and run the replay". Either can be a no, and the second only makes sense after the first.

## Review

Mercurius session `s_xT8IKRB82yUj`, closed 2026-09-29.

- **Round 1, needs_changes.** Three concerns and an advisory, all fixed at `b502849`: the personas change too, the
  `repo_notes` contract, and stage 2 gated on clint.
- **Round 2, needs_changes, stage 1 judged buildable.** C1 (stage 2 is not buildable) deferred, because that is the
  gate on clint the design already states. C2 (a second JSON block breaks the skill's one-block rule) fixed: the step 6
  parse rule is now spelled out under the hand-back contract. The advisory, wall time recorded three ways, is folded
  into "Proving it".

No round 3. What is left is clint's answers, not a design gap.

## Not read, and worth knowing

The parked persona pack at `D:/tmp/dotagents-personas-parked/` and the design at `bc58c32:docs/personas-design.md` were
not opened (notes (e)). clint undid that attempt on 09-23. This design takes only the file layout from his direction
that followed, and should be checked against the parked design before stage 2 is built.

## Open questions for clint

1. **Layout.** `personas/<id>/repos/<host>/<org>/<repo>.md`, as you gave it on 09-23? The alternative is beside
   `mercurius.yaml` at `<host>/<org>/<repo>/reviewers/<id>.md`, which keeps a repo's files together. Recommended:
   yours, since files travel in the prompt and need no deploy link either way.
2. **Who edits the file.** The conductor, from each reviewer's `repo_notes` (recommended), or each persona itself
   after every job, as the 09-23 note says?
3. **The resident.** Is a standing card per repo still wanted once the files exist, or only if stage 2 falls short?
4. **Panel size.** Is "about 150 changed lines, or a backport" the right line for steward plus one?
5. **Mercurius.** Should the Mercurius reviewer ever read a repo's reviewer file? Recommended no: Mercurius judges a
   design cold on purpose.
6. **The parked pack.** Does anything in `bc58c32:docs/personas-design.md` still stand, or is this the design now?

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
