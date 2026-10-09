# PR 1166 slop retro (2026-10-08 / 10-09)

Raw material for building review tooling for clint and atrium. Captures what happened, clint's reactions in his own
words, and why each step was taken.

## What happened

- PR 1166 (win32crypto e2ee-tls, FIPS) went up with code Claude wrote and clint reviewed. ekoby's review caught slop
  clint had missed: `tls_is_fips`, `tls_restrict_fips` and `e2ee_restrict_tls` in crypto.c ("do you need all 3?"),
  a restriction call in ext_oidc.c ("why do we do it here?"), and the same in oidc.c and ziti.c ("ditto").
- Root cause of that slop: the only failure was e2ee (OpenSSL 3.0 dialer -> Go FIPS host, 2 of 72 matrix runs).
  Claude applied the FIPS restriction to every TLS context in FIPS mode "to be safe". Normal TLS recovers through a
  HelloRetryRequest, so the extra restriction did nothing. Only e2ee needed it.
- Earlier cleanup commits had already dropped tests that exercised tlsuv instead of SDK code (3998a6f), dropped the
  e2ee-tls parser (3f49cbe), and removed or cleaned comments (8c50190).
- This session: dropped `tls_restrict_fips` and `tls_is_fips`, restored ext_oidc.c, oidc.c and ziti_enroll.c to match
  main, merged 5 new test files into 2, deleted a hold comment, and ran full ctest in both trees plus the Azure matrix.
- clint then asked for a DRY, code smell and slop review of the whole PR. It found dead code, duplicated logic,
  repeated comments, duplicated test helpers and single-user code in a shared header.

## clint's reactions, verbatim

- On the drafted ekoby reply: "look. eugene doesn't knwo waht failures we hit. we need to explain 'i hit this failure
  during this test. this was an attempt at fixing that but was misguided' something like that"
- On a comment in connect.c: "was this a comment we added? i want as few changes to main as possible"
- Same comment: "fine delete teh coment it is kinda gross"
- "have we unslopped ourself now?"
- "i want yiou to review the PR and changes for DRYness, code smells, slop/sloppy code"
- "it's been rough and i've looked like a douche cause i did a poor job revieing your slop"
- "first i want to learn from this experience and build tooling to help me/atrium with this flow"
- From the earlier session (handoff): frustrated by slow, unannounced work; "drop it" on `tls_restrict_fips`; asked
  for the test files to be merged into fewer files.

## Lessons

1. A fix spread to paths that never failed is a guess, and the human ends up defending it to the maintainer.
2. Every line changed against main has a cost. Rewording, recasing and restructuring untouched code all add to it.
3. The human reviewer misses slop in a large AI diff, so the AI has to self-review against the base before showing
   it.
4. Replies to a maintainer need the context they lack: what failed, in which test, what the code tried, and that it
   was wrong.
5. Tests should cover SDK code, go in the existing file for their area, reuse existing helpers, and not copy
   production logic.
6. Commit messages: one behavior line, not a comma-joined list. Claude's suggested message broke this rule.

Folded into dotfiles `agents/principles.md`, `coding.md`, `code-review.md` (2026-10-09).

## TODO

- [ ] Build the slop gate. Not yet: clint said "i definitely want a slop gate but we aren't building it yet. our
      priority is getting this pr unslopped first". Collect requirements from our interactions until then.

## Requirements gathered from interactions

- R1 (2026-10-09) Code references must open the code at that line, in one click. Today, `library/connect.c:982`
  in the terminal links only the file part. Clicking shows a hover card ("click to open it: in atrium's editor, in a
  tab..."), then a menu ("open in atrium's editor / open in a tab"). Two clicks, no line, and annoying. The
  orchestrator (atrium) owns that fix.
- R2 Mode matters:
  - Authoring mode ("we are authoring code"): the reference should open the code at the line, in a popup or a
    link. Not inline text, which makes clint scroll.
  - PR review mode: the reference should go straight to the GitHub PR diff line, where clint can add a comment.
- R3 Findings come ranked by priority (clint: "that output was fine"), and each one must make the referenced code
  easy to see.
- R4 When fixes are proposed, go bullet by bullet and show the code being changed (until R1/R2 exist, inline
  before/after in chat).
- R5 "Bullet by bullet" means ONE finding per message, then stop and wait for clint's call. Claude dumped all 13
  findings in one message, and clint answered "is taht ONE AT A FUCKING TIME????". The gate must page findings and
  never batch them.
- R6 Presentation that worked (clint: "phenomenal"): a finding title with file:line range, a one-to-two sentence
  why, then a Current block and a Proposed block that show only the changed lines, with `...` for the code
  between them. Limit: it stops working when the change is much larger.
- R7 Proposed code must be exactly what goes in the file. Never annotate a snippet with a comment that would not be
  in the code ("// main's line, unchanged" was flagged). Put notes in the prose around the snippet.
- R8 No comment where a nearby log line already says it (the hold's CONN_LOG explains the `return false`).
- R9 ALWAYS make as few changes as possible (clint: "wehn making changes we __ALWAYS__ need to make as few changes
  as possible. ALWAYS...."). A smell fix that adds code is not a cleanup. Drop it, or show it shrinks the diff
  against main.
- R10 The description must match the proposed code. L2 was described as "keep main's two blocks, change only their
  last two lines" while it added a helper and more lines. clint: "this current/proposed is more code and does not
  seem to align with the description?" The gate should count lines changed against the base for each proposal and
  state that count honestly.
- R14 R9 is not "no new code". clint: "adding code is fine when it has 'a reason' are you being overly agreessive
  with 'no new code'?". Claude overcorrected and dismissed L5 without counting lines. The free_tls_contexts helper
  actually SHRINKS the code, and Claude claimed it "would add lines". The rule: every change needs a stated reason,
  and the line count must be measured, never guessed.
- R15 Tag each finding by category (DRY, dead code, comment-only, simplification, test scope, ...). clint: "this is
  a 'dry' fix? categorizing the changes is prolly a good learning".
- R16 Ranking of code shapes: DRY and simple (best) > more lines but simple > DRY but dense. clint: "'more lines but
  simple' is better than DRY but dense but DRY AND SIMPLE is best". Fewer lines should also read simpler. A DRY
  fix that is harder to read is not a win.
- R17 Current/Proposed (R6) breaks down for moves and deletes spread over a file. T1 showed ~30 lines per side, and
  clint: "too many lines to tell - hard to understand the proposed updte". For a move or delete, use a unified
  `diff` block with only the -/+ lines and a line number on each hunk header.
- R18 Red/green diff coloring works (clint: "fucking love the green/red added/removed code"). But a tall message
  scrolls off the terminal. The description sits at the top and clint types at the bottom, so he can't review it.
  In chat: code first, then the description and question last, next to his prompt. For the tool: show the change
  in a side pane or popup, sized to fit, with the decision prompt beside it.
- R19 Footer format, verbatim from clint. No finding IDs (T1/L4) for the human, no line counts he can see in the
  diff, no extra prose:
  ```
  CHANGE DRY  : ctx_guard and e2ee_guard are each defined twice
  SUMMARY     : Keep one copy only.
  ```
  clint: "i don't need '8 lines' i can fucking see that... 'T1' - i don't care about the fucking identifier as the
  human... all the other words you use are just more of the same".
- R20 Minimize vertical space. No blank line plus a separate "Apply?" row (that costs 2 rows). Fold the question
  into the SUMMARY line. Each message is the diff and then the 2-line footer, nothing else.
- R21 Triage by obviousness. clint on T1: "that seems like a no bariner. i shouldn't ecen have to see that :)". The
  gate should auto-apply no-brainers (an exact duplicate removed, no behavior change, fewer lines), list them in one
  batch summary for a glance or veto, and spend clint's one-at-a-time attention only on judgment calls.
- R22 Before any comment edit, the first question clint asks is "net new or existing?". Rewording a comment that
  exists in main is bad (diff churn). Rewording one the PR adds is fine. State "net new vs a0d1575" in the finding
  so he does not have to ask. On T8/T5: "really these are nits but whatever".
- R23 Explain the mechanism before proposing removal. On T7 (a single Catch2 SECTION) clint asked "what's a section
  for?". After the answer, he kept it: the section name states the behavior under test, so it is not dead weight.
- R24 clint names this pass a "self/slop-review". Moving single-user fixtures out of the shared header is hygiene,
  not a nit: "this seems 'dumb' until it's needed we should not needlessly pollute .h files". A helper goes in a
  header only once a second file uses it.
- R25 A blanket "apply" covers only the exact kind just approved. clint said "any other of these just apply too"
  after a .h pollution move. Claude also applied a different T5 part (one anon namespace instead of `static`)
  unseen. clint: "i meant any '.h pollution' to fix not ALL of them we haven't reviewed yet". It was reverted.
  When the scope is unclear, ask. Never widen it.
- R26 A forced side edit is still a separate change. T4 (drop dead params) also reworded the comment that named
  them, and the footer said nothing. clint: "you have also mushed a comment change in there". Call out every
  side edit in the footer. Prefer deleting a stale clause to rewording it.
- R27 A "yes" approves only the last thing shown. Claude showed T4 code, then a comment-only follow-up ending
  "Apply T4 with this?". clint's "yes" was for the comment. Claude applied both, and they were reverted. Also,
  edits to EXISTING comments come after the code changes they depend on, as their own finding. A net-new comment
  that comes with new code can sit in the same diff (clint's correction during T6).
- R28 Do not narrow a general-purpose helper to fit its current callers. T4 proposed hardcoding issue_cert's
  ca/validity params because the one caller passes constants. clint rejected it: it is a helper whose params make it
  reusable later, and hardcoding them breaks any other caller. "Same constant at every call site" is not a finding
  on a helper's natural parameters. Drop it from the linter idea.
- R29 Never condense a diff with "..." elisions. clint on T2: "the condensed version hides the full diff for me. if
  i ask for a full diff give it to me". Generate the real diff from a patched copy (git diff --no-index) and show
  every line.
- R30 By T6 the loop worked for clint ("this process is really working well now... i'm enjoying this"). By then it
  ran: one finding, the full diff, net-new vs base stated, side effects named, code before comments, any trade-off
  (like +2 lines that only pay off in a follow-up) said up front with a recommendation.
- R31 If clint approves a change but criticizes part of it ("that comment fits... even though the comment itself is
  shit"), do not apply it yet. Iterate on the weak part first, then apply the finished version once. Claude applied
  T6 with the bad comment and then fixed it in a second edit. clint: "we should have not made that change before
  and should have iterated".
- R32 Refines R24. A single-user item leaves the shared header unless it is a helper that other tests could
  plausibly use later. If Claude cannot tell whether it is "potentially useful in the future", it asks clint and
  does not decide.
- R33 Judgment questions use the same 2-line footer as findings, one item per message. clint's template:
  ```
  SUMMARY HYGIENE : .h pollution - tls_with_ca is defined in a .h but only has one callsite
  QUESTION        : is it possibly useful to other tests to be able to make a tls context with a provided ca?
  ```
  Don't answer the question for him in a bullet list, and don't bundle several items into one question. Always
  show the code the question is about above the footer: "it's not quite enough unless i can see what i am
  answering".
- R34 An answered question is the approval. After clint answered "yes, reusable" to the R33 question, Claude showed
  the move-back diff and asked "Apply?" again. clint: "the previous question should have answered this". When the
  answer settles the action, apply it and show the diff as done.
- R35 For a move or a shrink, also show the diff against the PR base. Against the working tree, moving crypto.h's
  record macros and inline body into crypto.c read as "just moving them around". Against a0d1575 it showed the
  point: the PR's header addition drops from 2 macros and a body to one declaration. Header priority: public API
  headers (includes/ziti) are hardest to change, then inc_internal, then .c.
- R36 Churn does not outrank correctness. S2 proposed dropping the PR's `#include <string.h>` from e2ee_tls.c
  because main already used memcpy through a transitive include. clint: "it seems wrong to remove for the churn
  reason". Include-what-you-use is the correct state, so "fewer lines vs main" only applies to changes with no
  correctness value.
- R37 One site per message holds even for a single finding. S5 showed a new helper plus its call site, each against
  main and the working tree: 4 views in one message. clint: "too many changes presented at once". Split a finding
  into one site per message and show one diff each. Add the second view only when the first one hides the point
  (R35). Order the sites by dependency: the S5 split showed the call site first, and clint asked "e2ee_failed
  doesn't exist yet tho?". Show a new helper before its callers.
- R11 One change site per message. L4 put two files in one message, with "current:" buried at the end of a prose
  line. clint saw "TWO 'proposed' sections ... and 0 'current'". Each site gets its own message, with **Current**
  and **Proposed** as standalone labels.
- R13 The split format (one site, a kind tag, a one-line why, **Current**/**Proposed** labels) got a plain "yes"
  on the first try (L4a).
- R12 Say up front what kind of change it is (comment-only, code, test). clint had to ask "just comments changing?".

## Tooling ideas (not built)

- Slop gate: a skill or atrium step that diffs against the PR base, not HEAD, and runs the `coding.md` checklist
  before any diff reaches clint. It flags duplicated blocks, helpers that already exist nearby, the same comment
  in several places, and dead branches.
- Diff budget: lists every line changed against main that is comment-only, case-only or whitespace-only, or reworded
  without a behavior change, so each one is a deliberate keep.
- Scope check: for each call site the fix touches, require the failing test or run that proves the site needed it.
- Slop linter (clint, during T4): a diff-scoped script against the PR base that auto-fixes or flags the mechanical
  findings, so review only covers judgment calls. Candidate checks: capitalized sentence starts in net-new comments,
  header helpers with a single user TU, `static` mixed with an anon namespace, duplicated struct/function bodies
  (jscpd or PMD CPD), a Catch2 TEST_CASE with one SECTION
  (flag only, see R23). clang-tidy covers parts: misc-definitions-in-headers, misc-unused-parameters.
- Maintainer-reply drafter: requires the failure, the test, and what changed before it writes a reply.
