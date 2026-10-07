A card a link opened can be closed, which frees what its inventory lists. "close" is the operator's word for it
everywhere, since "finish" is an agent reporting its own work over. `GET /v1/tasks/{id}/close` shows what goes, what
is kept and what has to be asked, and `POST /v1/tasks/{id}/close {confirm, answers}` closes it: the session is
stopped, the review is archived with its run folder set aside as `<folder>-closed-<when>` minus `src/`, then the
worktree, its branch and the fetched PR ref are removed, and the card goes to done. A review the card walks that its
inventory does not name is archived too. A row that does not free stays live with what git said, and the rest carry
on. A worktree with uncommitted changes, or commits on no remote and not on the hub, is asked about with no default:
keep (stays on disk with its branch), stash (dirty files in one WIP commit, pushed to the hub as
`stash/<card short id>/<branch>` with a token minted for that push, then removed, and the card records the stash) or
delete. A stash that does not land keeps the worktree. A re-paste of a closed link makes a fresh review and card, also
at the same head. A card tagged `link:` is no longer exited by its own done report. The doors: "close…" on the card
menu, "close" on a pulls row with a walker, `atrium close <card>` (a preview, then `--yes` with `--keep`, `--stash`
or `--delete` per worktree seq or path, or `all`), and the `atrium_close` MCP tool. Design:
`docs/rnd/card-lifecycle-design.md` section 7 and Interview Q3 to Q5, phase 6.

Test plan:
- Open a pull request, then `atrium close <card>`: the preview lists worktree, ref, branch and review and changes
  nothing. `atrium close <card> --yes` removes the worktree and branch, archives the row, and the card is done.
- Open it again, write a file in the worktree, and close from the pulls row: it asks keep, stash or delete. Keep
  leaves the worktree on disk and the card lists it.
- With git.push hub, close a dirty one with stash: the hub has `stash/<card>/<branch>` with a WIP commit on top.
- Re-paste the closed link: a fresh card and review start.
