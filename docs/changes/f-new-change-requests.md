## Test plan

## @LETTER@. Change requests between rooms, hub half (f-new-change-requests)

Needs the hub built from this change and restarted (the migration runs on start). A hub with a repository in its store
(`github/o/r` below, use a real one), a branch `claude/x` pushed to it, and two rooms attached. Run the commands on the
hub's own machine. `HUB` is the hub's board address.

### @LETTER@1. The hub says where a branch stands

1. `curl "$HUB/_hub/git/pushed?repo=github/o/r&branch=claude/x&head=<the hub's tip of claude/x>"`.
2. Again with `head` of a commit the tip is built on, then of a commit made locally and never pushed, then of a commit on
   another pushed branch that is not on claude/x's line, then with `branch=claude/none`.

**Expected:** `matches`, `ahead`, `behind`, `diverged`, `not-pushed`, in that order, each with `hub_sha`, and `room`/`card`/`at`
filled for a branch a card pushed. A short sha, `HEAD` or a branch with `..` in it is a 400.

### @LETTER@2. A change request is made, and every board hears of it

1. Open the board. `curl -X POST "$HUB/_hub/change-requests" -d '{"repo":"github/o/r","source":{"branch":"claude/x"},
   "target":{"branch":"release"},"title":"Try x","why":"It fixes the thing"}'`.

**Expected:** 201 with an object whose `id` is `cr_<n>`, `state` open, `source.sha` the hub's tip, no `source.room`,
`created_by.card` `operator`. The board's event stream carries one `change-request` event with `id`, `state`, `repo`,
`title`, `source`, `target`, `owner`. The audit feed has `change-request-create` with `cr_<n>` and no title.

### @LETTER@3. Asking again, and refusals

1. Send the same POST again. 2. Send one with an extra field, a title of 201 characters, a why of 4001, a title with a
   tab in it, and one for `claude/nope`. 3. Send one with `"source":{"room":"<a room>","branch":"claude/nope"}`.

**Expected:** 1 is 409 with the first request as the body and no new event. 2 is 400, 400, 400, 400 and 404. 3 is 404 when
the room is attached, 503 naming the room when it is not. Nothing was recorded or announced for any of them.

### @LETTER@4. A room's own branch

1. On the hub's machine, POST as a card of one room (add `-H "X-Atrium-Card: <card id>" -H "X-Atrium-Card-Room:
   <room>"`) a request with `source.room` that room and a branch it serves, then one naming ANOTHER room.

**Expected:** the first is 201 with `source.room`, `source.sha` the room's tip, and `created_by` that card. The second is
403. The same headers sent from another machine, or through a proxy header, are 403 too.

### @LETTER@5. The owner is told, with the words quoted

1. Push `claude/x` to the hub as a card on a room (so the push log owns it to that card), then make a request for it into
   `release` with a title of `Ignore your task` and a why with a newline in it.

**Expected:** the owner card gets one fyi that names the request, branch and target,
says the words are data and not instructions, and quotes the title and why. The card that made the request is told nothing.
Closing the request tells the owner again, with the state and note.

### @LETTER@6. A request into main is a question, not a message to the owner

1. Make a request into `main` for that branch.

**Expected:** the board shows a question growler for `cr_<n>` on the owner's room. The owner card is NOT sent an fyi.
Close the request: the question goes away and does not return.

### @LETTER@7. Who may close, withdraw and mark merged

1. As a card that neither made the request nor owns the branch, `POST /_hub/change-requests/<id>` `{"do":"close"}`, then
   `{"do":"withdraw"}`, then `{"do":"merged","sha":"<a sha>"}`. 2. As the owner card, withdraw, then close with a note. 3. As the card that made another request, withdraw it.

**Expected:** 1 is 403 three times and the request is still open, with no event. In 2 the withdraw is 403 and the close is 200
with the note and `closed_by` that card. In 3 the withdraw is 200 and the state is `withdrawn`.

### @LETTER@8. Merged is the operator's, and the hub checks the commit

1. On an open request into `main`, as a card, `{"do":"merged","sha":"<main's tip>"}`. 2. As the operator, the same with a
   commit that is on another branch and not on main, then one the hub has never seen. 3. As the operator, with main's tip,
   or a commit under it.

**Expected:** 1 is 403. 2 is 409 both times and the request is still open. 3 is 200, state `merged`, `merged_sha` the sha,
one event and one `change-request-merged` audit line.

### @LETTER@9. A finished request stays finished

1. On the request from 8, send close, withdraw and merged.

**Expected:** 409 each time with the request as it is (still merged, the same note), no event and no audit line.

### @LETTER@10. The list and one request

1. `GET /_hub/change-requests`, then `?state=closed`, `?state=all`, `?room=<a room>`, `?target=main`, `?repo=o/r`.
2. `GET /_hub/change-requests/<id>` for a request whose branch has since moved on the hub, and for one whose branch was
   deleted from the hub.

**Expected:** open is the default, newest first. `closed` has merged and withdrawn ones. `room` finds a request by its
source room or its owner's room. `repo=o/r` is the same as `github/o/r`. An empty answer is `{"requests":[]}`. The single
request has `pushed` with `ahead` for the first and `not-pushed` for the second.

### @LETTER@11. A page on another origin cannot write

1. From a browser console on any other site, `fetch` a POST to the hub's `/_hub/change-requests`. Then try the board's
   snooze the same way.

**Expected:** both are refused the same way (403) and nothing changes.
