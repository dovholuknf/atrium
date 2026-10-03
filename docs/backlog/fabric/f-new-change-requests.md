# f-new-change-requests

Status: built on claude/f-change-requests (6f2bbecd store, 6a379c16 gitsync, 6bdf367a link, then the docs), awaiting
review. Hub forge stage 5, hub half: the "Pushed" read and change requests between rooms. Not landed.

- Endpoints, fields and the `change-request` event are in changelog/fabric/2026-10-02-f-new-change-requests.md and were
  written to follow the API drafted to @UI (a deviation is listed below). Test plan: docs/changes/f-new-change-requests.md.
- Stage 5 is manual: no merge button, no queue. `merged` is the operator's, and the hub checks the sha is on the target in
  its own store.

Decisions and deviations, for the review:

- A card is believed only on the hub's own machine, by `X-Atrium-Card` and `X-Atrium-Card-Room`, because the headers are
  claims. Any other reach naming a card is 403. The room header is new (the brief named only the card).
- "Same gate as snooze": the snooze route has no check of its own beyond the listener and the zrok share's login, so the
  change-request writes add the cross-origin check the documents routes ask and nothing stricter. A zrok public share
  behind its login can write, as it can snooze.
- The owner of a room's branch is a worktree-folder-name heuristic (`claude/f-x` is the live card in `.../f-x`), because
  room cards carry no branch. None or two is no owner. A room could send its branch on the card payload to make this exact.
- `state=closed` lists every finished request (merged and withdrawn too), as one filter, since the brief gave three states
  and four exist. `state=merged` and `state=withdrawn` also work.
- A question growler needs a room. Its room is the source room, else the owner's, else the creator's; a main request the
  operator made on a hub-pushed branch with no owner has none and raises none. The growler is a new store primitive
  (`GrowlRaise`, `GrowlEnd`) because the room growler is one per room and reason and a card sync ends rows with a card id.
- The owner is not told of a change it made itself, nor of a main request (the question is the operator's).
- 15 s bound on every read of the hub's store and of a room's refs. A room that is not attached or does not serve git is a
  503 on create, never a 404.

Not done: nothing is re-read from a room after the request is made. `source.sha` is the tip when asked and `pushed` says
how the hub's branch has moved.
