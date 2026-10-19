Docs: internal/api/web/js/link-opening.js header, internal/api/prworktree.go

## Test plan

## @LETTER@. Opening a pull request by link says what it is doing

### @LETTER@1. The popover paste draws a card

1. On the Pull requests page choose "Paste a PR", paste the URL of a pull request and press Open.

**Expected:** the popover closes at once and a card for the link shows at the top of the page with the step the room is
on. A failure stays on that card with its reason, and `edit`, `again` and `dismiss` work as they do for a board paste.

### @LETTER@2. A slow step counts seconds

1. Open a pull request in a large clone, so the fetch takes longer than three seconds.

**Expected:** the step line reads "fetching the head of pull request N… 14s" and the seconds climb once a second. A
new step starts its count again.

### @LETTER@3. The fetch does not collect garbage

1. Open a pull request in a clone with many loose objects and watch `git` in the process list.

**Expected:** no `git gc` or `git maintenance` child runs after the transfer, and the open does not wait on one.
