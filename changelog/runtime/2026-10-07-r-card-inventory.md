A card keeps an inventory of what the room made for it, in a new `card_resources` table: its worktree, the fetched
`refs/atrium/pr/<N>`, the branch when the open made it, and its review row. The open verb writes the rows as it makes
each thing, under a pending owner until the card starts, then hands them to the card. A rolled back open marks its rows
freed. A worktree that was already there is not recorded, since it is not the card's. Sizes are measured right after an
open (five seconds at most) and on ask, never on a timer. `GET /v1/tasks/{id}/resources` lists the rows with the
card's disk, and `POST /v1/tasks/{id}/resources/measure` measures again. The card details show "N MB on disk", and a
click measures again. The pulls row shows its walker card's disk beside the cost. Finish (r-finish-pr-review) frees
these rows. Design: `docs/rnd/card-lifecycle-design.md` section 6, phase 5.

Test plan:
- Open a pull request from the pulls tab. `GET /v1/tasks/<card>/resources` lists worktree, ref, branch and review,
  and the card details show its disk.
- The pulls row shows the disk beside the cost.
- Add a large file to the worktree, click the disk chip, and the figure grows.
