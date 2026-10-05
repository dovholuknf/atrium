A PR's review now moves with its claim. `POST /_hub/pr-claims/move` (and `MovePRClaim`, which the room handoff calls)
refuses unless both rooms are online, asks the old room for the review as a capped tar.gz (`GET /v1/prs/export`, 64 MiB,
the run folder without `src/`, no links), pipes it to the new room (`POST /v1/prs/import`, a fresh row id and a run
folder under that room's `reviews_root`, a running row arriving `queued` and started so the runner resumes from
`steps/`), moves the claim, then has the old room archive its row and stop its run
(`POST /v1/prs/{id}/archive`, the folder stays). The hub keeps no copy. A failed import leaves the claim and the old
row, and a claim with no review on the old room just moves. The move may name `walker`, the card that moved with the
PR, which is recorded as the new row's walker. Item f-pr-review-move.
