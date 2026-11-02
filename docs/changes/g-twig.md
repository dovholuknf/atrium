Docs: none, website/docs/cli.md has the new row

## Test plan

## @LETTER@. atrium twig carries uncommitted work to a new branch

### @LETTER@1. Twig from a worktree with edits

1. In an atrium worktree, commit something locally, then edit a tracked file and add an untracked file and folder.
2. Run `atrium twig some.tangent`.

**Expected:** a new worktree `some.tangent` at the same HEAD, the edits there unstaged, the untracked file and
folder copied, a card running in it, and the first worktree unchanged.

### @LETTER@2. A taken name

1. Run `atrium twig some.tangent` again, and `atrium twig main`.

**Expected:** both are refused with a sentence naming the branch, and no card starts.
