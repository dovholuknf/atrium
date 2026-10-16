## Test plan

## @LETTER@. The Host line carries .atrium

### @LETTER@1. A block is written as room.atrium
Run `atrium ssh config add sg3`. `~/.ssh/atrium_config` holds `Host sg3.atrium` and `HostKeyAlias sg3.atrium`.

### @LETTER@2. An old block is rewritten
With a block that says `Host sg3`, run `atrium ssh config add sgg`. The sg3 block now says `Host sg3.atrium`.

### @LETTER@3. ssh reaches the room by the new name
`ssh sg3.atrium hostname` answers the room's hostname. `ssh sg3` no longer matches the block.
