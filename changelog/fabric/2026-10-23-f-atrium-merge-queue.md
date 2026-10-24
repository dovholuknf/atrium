A worker lands its branch with `atrium merge` (or `atrium_merge`): the hub queues it, merges it onto `claude/main`,
runs the tests and answers merged, red, conflict or refused. A conflict starts one slim merger. The Terminals tab shows
what needs a person, `atrium merge --list` the queue, and `atrium merge land` puts `claude/main` on the hub's `main` as
one commit by the operator, leaving `claude/main` as it was. The hub never pushes to the forge. A card can no longer
push `claude/main`, and the merger role is retired. (f-atrium-merge-queue)
