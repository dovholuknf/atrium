## Test plan

## @LETTER@. The ssh block names the room as typed

### @LETTER@1. A LAN HostName for the same Host

1. In `~/.ssh/config` add `Host sgg` with `HostName sgg.lan`.
2. Run `atrium ssh config add sgg` and read `~/.ssh/atrium_config`: the ProxyCommand ends `ssh-proxy %n`.
3. Run `ssh sgg`. The proxy is asked for room `sgg`, not `sgg.lan`.

### @LETTER@2. An old block is migrated

1. Edit a block in `~/.ssh/atrium_config` so its ProxyCommand ends `%h`.
2. Run `atrium ssh config add` for any room. Every block now ends `%n`.
