## Test plan

## @LETTER@. A worker knows its commit is already visible

### @LETTER@1. Launch text in a worktree of the launcher's checkout

1. From a card whose directory is a git checkout, launch a worker with a brief into a worktree of that checkout.
2. Read the first prompt the worker was given.

**Expected:** it says "Your commits are already visible to <launcher handle>", says not to push, and still ends with
the usual atrium_done and atrium_blocked text. A worker launched into an unrelated repository gets no such sentence.

### @LETTER@2. atrium_git_push is a success, not a refusal

1. In that checkout, point the `hub` remote at another room's forwarder on this machine
   (`http://127.0.0.1:<other port>/git/hub/...`).
2. Have the worker commit, then call atrium_git_push for its branch.

**Expected:** the answer is "nothing to push: <launcher handle> sees this commit already", not an error.

### @LETTER@3. No push to origin is suggested

1. Read the launch text of any worker.

**Expected:** nothing in it suggests `git push origin`. Only the hub talks to forges.
