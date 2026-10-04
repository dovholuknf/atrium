# u-new-review-tab-read-back-comments. Read the PR's comments back and mark a finding done

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G2 of docs-deps. Owned by @ui.

## What is missing

Review-tab option B: after a finding is posted, read the PR's comments back from the forge and mark that finding done
on the walk. Review-tab stage 3 and pulls-view section 7 both promise it "with the doors", but the bullet list of
pulls P3 dropped it, so no item owns it.

## Why it is needed

The walk never learns that a comment was posted. A finding stays open until clint presses `d` by hand.

## Depends on it

- the pulls E2E, whose "walk done" should not need `d`
- P4 pending-review posting (option C), which builds on the same read-back

## Done looks like

- After a post, the board reads the PR's review comments and matches each to a finding by file, line and body.
- A matched finding is marked done with a link to the comment. An unmatched one stays open.
- A comment deleted on the forge reopens its finding, or says it is gone (review-tab open question 6).
- A headless section covers the match, the miss and the deleted comment.
- Needs the forge interface (`r-new-forge-interface`) for the read, so GitHub first.
