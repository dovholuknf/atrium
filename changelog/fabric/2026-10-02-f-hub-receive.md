The hub's own git store takes pushes (hub forge stage 1, part 2). A room's card pushes branches to
`/git/hub/<host>/<owner>/<repo>.git` over the link `git` kind, as its room and its card, and the operator pushes `main`
from the hub machine or over the overlay. The hub applies plain git rules before any ref moves: only the operator moves
`main` and `claude/main`, a branch belongs to the card that first pushed it, nothing is deleted or rewritten, and a case
twin or a directory/file clash is refused. Every accepted push is a row in a new push log (migration 0008), the board's
repository list now shows each branch's owner, and `atrium rooms git release <repo> <branch>` lets a branch go. Item
f-hub-receive.
After review: a push is written to the log as pending before git runs and settled after, and a hub that died in between
settles it from the refs when it starts, so a branch is never left without an owner. The log's ids are taken inside the
transaction, so a restart or a clock that is behind cannot make a release miss. Nobody pushes into a `git_repos` mirror,
the operator included. A request that names its card twice is refused, and one push asks at most 8 rooms for 10 seconds.
