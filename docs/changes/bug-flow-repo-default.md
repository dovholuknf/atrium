## Test plan

## @LETTER@. The merge flow finds the hub's only repository

### @LETTER@1. One repository, flow_repo unset

1. Clear the `flow_repo` hub setting on a hub that mirrors exactly one repository.
2. Open an item whose branch has landed on `claude/main`.
3. Its landed, deployed and pushed steps are read from that repository and not left pending.

### @LETTER@2. No repository can be chosen

1. Clear `flow_repo` on a hub that mirrors two repositories, or none.
2. Open an item with a build.
3. Its pending steps each say that `flow_repo` is unset and why none was chosen.
