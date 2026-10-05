# atrium_git_push and atrium_git_url on a room's own control server

- A card on any room has `atrium_git_push` and `atrium_git_url`, not only a card talking to the hub's control MCP. Both servers register the same code (`link.GitDoor`).
- A clone with no hub remote, such as one made by hand, is pushed to the forwarder URL for the repository it is, named by its origin or by where it sits under the scm or git root. Nothing is written to the clone's config.
- The hub's adopted mirror of atrium takes work branches. Its integration branch, main, tags and refs outside refs/heads are still refused.
- A room asks its hub's lookup over the git link (`/_hub/git/url`). Against a hub older than that, it reads the hub's store and lists only the hub's branches, and says so.
