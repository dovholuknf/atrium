## Test plan

## @LETTER@. Atrium review, stage 1

### @LETTER@1. Launch a review

1. Run `atrium launch --kind review --base main` in a prepared worktree.

**Expected:** the card carries the tag `atrium:review`, its BRIEF.md starts with the review rules, and the card is not
nudged when its turn ends.

### @LETTER@2. The agent files findings

1. In the review card, have the agent call `atrium_review` with `start`, then `finding` (with `{path, old, new}` edits)
   and `batch`.

**Expected:** only a review card has the tool. Each finding gets a unified diff computed on a scratch copy, a diff
against the merge-base and a line count against the base. `apply` writes the stored edits into the card's directory only,
and a path outside the card is refused.

### @LETTER@3. The review view

1. Open the chat tab and pick the review card.
2. Click "review" in the header.

**Expected:** the button shows only for a card with a review. The left rail groups findings as "Applied for veto" and
"One at a time", each with a category chip, title and state. A finding shows its site, a red and green diff, its note
and the footer lines verbatim. There are no buttons: the human answers in chat. The view refreshes about every 3 seconds
while the card is selected. The route is `GET /v1/tasks/{id}/atrium-review`, which answers 404 for a card with no review.
