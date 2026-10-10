## Test plan

## @LETTER@. Ended cards on the Terminals tab

### @LETTER@1. The group is closed and counted

1. Let a worker finish with atrium_done and leave, so its card is `done` with no runner.
2. Open the Terminals tab.

**Expected:** under the rows there is an `ended` heading with a count, closed. No ended rows are drawn.

### @LETTER@2. Open it and resume

1. Click the `ended` heading, then reload the page.
2. Read the row, then click `resume`.

**Expected:** the group is still open after the reload. The row shows the title, how long ago it ended and `done <sha>`
(or `blocked: ...`, `report: ...`, `exited`, `crashed`). Resume starts the card again on its conversation.
