## Test plan

## @LETTER@. An ssh proxy does not outlive its ssh

### @LETTER@1. The Host block keeps a dead connection short

1. Run `atrium ssh config add <room>` and open `~/.ssh/atrium_config`.

**Expected:** the block has `ServerAliveInterval 30` and `ServerAliveCountMax 3`, also for a block written before.

### @LETTER@2. The proxy ends with ssh

1. Run `ssh <room>.atrium`, then kill that ssh process hard.
2. Wait a little over 30 seconds and list processes.

**Expected:** no `atrium ssh-proxy` for that room remains.
