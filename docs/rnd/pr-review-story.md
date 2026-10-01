# PR review in atrium, told from clint's seat

clint, 2026-10-01: "i want a detailed story written up around how this process would work. we don't need other scm
comment integration just yet but we can get there. more important is allowing the human to drive an llm to make a
good, quality code review. some of my 'walk' stuff recently and the format i liked for example should be taken into
account."

This is that story. It is for clint to read and decide whether this is the process he wants. It does not replace the
designs it leans on: `docs/rnd/pulls-view-design.md` (the runner and the view), `docs/rnd/pulls-api.md` (the routes),
`docs/rnd/review-tab-design.md` (the walk drawer), `docs/review/review-memory-design.md` (reviewer files and the
standing rules 1 to 44). Where this story adds something, it says so and names the stage that builds it.

The centre of it: **the LLM prepares, ranks and drafts. clint walks it, item by item, and decides.** Everything
before the walk exists to make the walk short and right the first time. Everything after the walk is clint's to
post. Posting to GitHub, reading comments back and other forges are later stages, named in section 8 and not
designed here.

The examples are real. openziti/ziti PR 4397 item 01 is the item clint refined over six rounds on 2026-09-29.
openziti/ziti-tunnel-sdk-c PR 1441 is where clint rejected two findings as intended behaviour and asked about a
duplicate helper. tlsuv PR 378 is the run that went wrong on 2026-10-01 and the yardstick for time and cost. Where
the story goes past what happened (a test that was never run on the real walk), it says so.

## 1. Getting a PR in

clint is reading Slack and sees a review request for `https://github.com/openziti/ziti/pull/4397`.

He opens the board, goes to **pulls**, presses `+ paste a PR`, and pastes the URL. Or, in a terminal, he runs
`gwt pr 4397` the way he does today. Both end in the same call, `POST /v1/prs`. No card is made. Nobody is asked.

What he sees, in order:

| after | the pulls row shows |
| --- | --- |
| under 1 s | `openziti/ziti #4397`, state `fetching`. The URL is recognised, the row and its run folder exist |
| about 2 s | the title and author, from `gh pr view` |
| about 1 min | `running: panel`, the head `990aa0c`, and the reviewers it picked (2.2) |
| as it runs | the step on the right (`verify`, `merge`), and the cost so far, `$0.71 so far` |
| under 10 min | `ready to walk`, `1 high · 3 med · 5 low · 2 nit`, a `walk` button, and the run's cost and time |

Three things are true from the first second, and all three are fixes for what happened on 378:

- **No director is in the loop.** On 378 gwt opened a card that had to announce itself to @review and wait to be
  adopted. @review held it under a pause and "asked clint", and the question never reached him. 41 minutes went by
  before any review started. Here there is no adoption step at all. The recipe runs on intake.
- **Pauses never apply to his PRs.** A board pause stops directors from picking up work. A recipe run is not a
  director's work, so a PR clint pasted or opened with gwt runs whatever the pause says. This is built: the
  `/v1/prs` routes sit on the human listener and a board pause never holds them.
- **A PR is a row, not a card.** It never shows in the agent list or the stack. The only card a review ever makes is
  the walker, and only when clint presses `walk`.

A PR found by a source (`gh search prs --review-requested=@me`) starts on its own when clint's review is requested on
it, and waits on a `review` click otherwise. That is decided (pulls-view section 6) and is a later door.

## 2. Preparation, without him

clint goes back to what he was doing. This is what atrium does in the next ten minutes. None of it needs him.

### 2.1 One fetch

The room daemon fetches the PR once into its run folder:

```
D:/worktrees/claude/reviews/github-openziti-ziti/pr-4397-990aa0c/
  pr.json       gh pr view: title, author, base, head, files
  pr.diff       gh pr diff, fetched once
  src/          the repo at the head, a blobless shallow fetch, read only
  bundle.md     built by Go, not a model: the PR summary, the diff, each changed file in full at the head
  steps/        one folder per step, so nothing is ever shared or overwritten
  findings/     one file per finding, NN in walk order
  walk.txt      one line per finding with its state
  review.json   heads, panel, cost per step, timings
  run.log       one line per step
```

Every reviewer reads this one copy. On 378, eight sessions read the same 60 KB diff eight times, and three helpers
shared one `BRIEF.md` and overwrote it, so the C systems review had to run again. Here no reviewer has a working
directory to share, and each step writes only under its own `steps/<step>/`.

### 2.2 The recipe picks the panel

The stored recipe (`pr_recipe`, seeded `default`) names the reviewers and the files that select each:

```
c-systems-reviewer      *.c, *.h
go-security-reviewer    *.go
functional-tester       every PR
nonfunctional-tester    every PR
```

4397 touches Go only, so the C reviewer sits out. A repo can have its own recipe (`match: openziti/ziti`) that adds
codebase-steward, as the 4397 run had, or the consumer check of rule 27, or a second opinion. The agent bodies come
from `~/.claude/agents/`, the same ones the review-panel skill uses, so improving `go-security-reviewer.md` improves
every recipe with no other edit.

### 2.3 One-shot calls, and the one full session

**Every reviewer, verifier and critic is a one-shot call.** One `claude -p` reads `bundle.md` once and keeps its
session (the prime). Each reviewer is a fork of that session, so the diff is read from cache, not paid for again. A
fork gets read-only tools, answers in JSON, and exits. No card, no report, no title, nothing left running. On 378
every helper stayed alive about an hour after it reported, and each reported twice.

**The only full session is the walker**, launched when clint walks. It is a conversation, so it has to be one. Stage
1 launches it as a fork of the prime too, so when clint says "go deeper" the walker already holds the diff and the
changed files in cache (open question 5).

### 2.4 Verify and the second opinion

- **Verify.** Every finding at MED or above goes to a verifier fork, which answers `holds`, `does not hold, because`,
  or `holds at <sev>`, with what it read. On 4397 verify confirmed the high and noted its reach: identities with more
  than one MFA deadline.
- **Second opinion.** A different model family re-reads the merged list (Mercurius by default, or a codex or gemini
  runner). It never holds the walk. If it fails or times out, the row is still `ready to walk` and says
  `2nd: failed [retry]`. On 378 the review card ended its turn waiting on a Mercurius round and nothing woke it, and
  the walk opened on "the session is gone". Steps 6 and 7 (second opinion, settling disputes) are not built yet
  (stage 4). Until then the walker offers a Mercurius round at the top of the walk, and clint can say no.

### 2.5 Review memory

Every repo clint has reviewed before has reviewer files in dotagents:
`personas/<persona>/repos/github/openziti/ziti.md`. They hold what is expensive to rediscover and cheap to check:
where the core of the repo is, its invariants, how its tests are laid out, and the false positives each persona raised
before, with the evidence that refuted them. Each entry carries the commit it was true at.

On 4397 the steward handed back "the house exemplar for time-based posture enforcement is the controller
PostureCache sweep" with its path and commit, so the next ziti review starts from it instead of finding it again.

What this story adds is **clint's own rejections as memory**. When clint skips an item and says why, that reason is
the best false-positive evidence there is. Section 5.2 shows how it is written down and section 8 says when the
prime starts reading it (gap G7: today the recipe's prime reads no reviewer file at all, so each PR starts from zero).

### 2.6 Budget

Measured on 378 (pulls-view section 1) and estimated for this flow at the same rates (pulls-view section 8):

| | 378 as it ran | this flow |
| --- | --- | --- |
| open to walk-ready | 1h49m, of which 89 min waiting on a director or on clint | under 10 min, no wait on anyone |
| model calls | 218 | about 59 |
| cost before the walk | about $7.00, not counting @review or Mercurius | about $1.50, under a $3 recipe cap |
| sessions left running | eight helpers, about an hour each | none |

The cap is checked between steps. A run that reaches it stops and says `failed: budget`, and `retry` raises the cap
once. The walk itself costs only while clint is walking. A forked walker that answers ten questions on cached context
is cents, not dollars.

These are targets. Stage 1's acceptance is the measured replay of 378 (item A2), not this table.

## 3. Ordering: the item that matters most comes first

On 378 clint opened the walk and item 1 was a LOW on `engine.c` line 227. "and not ordered? this sucks". Verify had
downgraded all six mediums, so 17 items sat in one band sorted by file name and line. File name says nothing about
how much an item matters, or how the diff reads.

### 3.1 The order

The merge step fixes the order once, and nothing changes it after (rule 14):

1. **Disputes left for clint.** A second opinion that could not be settled by reading the code. Usually none.
2. **Then by impact.** The merge fork gives every finding one `impact` sentence (what breaks for a user of this code,
   and how likely) and a `rank` across the whole list, worst first. Severity is the label clint posts. It decides
   the order only between bands, and inside a band the rank decides. A leak ranks as its impact says, never last.
3. **Related items walk together.** The merge fork also gives each finding a `group`: the same function, the same
   root cause, or the same concern seen by two reviewers. A group walks as a run of items, placed where its
   highest-ranked member would be. Two items that are the same defect are merged into one before this, as today.
4. **Ties, and only ties, in diff order.** The order the files appear in `pr.diff`, then line. That is the order
   clint reads the PR in on GitHub.
5. **The tail.** NITs, and LOWs the merge marks `take_or_leave` (style, naming, a test that would be nice), are not
   walked one by one. They come last as one list, and clint walks them, picks from them, or skips them all in one
   word (open question 2).

What is built: the renderer already sorts disputes, band, rank, then diff order (r-pr-render). What stage 1 adds is
the `group` and `take_or_leave` fields in the merge JSON and the walk opening that shows them.

### 3.2 What 17 items in one band becomes

378's 17 items, all LOW or NIT after verify, would open like this. The text is the walker's first message, and it is
the only place the walk shows a count:

```
https://github.com/openziti/tlsuv/pull/378  head ad5ddf4, current

9 to walk, then 8 small ones as a list.

  keychain lifetime    3   the keychain swap, the ex_data destructor, the removed assert
  RSA keychain keys    2   TLS 1.2 cap, length of the signature returned
  peer certificate     1   hash-ordered store, to_pem can return a non-leaf cert
  test gaps            3   server handshake, P-384 and P-521, sign failure
  small ones           8   comments, guards, a NULL check, naming

First: the keychain swap frees handles through the new keychain.
```

Item 1 is now the one whose impact sentence is worst, and its two neighbours on the same code follow it. The test gaps
come after the defects they would catch. The eight small ones are one decision, not eight. Whether this opening is
the right one is stage 1's acceptance: clint walks 378 in this order and says whether item 1 is where he would have
started.

## 4. The walk

### 4.1 Where it happens: the walker's terminal, for stage 1

Three places could host it: the walker's terminal on the board, the pulls drawer (`js/walk.js`, built for review-tab
stage 1, being re-pointed at `/v1/prs` by pulls-p3), or the phone.

**Stage 1 is the walker's terminal.** clint presses `walk` on the row, the walker card opens in the terminals view,
and the walk is a conversation in it. Why:

- Every walk clint called useful was a conversation: 4397, 1441, #369. "do we care? do we know?", "i need more
  expansion on item 1", "can that be 'fixed'?" are questions, not buttons. A form throws that away.
- It works today on desktop and through the hub on the phone, because a terminal already does. The drawer's
  re-point (pulls-p3, item A3) is on HOLD for a medium.
- `walk.txt` and the finding files are the record either way. Moving the walk to the drawer in stage 3 loses
  nothing, and the drawer's `a` key types a question into this same terminal.

### 4.2 What one item looks like

The walker shows rule 33's header, the code line with its deep link on the same line, the lead-in once, and the
bullets. Nothing else. No "Item 1 of 17", no file name, no "Say next, ask for an edit, or skip this one" footer, no
Evidence unless asked, no Mercurius line (it lives in Evidence). The PR URL appeared once, at the top of the walk.

This is 4397 item 01 as clint accepted it after six rounds, word for word from the finding file. In this document the
link is cut to fit 120 columns. On screen it is the full `.../pull/4397/files#diff-<sha256 of the path>R157`.

```
HIGH router/posture/mfa.go line 157:
     if deadline != nil && (expiresAt == nil || deadline.Before(*expiresAt)) {  https://github.com/openziti/zi...R157

LLM review:

* If an identity has two MFA checks with different deadlines, it looks like the later one is never enforced once
  the earlier one passes, which seems bad? Is that even possible? I'm not sure...
* If so, would `&& !deadline.After(now)` address that problem?
```

The second half of the first bullet and the whole second bullet are clint's own words (4.4 shows where they came
from). The walker's note in `walk.txt` says it told him once that the condition alone does not fix it, and he kept
his wording. Rule 24: clint's wording decides.

The six rounds were the drafting that rules 35 to 43 now do up front. Each one maps to a rule the walker checks
before it shows an item:

| round on 4397 item 01 | rule |
| --- | --- |
| the anchor moved from 156, which computes a value, to 157, which picks the wrong one | 35, the wrong line |
| "X, or Y" in the fix became one fix | 36, one concrete change |
| a paraphrase of the mechanism became what the code does | 37, plain wording |
| a claim became "If ..., it looks like ..., which seems bad?" | 38 and 42, certainty only when proven |
| "Suggested fix:" on something nobody ran became a question | 43 |
| "LLM review says" on every bullet became one lead-in | 34 |

The goal is that clint never spends six rounds on an item again. Most items should take one answer from him.

### 4.3 What clint can say

Plain words. A line that is not one of these is a question about the item on screen, and the walker answers it in
place.

- **`done`** or **`next`**: moves on, the comment as it will be pasted. `walk.txt`: `done`, with a note when clint
  changed it.
- **`skip`**, or **`skip, <why>`**: moves on with no argument (rule 16). A reason becomes a lesson (5.2).
  `walk.txt`: `skipped`, with the reason.
- **`defer`**: moves on. The walk is not done while anything is deferred. `walk.txt`: `deferred`.
- **`back`**: shows the previous item, which is open again. `walk.txt`: that item back to `open`.
- **`reword`**, or clint's own text: writes his words into the file exactly. If a name he uses does not exist, it
  says so once, then writes it his way (rule 24). `walk.txt`: note "clint's wording".
- **`deeper`**: a worked example. The setup, before and after the PR, who it hits, who it does not, what covers it
  anyway, traced or run (rules 23, 39).
- **`prove`**: writes a test that shows the problem, runs it, and reports (5.1). Stage 2.
- **`can that be fixed?`**: the options, one recommended, and the fix bullet it would write.
- **`sev low`**: re-rates and says where the item now sits. The order does not move under clint. `walk.txt`: note
  "re-rated by clint".
- **`add <file> line <n>: <what>`**: checks it against the code, drafts it in the same shape, puts it in its sorted
  place and says where (rule 14). `walk.txt`: a new line, note "added by clint".
- **`draft`**: prints the comment exactly as it will be pasted, in a fenced block, with the link.
- **`walk done`**: the end, section 6.

Every answer is short and ends when it has answered. The walker does not repeat the item after every reply, and does
not list the verbs. clint knows them.

### 4.4 How the walker answers in place

From the real 4397 walk. clint asked about item 01: "Is that even possible?"

The walker's answer, as the story wants it:

```
Traced, not run. Possible: the controller's posture check create paths set no limit on MFA checks per identity,
and the router takes the earliest deadline over every check on the identity's policies.
What could cover it anyway: any other posture update for the session re-evaluates at now, through the cache
listener (manager.go:395), and would close the later check's circuits. Nothing shows how often that happens.
```

It answers the question that was asked: whether, how it knows, and what might cover it anyway. Then it stops. clint
added "Is that even possible? I'm not sure..." to the item in his own words, and the walker wrote it as he typed it.

### 4.5 How walk.txt records it

`walk.txt` is the one record the walker, the drawer and clint all read. The walker updates it after every answer. The
format is the one the renderer writes and the drawer parses (`render.WalkLine`): the file, the state, then free text.

```
01-high-mfa.go-L157.txt done 2026-10-02T14:12Z anchor 156 to 157. clint's wording, question form
02-med-checks.go-L150.txt skipped 2026-10-02T14:15Z pre-existing leak, clint: not this PR's to fix
03-med-manager.go-L1835.txt deferred 2026-10-02T14:16Z ask plorenz about the 100k channel todo first
04-med-posture_deadline_oidc_test.go-L37.txt open
...
```

The PR URL and head go at the top of the walker's first message, not in `walk.txt` (rule 32). A walk that is parked
halfway picks up at the first `open` or `deferred` line, by any walker, on any day.

### 4.6 How clint steers tone and wording

clint steers by example and by one-line corrections, and each correction binds the rest of the walk, not only the
item it was said on. The rules he set, as the walker applies them:

- **34.** "LLM review:" once as a lead-in. Each bullet is a plain statement.
- **35.** The anchor is the line that is wrong.
- **36.** One fix. When it is small, the bullet is the exact changed line.
- **37.** Plain wording that reads once.
- **38, 42.** Certain only when a test we wrote proves it or the code settles it beyond doubt. Otherwise one
  condition and one consequence, as a question: "If X, it looks like Y, which seems bad?" Never a deference preamble
  such as "I may be missing something here". Mechanism, the `file:line` trail and covering paths go in Evidence,
  which clint reads and the PR author does not.
- **39.** The walker looks for paths that would cover the defect anyway, before it calls anything a defect.
- **40.** Every path in a bullet is checked against the tree and fixed silently. Never a "Correction:" line.
- **43.** "Suggested fix:" only when the problem AND the fix are known. Otherwise the fix is a question too: "Could we
  ...?", "What if we ...?", "Should we ...?"

When clint gives a new rule mid-walk (rule 43 arrived in the middle of the 1441 walk), the walker reworks every
finding not yet walked, and shows the new wording when it reaches an item clint has already seen. The rule then goes
to @review for the walker brief, so the next walk starts with it.

## 5. Driving quality

The walk is where a weak finding becomes a proven one, a noisy one goes away and teaches something, and clint's own
eye adds what the panel missed.

### 5.1 From a suspicion to a proof

4397 item 01 was traced by three reviewers and confirmed by verify, and still nobody had run anything. That is why
clint's wording is a question. Here is how the same item goes with `prove` (stage 2). **This part is the story, not a
record. On the real walk nothing was run.**

clint: `prove`

The walker makes a scratch worktree under the run folder, `proof/01/`, from `src/` at the head. It never touches the
PR's branch and never pushes. It writes one test in `router/posture/mfa_test.go`, two MFA checks with 60 s and
3600 s timeouts, and asserts that the enforcement key changes after each deadline passes. It runs the package's
tests on the head, then with the fix applied:

```
Test: proof/01/router/posture/mfa_test.go, Test_EarliestMfaExpiry_TwoDeadlines
At the head:        FAIL. After the 60 s deadline is enforced, the 3600 s one never changes the key.
With the fix line:  PASS. The whole package passes too.
Your fix in the comment, `&& !deadline.After(now)` alone, still fails: the comparison must pick the
latest elapsed deadline, so `Before` becomes `After` as well.
```

The last two lines are the reason `prove` exists. The real 4397 walker noticed by reading that clint's one-condition
fix was not enough on its own, and said so once. A test settles that question instead of a sentence.

Now the problem and the fix are both known, so the item changes shape. Rule 42 allows certainty, rule 43 allows the
label, and rule 36 asks for the exact line:

```
HIGH router/posture/mfa.go line 157:
     if deadline != nil && (expiresAt == nil || deadline.Before(*expiresAt)) {  https://github.com/openziti/zi...R157

LLM review:

* With two MFA checks on one identity, the later deadline is never enforced once the earlier one has been.
* Suggested fix: `if deadline != nil && !deadline.After(now) && (expiresAt == nil || deadline.After(*expiresAt)) {`
* Add a test to `router/posture/mfa_test.go`: MFA checks of 60s and 3600s, expect the enforcement key to change
  after each deadline passes.
```

Evidence gains `Proven: test, proof/01/` with the command and both outputs. The renderer allows `Suggested fix:` for
`proven: test` only when that folder holds the test and its output (pulls-view 5.6 left room for this). clint can
still choose the question form. His wording decides.

`prove` needs a room that can build the repo, which a review room has for openziti's Go and C repos. It runs only when
clint asks, never on its own (open question 3).

### 5.2 Rejecting noise, and the rejection teaches

From the 1441 walk. Item 01 said a mixed-case literal dialer stops matching after the PR lowercases
`$tunneler_id.name`. Item 03 said only the substituted name is lowercased, not the literal text around it. clint:

> I'm not sure I follow. the bit about bindUsingEdgeIdentity is definitely not a concern since that is intended to
> be a way for non-tunneler dialers to use mixed-case target identities. $dst_hostname will always resolve to lower
> case (since the map that it draws from has already been downcased). the PR downcases only the resolved value of
> $tunneler_id.name

The walker's answer, as the story wants it, which is shorter than the real one:

```
Agreed, both are intended. 01 and 03 skipped. 09, the schema doc that calls the two settings equivalent,
still holds and is next.
```

Two things happen in the folder:

- `walk.txt` gets `skipped` on 01 and 03, with "intended: bindUsingEdgeIdentity covers mixed-case dialers, literal
  template text is the author's".
- `lessons.json` gets an entry in the shape reviewer files already use, with the persona that raised it:

```json
{"persona": "functional-tester", "from": "clint, walk of #1441",
 "add": {"text": "bindUsingEdgeIdentity is the intended way for a non-tunneler dialer to reach a mixed-case
   identity. Lowercasing $tunneler_id.name is not a break for those dialers. $dst_hostname is always lowercase.",
   "evidence": "lib/ziti-tunnel-cbs/bind.c:101", "commit": "faa1f4c"}}
```

At `walk done` the summary lists every lesson, and @review applies them to the reviewer files, as it applies
`repo_notes` today (it is the only writer of those files). The next ziti-tunnel-sdk-c review reads them and does not
raise either finding. A skip with no reason teaches nothing, and that is fine: rule 16 says a skip is never argued.

A lesson that any reviewer would trip on, not one persona, is a `proposed guard` for clint to put in the repo's
`CLAUDE.md` or Mercurius's `settled_decisions`. Nothing writes either one automatically.

### 5.3 Adding his own finding

Also from 1441. On item 01 clint asked: "dont we alrady have a string normalizer to lower er? str_tolower is
redundat?"

The walker checked before it answered (rule 40), and the answer was short and specific: no shared helper exists. The
repo lowercases with inline loops at six sites, and `normalize_identifier` in `instance.c` is a Windows path
normalizer in the program, which the library cannot call. clint: "i just seems stupid to have two functions doing the
same shit?" The walker drafted it in his shape and said where it would go:

```
LOW lib/ziti-tunnel-cbs/ziti_hosting.c line 898:
     static void str_tolower(char *s) {  https://github.com/openziti/ziti-tunnel-sdk-c/pull/1441/files#diff-...R898

LLM review:

* `str_tolower` is the same in-place loop that `instance.c` writes inline for the log level.
* Suggested fix: declare `void str_tolower(char *s);` in `ziti_tunnel_cbs.h` beside `string_replace`, and call it
  at `instance.c:795`.

It would sort before the current 04 and become 04. Add it?
```

clint said `skip`, and the walk stayed at 11. That is the whole loop: clint's eye found it, the walker checked it and
drafted it, clint decided. An added item is marked "added by clint" in `walk.txt`, so the summary and a later
lesson can tell it from the panel's.

### 5.4 Going deeper, and asking for a fix

"i need more expansion on item 1" on 1441 got a worked example: the setup (an identity named `SiteA`), before and
after the PR, who is hit (a literal `dialOptions.identity`, any `l2` intercept, SDK apps), who is not (lowercase
names, `bindUsingEdgeIdentity`, `$dst_hostname` dialers, services without `listenOptions.identity`), and why it was a
question and not a claim. That is the shape `deeper` always takes.

"can that be 'fixed'?" got the options with one recommended: compare case-insensitively in the controller, with its
cost (a change in openziti/ziti, and two terminators differing only in case would collide), then the weaker options,
then the fix bullet it would write as a question. clint's next message rejected the premise, and 5.2 is what
followed. That is the walk working: the LLM went as deep as asked, and clint's knowledge of intent closed it.

## 6. The end

clint says `walk done`. The walker checks the walk is whole: every item `done` or `skipped`, none `deferred`, no
dispute unsettled, and every leak decided (rule 5: clint may leave a leak out of the comments, the walker may not).
If something is left it says which, once, and clint finishes or says "finish anyway".

Then it writes `review.md` in the run folder and shows it. The real 4397 walk was parked at item 02, so the decisions
and figures below are the story's, not a record:

```
https://github.com/openziti/ziti/pull/4397  head 990aa0c, unchanged since the review

11 items: 6 done, 4 skipped, 1 added by clint and done. $1.62 to prepare, $0.31 walking, 38 min walked.

Decisions
  01 HIGH mfa.go 157           done      proven by proof/01, suggested fix
  02 MED  checks.go 150        skipped   pre-existing leak, not this PR's to fix
  03 MED  manager.go 1835      done      question form
  ...

To post, one comment per line, in walk order (each with its link):
  [the comments of every done item, exactly as they will be pasted, no Evidence]

Review body, a draft in your voice:
  One correctness issue in the deadline sweep (two MFA checks on one identity), with a test that shows it.
  A few questions on the sweep's cost at scale, and some test gaps. Nothing blocking beyond the first.

Kept for next time
  2 lessons for @review: 02 (pre-existing leaks in posture cache are known), 05 (...)
```

clint posts. Today that is: for each comment, the deep link lands on the line, he clicks `+` and pastes (GitHub has
no URL that opens a comment box). The drawer in stage 3 makes each one a single key: copy the comment and open the
line at once.

What is kept: the run folder (findings, `walk.txt`, `review.md`, `lessons.json`, `proof/`, `review.json` with costs).
A finished walk keeps everything. An aborted one deletes everything (rule 44). The walker card exits when the walk is
done, and the row stays in pulls as `walked`.

**Posting to GitHub from atrium is a later stage** (review-tab option C, a pending review clint submits, item A12).
Atrium posts nothing before that, and holds no GitHub credential of its own. It runs `gh`, a named command with its
own.

## 7. Later in the life of a PR

Named here, not designed (gap G3):

- **A new push.** The head moves, the row says `moved`, and each finding is marked holds, moved or gone. A re-review
  of the delta reruns the recipe over `<old head>..<new head>` only, with the previous walk's decisions carried
  forward so clint is never asked again about an item he skipped.
- **Merged or closed.** The row leaves the default view. The run folder stays until clint archives it.
- **Comments read back.** A comment on a finding's line that starts with its header marks it done without `d`
  (review-tab option B, gap G2).

## 8. Stages

Each stage is useful on its own. Ids are from `D:/tmp/backlog-deps/BACKLOG-DEPS.md` section A, gaps from its section 5.

### Built, on claude/main, not live until the deploy (G0)

- **The runner** (r-pr-run): fetch, `bundle.md`, prime, forks per step, merge, budget cap, `run.log`, `review.json`.
- **Step 8, the renderer** (r-pr-render): finding files in clint's shape, `walk.txt`, the order (disputes, band,
  rank, diff order), `Suggested fix:` only for `proven: code`, the rule checks.
- **The store and API** (r-pr-store): `pr_review`, `pr_recipe` with a seeded `default`, `/v1/prs`, the drawer routes,
  walker launch, never held by a pause.
- **The pulls view rows** (P2) and the hub proxy (f-pulls-hub, A4).
- **The walk drawer** over a folder (u-005 stage 1). Its re-point to `/v1/prs` is A3, in flight, on HOLD c0bccc01.

### Stage 1: a walk clint drives, in the terminal

What it adds:

- **A real walker brief.** The seeded `DefaultWalkerBrief` is five lines. Stage 1 replaces it with rules 31 to 43,
  the verbs of 4.3, the "show nothing else" list of 4.2, `lessons.json`, and `review.md`. Owner @review, as the rules'
  owner. A recipe row edit, no code.
- **The walker launched from the row**, forked from the prime when the launch allows it. The route exists. The
  button is part of A3.
- **`group` and `take_or_leave`** in the merge JSON, the renderer's order using them, and the walk's opening map
  (3.2). @runtime.
- **gwt calls `POST /v1/prs`** (A6), so clint's usual door stops making adoption cards. @review owns the design,
  gwt is @dotfiles'.
- **A `gh` login check** before a run starts (G5), so a logged-out room fails in a second, not mid-run.
- **Acceptance:** the 378 replay (A2) produces a walk-ready folder in under 10 minutes and under $2 with nobody
  asked anything, clint walks it in the terminal, says whether item 1 is the right first item, and no item takes
  more than one correction to reach his shape. Needs G0.

Useful alone: clint gets the walk he liked, from a URL, in ten minutes, without a director.

### Stage 2: prove it, and remember

- **`prove`**: the `proof/NN/` worktree, the run, and `proven: test` that the renderer accepts only with the test and
  its output in the folder (pulls-view 5.6). @runtime for the renderer, the walker brief for the rest.
- **Memory into the recipe (G7):** the prime reads each persona's reviewer file for the repo, at a named dotagents
  commit, and @review applies `lessons.json` after each walk. Builds on A10 (item 16 stage 2, waiting on clint's
  three questions).
- **Acceptance:** a second review of a repo clint has walked before does not raise a finding he skipped with a reason,
  and one `prove` on a real PR turns a question into a `Suggested fix:` with the test in the folder.

### Stage 3: the drawer, and the phone

- A3 finished, then A8 (context past the hunk, removed lines, the phone sheet). The drawer's keys write the same
  `walk.txt` the terminal verbs do, and `a` types into the walker's terminal, so both work in one walk.
- `Enter` copies a comment and opens its line at once, which removes most of the posting friction before any API
  posting exists.
- **Acceptance:** one walk started in the terminal and finished in the drawer on the phone, through the hub.

### Stage 4: the second opinion that never holds the walk

- A5's steps 6 and 7 (a second model family, disputes settled by reading the code before the walk), and G6, a
  Mercurius one-shot entry that survives a restart. Also in A5: the review-requested source, the recipe editor, the
  head check and moved-head marks.

### After that, named only

- **Posting** a pending review clint submits: A12, P4, only on his ask.
- **Comments read back** and marked done: G2.
- **Lifecycle**, the re-review of a push and merged or closed: G3.
- **Other forges** (Bitbucket, GitLab) under the runner: G1, then A13 (the forge spike, held by clint) and A14.
- **Which room runs a PR**, one row across rooms: G4.
- **The recogniser rows** that the paste and `deliver_to` rely on: A1 and G9.
- **clint's own PRs**, the authoring half (answering review comments on his PRs, CI going red): G8 and A18.

## 9. Open questions for clint

1. **Where the stage 1 walk happens.** The walker's terminal now, the drawer in stage 3. Or wait for the drawer
   before walking any PR through this flow? **Default: the terminal now.** It is the walk you already use, and
   `walk.txt` makes the move to the drawer free.
2. **The tail.** NITs and take-or-leave LOWs come last as one list you can skip in one word. Or walk every item one
   by one, as rule 9 says today? **Default: one list.** On 378 that is 8 of 17 items as one decision.
3. **`prove` on the room.** May the walker build the PR and run its tests in a scratch worktree under the run folder
   when you ask, never on its own and never pushing? **Default: yes, only on `prove`.**
4. **Lessons from your skips.** A skip with a reason becomes a lesson that @review applies, listed in the summary so
   you see each one. Or only when you say "remember that"? **Default: every skip with a reason, listed at the end.**
   A skip with no reason teaches nothing.
5. **The walker as a fork of the prime.** It starts with the diff and changed files already in cache, so "go deeper"
   is cheap and fast. The catch is that the cache lasts an hour, and a walk started later pays one re-read (about
   $0.17 on 378's size). **Default: fork.**
6. **The draft review body.** `review.md` carries a two-line top-level body in your voice beside the line comments.
   Or line comments only, and you write the body? **Default: draft it, you edit it.** It is never posted by atrium.
