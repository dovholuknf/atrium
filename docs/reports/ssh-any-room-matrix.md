# ssh to any room through the hub: the matrix

Run 2026-10-14 from sg3 (Windows, OpenSSH_for_Windows_9.5p2, atrium installed build 41ba238a). Rooms: claude-sg4,
sg3, m1mini, sgg. Remote access was on from `*` on all four (sg4 port 22, sgg port 22 user localai, sg3 port 2222,
m1mini port 22).

The probe, per pair, with no login (BatchMode, no key offered, the host key not recorded):

```
ssh -n -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=NUL \
    -o "ProxyCommand=atrium ssh-proxy <room>" -v <user>@<room> hostname
```

"Tunnel" says the hub carried the stream to a sshd that answered with its banner and began key exchange. "Login" is
sshd's own answer.

| from | to | command | tunnel | login |
| --- | --- | --- | --- | --- |
| sg3 | claude-sg4 | `ssh ... -o "ProxyCommand=atrium ssh-proxy claude-sg4" claude@claude-sg4 hostname` | ok, `SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.19` | `Permission denied (publickey,password)`: sg3 holds no private key and sg4 has no key of sg3's |
| sg3 | sgg | same, `ssh-proxy sgg`, `localai@sgg` | ok | `Permission denied (publickey,password,keyboard-interactive)`, same cause |
| sg3 | m1mini | same, `ssh-proxy m1mini`, `claude@m1mini` | ok | `Permission denied (publickey,password,keyboard-interactive)`, same cause |
| sg3 | sg3 | same, `ssh-proxy sg3`, `claude@sg3` | ok | `Permission denied (publickey,password,keyboard-interactive)`, same cause |
| claude-sg4 | sg3, sgg, m1mini | not run: no way into sg4 from this worktree | untested | untested |
| sgg | sg4, sg3, m1mini | not run, same | untested | untested |
| m1mini | sg4, sg3, sgg | not run, same (m1mini has never been tested from either side beyond the row above) | untested | untested |

One finding to note: running `ssh` with `Select-Object -Last` or any pipe that waits for end of output hangs for as
long as the stream is open. That is the harness, not the proxy. `atrium ssh-proxy claude-sg4 < nul` alone prints sg4's
banner and exits.

`atrium ssh setup --check` on sg3 (the installed build) says why no login works: no sshd service registered (step 2,
needs a person), no key at `~/.ssh/atrium_ed25519.pub`, nothing published to the hub, no atrium block in
authorized_keys, and `~/.ssh/config` does not include `atrium_config`.

## ITEMS TO FIX

1. No room has a key the others trust, so every pair stops at `Permission denied`: the wizard steps 5 to 7 (make a key,
   publish it, install the hub's set) are check-only, and the fix half is unbuilt. `internal/sshwizard/steps.go`,
   `internal/cli/sshsetup*.go` (f-ssh-room-login).
2. The login user per room is not a setting: sgg is `localai`, the rest `claude`, and nothing carries it to the
   others. `atrium ssh config add` cannot write `User` until the hub relays it. `internal/sshcfg/sshcfg.go`,
   `internal/link` (f-ssh-room-login).
3. The hub does not hold or relay room public keys, and removing a room does not remove its key from the others.
   `internal/link`, `internal/hubstore` (f-ssh-room-login).
4. Windows admin accounts read `administrators_authorized_keys`, not `~/.ssh/authorized_keys`. The wizard already finds
   that file (`AuthorizedFile` in `internal/sshwizard/steps.go`) but nothing writes it yet.
5. sg3 has no sshd service registered by the wizard's reading, yet answers on 2222. Which sshd that is was not
   confirmed, and the backlog item notes sg3's host key under the room name differs from the one at 192.168.1.147.
   Confirm with `atrium room remote-access` on sg3 once item 7 is installed there.
6. The installed atrium on sg3 (41ba238a) predates the `--addr` flag and the sshd banner in `remote-access`, so
   f-ssh-wslrelay-loopback cannot be shown fixed there until it is upgraded. `internal/cli/remoteaccess_cmd.go`.
7. sg4, sgg and m1mini rows were not run: they need a session on those rooms, with each pair's key in place (item 1).
8. FIXED on this branch: sshwizard tests failed anywhere but Windows, because `filepath.Join` built `C:\ProgramData`
   paths with the host OS's separator. `internal/sshwizard/whichsshd.go`, `steps.go` (`joinFor`).
9. FIXED on this branch: `atrium room remote-access on` took loopback blindly, so on a Windows machine with WSL it
   opened onto wslrelay. It now runs the which-sshd probe, says so when 127.0.0.1 is not this machine's sshd, and picks
   the address that is. `--addr` still overrides. `internal/cli/remoteaccess_cmd.go` (f-ssh-wslrelay-loopback).
