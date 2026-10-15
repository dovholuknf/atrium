- The launch dialog's `repo` button lists this room's own checkouts (from its cards, newest first) and the hub's repos,
  with a filter. A repo the room has no checkout of offers "clone from hub", which clones from the hub mirror into
  `<git.scm_root or ~/git>/<host>/<org>/<repo>`. It no longer reads providers (launch-repo-picker).
- Text selection in the launch dialog was probed in every input, on label text, across the backdrop, by double-click
  and by shift-click, and is covered by a unit.
