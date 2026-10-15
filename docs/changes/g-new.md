Work starts the way `gwt new some.new.work` starts it: one call, from a checkout or from the board.

`POST /v1/new {repo, name, from?, prompt?}` on the room (`internal/api/newwork.go`) updates the repo's origin, makes a
worktree through `branchWorktree`, runs the repo's setup hook and launches a card there. `atrium new <name> [--from <ref>]
[--prompt <s>]` reads the repo from the checkout's origin and calls it. The launch dialog's `repo` button opens the same
thing inline.

Where the branch starts, in order: `--from` (origin's copy first, then the ref as given), `origin/<name>` when the
remote has that branch, origin's default branch. The operator's clone is never checked out or pulled, only its
remote-tracking refs move. A failed fetch is reported on the answer and the base is what was last fetched. The worktree
lands at the provider's worktree root, `<root>/<org>/<repo>/<name>`, or under the scm folder's `worktrees/<host>`.

The setup hook is `<hooks>/<host>/<org>/<repo>/worktree.ps1` (or `worktree.sh`), where `<hooks>` is the
`worktree_hooks_dir` setting, default `~/.atrium/hooks`, and `<host>` may be the full host or its first label. It gets
`-WorktreePath -Org -Repo -RemoteHost` (the `.sh` gets them as arguments) and runs once, in the new worktree. A failing
hook is reported on the answer and fails nothing.

## Test plan

## @LETTER@. New work from a checkout and from the board

### @LETTER@1. From a checkout

1. In a clone of a repo atrium knows, run `atrium new some.new.work`.
2. Run it again.

**Expected:** a worktree named `some.new.work` exists off origin's default branch as it is now, a card runs in it, and
your clone's own branch has not moved. The second run answers the same worktree and card and starts nothing.

### @LETTER@2. A branch the remote has, and --from

1. Push a branch `work-b` to origin, then `atrium new work-b`.
2. Run `atrium new other --from release`.

**Expected:** `work-b` starts at origin's `work-b` and tracks it. `other` starts at `release`. A `--from` that names
nothing is refused and makes no worktree.

### @LETTER@3. The launch dialog, no providers

1. With no providers, open new agent and press `repo`.

**Expected:** an empty state appears inside the dialog with an `open providers` button. No second modal opens. The
button closes the dialog and shows runners, providers.

### @LETTER@4. The launch dialog, one provider

1. With a provider, press `repo`, pick a repo, type a name, optionally a from, and press `start new work`.

**Expected:** the card opens in the new worktree. A refusal is a line in the dialog and the form stays filled in.
`pick an existing worktree instead` and `browse` still work.
