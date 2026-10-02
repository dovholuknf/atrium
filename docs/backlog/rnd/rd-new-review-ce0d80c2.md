# Review: interview mode (operator-focus 2.9, stage L6) and the interviewer brief (ce0d80c2, m1mini, 2026-10-02): HOLD (wording only)

One commit: `docs/rnd/operator-focus.md` gains section 2.9, L6 and held questions 6 and 7, and the new
`docs/rnd/interviewer-brief.md`. Questions are held in the doc.

## Your checks

- **The answers file outside every repo, at 0600: right.** It is `~/.atrium/interviews/<id>.md` on the hub, written
  from the hub row, and it follows the factory log's home once clint decides. The design cites it by path and
  paraphrases it. Say the directory is 0700 too, as with records.
- **A changed earlier answer: right.** It reaches the interviewer as "Q2 changed", later questions can be withdrawn or
  rewritten, and the screen marks them. L6's acceptance tests exactly that.
- **Queueing two ahead, marked "may change": right.** A change to an earlier answer must also withdraw or mark the
  queued ones, so the screen never shows a stale "next" as if it still applied. Add that line.
- **Starting only when clint asks: right**, and consistent with his rule. Turning held decision rows into an
  interview, with answers marking them `via: interview <id>`, keeps one list.

## Public repo: the interviewer brief needs three fragments paraphrased (holds)

The brief is the right kind of document: what went wrong and the rules that came out of it, with no transcript. But
section 1 quotes three small pieces of clint's words verbatim: "inverted", "origin and hub", and "a repeat" ("which
he called a repeat"). Under the public-repo rule (rd-new-review-6cf2c9a2, held until clint decides where the log
lives), his own words do not go in this repo, short ones included. Paraphrase them:
- "the wording was back to front";
- "he had already said, in passing, that a clone keeps both remotes";
- "and he pointed out it had been asked before".

The rest (that he found questions hard to follow, that the picture was wrong until question 4) is a description, not
his words, and is fine. The card id is fine too.

## Smaller

- Item 7 of the brief writes his exact words to the answers file. Also say that the interviewer's own cwd is never a
  public repo's worktree, so a stray write cannot land the answers file in one.

## Verdict

HOLD on the three verbatim fragments only. Everything else is OK. A re-read is that one paragraph of section 1. doc-ok
on OK, then the doc lands alone.

Closed: none
Open: Q1 (three fragments), S1 (directory 0700), S2 (queued questions withdrawn on a change), S3 (the interviewer's cwd)

Quality: good. The brief turns one rough interview into rules that will save the next one, and the screen is the
decision list's sibling rather than a second system.
