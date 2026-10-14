## Diagnosis

After a hub restart a pane headed with one session showed another session's terminal, and keystrokes went to the
other session. Board-only: the hub proxy and the room route `/v1/tasks/<id>/attach` by the id in the URL and did what
they were asked.

Cause, in `js/terminal-links.js`. `connectTerm(taskID)` schedules its own reconnect from several branches of `onclose`
with `setTimeout(() => { if (term) connectTerm(taskID); }, attachRetryDelay())`. The timer closes over the card it was
scheduled for, and its only guard asked whether SOME terminal existed. A restart leaves retries pending for seconds. If
the pane moved to another card in that time (a click, the restore, the waiter), the old card's timer fired, dialled the
old card, replaced `termSock`, and left the header on the new card. `onData` sends on `termSock`, so input went to the
card named by the socket and not the one named above the terminal.

Fix: every retry goes through `redialTerm`, which returns unless the pane still shows that card. `connectTerm` itself
refuses a card the pane is not showing. As a last line, `sendFrame` reads the card out of the socket's own URL and, on
a mismatch with the pane, sends nothing, says so in the terminal, and closes the pane.

## Test plan

## @LETTER@. A pane never dials another card

### @LETTER@1. Retry after switching

1. Attach a session, then stop its attach from opening (restart the hub) so it is retrying.
2. Before it reconnects, click a different session.
3. Wait past the retry delay.

**Expected:** the header and the terminal stay on the second session and no attach is dialled for the first. Headless
unit `crossedPane`.
