Change requests between rooms, and the hub's "Pushed" read (hub forge stage 5, hub half). A room that has work another
room, or the operator, should take can now say so on the hub, and the hub keeps the request, tells the owner of the branch
and the boards, and checks a merge against its own store. Stage 5 is manual: there is no merge button and no queue. Item
f-new-change-requests.

Endpoints, on the hub's `/_hub/` API:
- `GET /_hub/git/pushed?repo=github/o/r&branch=claude/x&head=<sha>` answers `{"state","hub_sha","room","card","at",
  "released"}`. `state` is `matches` (the hub's tip is `head`), `ahead` (`head` is a commit the hub's tip is built on, so
  the hub has more), `behind` (the hub has the branch and not that `head`: a head it has never seen is this), `diverged`
  (the hub holds `head` and both moved) or `not-pushed` (no such branch). Read with `merge-base --is-ancestor` in the hub's
  bare repo, no shell, 15 s bound. `room` and `card` are the branch's owner in the push log, `at` its latest push.
- `POST /_hub/change-requests` takes `{repo, source:{room?, branch}, target:{branch}, title, why, change?}` and answers 201
  with the object. 400 for an unknown field, a title over 200 or a why over 4000 characters, or a control character (a
  newline is allowed in the why only). 404 when the source branch is not in that room's served set (room given) or not in
  the hub's store (room omitted). 503 when the room is not attached or does not answer. 409 when an open request for the
  same repo, source and target exists, with that request as the body.
- `GET /_hub/change-requests?state=open|closed|all&room=&target=&repo=` answers `{"requests":[...]}`, newest first, never
  null. `state` defaults to open, and `closed` is every request that is over (merged and withdrawn too). `room` matches the
  source room OR the owner's room, without regard to case. `repo` takes `github/o/r` or `o/r`.
- `GET /_hub/change-requests/<id>` answers the object plus `"pushed"` (the read above for the source branch and the sha it
  had when asked, or null when the hub cannot say).
- `POST /_hub/change-requests/<id>` takes `{"do":"close","note"}`, `{"do":"withdraw"}` or `{"do":"merged","sha"}`.
  A request that is not open answers 409 with the row as it is: it ends once.

The object: `id` (`cr_<n>`), `repo`, `source` (`room` only for a room's branch, `branch`, `sha`), `target.branch`, `title`,
`why`, `change`, `state` (open, merged, closed, withdrawn), `created_by` (`room`, `card`; the card is `operator` when the
operator made it), `created_at`, `closed_at`, `closed_by`, `note`, `merged_sha`, `owner` (`room`, `card`; empty strings
when the hub found none).

Who may do what:
- Writes are the operator, under the gate the snooze route has: the listener's checks, the cross-origin check asked again
  in the route, and for a zrok public share the share's own login. A request that names no card is the operator's.
- A card names itself with `X-Atrium-Card` and `X-Atrium-Card-Room`, and is believed only on the machine the hub runs on
  (where the control server asks from). Any other reach naming a card is refused: a header is a claim, not a proof.
- A card may make a request for a branch of its own room or one pushed to the hub, and may withdraw or close its own. The
  owner of the source branch may close one against it, with a note. `merged` is the operator's alone, and the hub checks
  that `sha` is the target branch's tip or a commit under it in its own store, else 409.
- The owner of a hub-pushed branch is the push log's owner. The owner of a room's branch is the one live card on that room
  whose worktree folder is named for the branch (`claude/f-x` is the card in `.../f-x`), because a room's cards do not
  carry a branch. None, or more than one, is no owner: the hub does not guess.

What it tells:
- Every board, an event `change-request` with `{id, state, repo, title, source, target, owner}`, on create and on every
  change that was accepted, and not on a refusal or a duplicate.
- The owner card, an fyi through the relay's own delivery, with the title, why and note QUOTED as data and said to be data,
  not instructions. Not the card that made the change itself.
- A request into `main` is the operator's to decide, so the owner is not told. A question growler (reason `question`, id
  `cr|<id>`) is raised instead, on the room of the source, the owner or the creator, and it ends with the request. It is
  raised once per request: a finished request does not come back as a question. A request the operator made itself, on a
  hub-pushed branch with no owner, has no room to hang one on and raises none.
- The audit log, `change-request-create|close|withdraw|merged` with the id and nothing else.

A room name is folded to ASCII lowercase when a request is stored, as room names are everywhere else, so `source.room`
reads back lowercase and two asks that differ only in the room's case are one request (409 hands the first back).

Storage: migration 0009 `change_request`, with a unique index on the open request per repo, source and target. The hub
holds a growler about neither a card nor a room's health (`GrowlRaise`, `GrowlEnd`), because the room-growler row is one
per room and reason and a card sync would end a row with a card id.

Not done: no merge button and no queue (stage 5 is manual), and no room-side change. A request is not re-read from the
room after it is made: `source.sha` is the tip when asked, and `pushed` says how the hub's branch has moved since.
