Docs: none, website/docs/files.md is updated on this branch

New setting `open_paths`: absolute directories, one per line, empty by default, in settings under the picker's box.
Files under them, and under an atrium path, open from any card's terminal. An atrium path is the room's card root, the
directory of every card on the room live or archived, the room's repository clones and worktrees, WORKTREE_ROOT and
each provider's worktree root. The probe, `files/view`, `files/launch`, `files/list` and download accept them, resolved
through `safepath`. The editor, `files/open`, upload, delete and zip stay against the card alone.

## Test plan

## @LETTER@. Opening a path outside the card

### @LETTER@1. An atrium path

1. In card A's terminal, have the agent print the absolute path of a file in card B's directory.
2. Hover it, then click it.

**Expected:** the tip names the file and a click opens it in a tab.

### @LETTER@2. A listed folder

1. Print a path under a folder that no card uses. Hover it: the tip says "outside this card".
2. Add that folder to "folders a terminal link may open outside its card" and save. Hover the path again.

**Expected:** it now opens. A path under a sibling folder, or a `..` or symlink out of the listed one, still says
"outside this card".
