## Test plan

## @LETTER@. The integration branch can be fetched through the lookup

### @LETTER@1. Ask for claude/main and main

1. From a remote card, call `atrium_git_url` with a repository the hub mirrors and `branch: claude/main`.
2. Ask again with `branch: main`.
3. Fetch the URL given for each.

**Expected:** both answer `found` as a `hub` source marked `readOnly`, and the fetch works.

### @LETTER@2. Pushes are still refused

1. Push a work branch to `claude/main` on the hub.

**Expected:** the push is refused as before, and an ordinary `claude/*` branch still answers with no `readOnly`.
