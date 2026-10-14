## Test plan

## @LETTER@. The reply guard blocks a handed-over deploy command

### @LETTER@1. A deploy script in a code block

1. Register `atrium turn --event end` as a Stop hook and set `ATRIUM_GUARD=on`.
2. End a turn with a reply holding `atrium-deploy.ps1` inside a code fence.

**Expected:** the turn does not end. The model reads the reason and withdraws the command, once.

### @LETTER@2. The same name in prose

1. End a turn with a reply that mentions `atrium-deploy.ps1` outside a code fence.

**Expected:** the turn ends as before.
