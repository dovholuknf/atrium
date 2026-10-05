# m-new-deploy-queue: a deploy queue, made visible

The landed commits that still need a deploy are now one generated list, not something rebuilt from `git log`.

- `GET /_hub/deploy-queue` lists each landed commit not yet live, oldest first: sha, subject, the item its changelog
  entry names, and whether it needs a `hub` deploy, a `room` deploy or `both`, with the rooms that are behind. Add
  `?format=md` for the table HANDOFF carries.
- Live is what each process reports as its running commit. The hub reports its own build. A room now sends its commit in
  the hello and the hub keeps it with the attached room (`commit` in `/_hub/rooms`). A room built before this change
  sends none, so it is named under `unreported` and taken as exactly as far behind as the hub. That over-lists and never
  hides a commit, and it clears when that room is deployed once.
- A commit that touches only hub-only code needs the hub. Other shipped code needs both, since the room restarts on the
  same binary. Docs, tests and review commits are not listed, the same rule the deploy-ready pill uses.
- The deploy-ready dialog (the pill's click) shows the queue under the report.
- `deploy-ready.ps1` and `deploy-batch.ps1` print the queue before they change anything and save it as
  `DEPLOY-QUEUE.md` under the atrium directory for HANDOFF. A hub that does not answer is said and the deploy goes on.

Nothing here deploys. The endpoint reads git and the attached rooms and writes nothing.
