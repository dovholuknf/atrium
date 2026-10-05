A PR is now placed on the least busy online room (fewest running sessions, a tie to the lower room name), and the hub
keeps one claim per PR key across rooms (host/org/repo/number, migration 0010_pr_claim). A room asks the hub before it
makes a PR row. The owner makes the row, another room makes nothing and is told the owner, and a hub that cannot be
reached makes the row `claim: pending` (room migration 0081) and asks again when the room reattaches. The pulls view
folds two rows with one key to the owner. A claim whose room is offline is never re-placed: the board's growler shows a
WARNING naming the PR and the room, and `POST /_hub/pr-claims/move` is the one way an owner changes. Nothing polls and
nothing takes a webhook. Item f-new-pr-room-and-dedupe.
