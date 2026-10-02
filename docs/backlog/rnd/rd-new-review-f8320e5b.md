# Review: rnd-new-review-quality f8320e5b

`docs/backlog/rnd/rnd-new-review-quality.md`, the only file in its commit, on c4af3b8a. It lands by cherry-pick.

Verdict: **HOLD** on two pointers. Point 3 of the request is whether each steal points at the right doc, and two do
not. Both are one-line fixes.

## Names and quotes: pass

- No tool, vendor, company or person is named.
  - "Published studies" and "a tool that tried it turned it off" stay anonymous.
  - PR 378 is the repo's own reference, already used in rnd-new-pulls-view.md and QUEUE.md.
- Nothing is quoted.
- The survey file is ignored by `.gitignore:51` (`*-comparison*.md`), as stated, and this item does not depend on it
  to be read.

## Pointers

Checked against claude/landing:

| steal | points at | holds? |
| --- | --- | --- |
| 1 finding bar | pulls-view's stored recipe, and review-on-atrium section 7 | yes (section 7 is the standing reviewers) |
| 2 addressed rate | change-record finding states, and review-on-atrium 7.4 | yes (change-record 172, and 7.4 is the knowledge base) |
| 3 tools step | pulls-view, before prime, and the lifecycle's tested gate | yes |
| 4 confidence | pulls-view's verify and merge steps | yes |
| 5 incremental | pulls-view "head reviewed" (its row shows the head it was reviewed at), and change-lifecycle 2.1 | yes (2.1 is "which hunks reopen") |
| 7 never-report list | the told-off list in review-on-atrium 7.4 | yes (`false-positives.md`) |
| 9 cheap triage | `docs/rnd/opencode-token-routing.md` | yes |
| **10 blast radius** | "the PR review story, sections 3 and 4, not landed" | **no path**. It is `docs/rnd/pr-review-story.md` on the orchestrator's branch `claude/pr-review-story`, which is how `langchain-openwiki-spike.md:125` cites it. Give the path and the branch |
| **11 replay set** | "pulls-view acceptance, `docs/backlog/release/m-001.md`" | **wrong doc**. m-001 is "evaluate every test for efficacy". The 378 replay is the pulls-view acceptance: `docs/backlog/rnd/rnd-new-pulls-view.md`, and QUEUE.md P1c ("the 378 acceptance replay"). Point there, and drop m-001, or say why it is related |

## Lows, wording only

- **Unsourced claims, item 4 and "Why".** "a model asked to judge severity does little better than chance" and the
  addressed-rate claim are stated as fact with no source. The source is a survey that stays local, so say "the
  survey found". That keeps the claim honest without naming anybody.
- **Item 4's 80 cut-off.** It is also question 2. Mark it as the starting value, which question 2 settles.

Atrium-Verdict: hold c4af3b8a..f8320e5b
Quality: a tight, useful item that names nobody. Two pointers need fixing.

## Re-read: 74d44a19

Range `c4af3b8a..74d44a19`. The fix is one commit on f8320e5b, and only the item changes.

Closed:
- **Item 11.** It now points at the 378 acceptance replay, in `rnd-new-pulls-view.md` and QUEUE.md P1c. m-001 is gone.
- **Item 10.** It cites `docs/rnd/pr-review-story.md` on `claude/pr-review-story`, marked not landed.
- **The Lows.**
  - "The survey found" now comes before the severity claim and both addressed-rate claims.
  - 80 is marked as the starting value, which question 2 settles.

The item still names nobody and quotes nobody. One nit: the first bullet under "Why" now runs past the 120-column wrap.
Fix it whenever the file is next touched.

Verdict: doc-ok (re-read, c4af3b8a..74d44a19)
Quality: the fix is clean and the pointers are now right.

Atrium-Verdict: doc-ok c4af3b8a..74d44a19
