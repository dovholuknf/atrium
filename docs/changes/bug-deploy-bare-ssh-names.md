`scripts/live/atrium-deploy.ps1` used to ssh to the bare room name only, so a room whose bare name timed out or did not
resolve was reported offline even when `ssh <room>.atrium` (which rides the hub) worked. `Resolve-SshRoute` in
`live-common.ps1` now probes the bare name, then `<room>.atrium`, and the deploy says which route answered. That route
serves the probe, the scp, the swap, the detach retry and the log tail, so routes are never mixed. Offline means neither
route worked. A hang on either route with no answer is still reported as a hang. `bug-room-stale-hub-address` is not
fixed here.

The bare probe is one ssh with `ConnectTimeout=5` and a 10s cap, never retried, so a dead bare name costs seconds before
.atrium is tried. Over .atrium the ssh session rides the room being stopped and dies mid-swap, so the swap and its retry
now run detached on the room machine (nohup on mac, a Win32_Process create on Windows so it leaves the sshd job), writing
to `~/.atrium/deploy-swap.out`. The script waits on the hub (`Test-RoomBackOnHub`) and reads that file over ssh whenever
ssh answers. Not run against a real Windows room here: the Windows launch command is only parse-checked.

`test-ssh-route.ps1` tests `Resolve-SshRoute` in isolation with a fake probe, because the deploy script itself is not
run without real rooms. It uses no pwsh-7-only syntax and was run under Windows PowerShell 5.1.

## Test plan

## @LETTER@. Deploy picks the ssh route that works

### @LETTER@1. Route choice

1. Run `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/live/test-ssh-route.ps1`.

**Expected:** every check prints PASS, including the swap ssh dropping while the room still comes back, and the script ends with `all passed`.

### @LETTER@2. A room reachable only through the hub

1. Run `atrium-deploy.ps1 -WhatIf -Rooms <room>` for a room whose bare name does not resolve but `<room>.atrium` works.

**Expected:** the log says `<room>: ssh route <room>.atrium` and the room is not reported offline.
