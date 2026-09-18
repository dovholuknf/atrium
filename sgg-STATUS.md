# STATUS: remote board access + a second room on sgg

Branch `claude/sgg-room`. Board session for clint (AFK). Report channel to `atrium-87300` is the MCP gap noted in
CLAUDE.local.md, so this file plus commits is the report. The orchestrator polls git.

## Discovery (done, read-only)

Overlay tooling on this machine (SG4):

- zrok present and ENABLED. `~/.zrok2/environment.json` exists, api endpoint `https://api-v2.zrok.io/`, ziti identity
  `96g-guehdr`. This is the path proved end to end in `docs/overlays.md`, so zrok is the chosen overlay.
- ziti present with several enrolled identities (sg4 and others), but no service or bind policy is known to exist for
  a hub, and it is unexercised per the docs. Not chosen.

sgg reachability:

- Reachable over SSH today. `~/.ssh/config` has `Host sgg` -> `sgg.parkplace-via-dhcp`, user `localai`, key
  `id_ed25519`. `known_hosts` already has it.
- sgg is WINDOWS. `OS=Microsoft Windows NT 10.0.26200`, `ARCH=AMD64`, host `SGG`, home `C:\Users\localai`, default
  SSH shell is PowerShell 7 (`c:\program files\powershell\7`).
- sgg has NO ollama, NO zrok, NO ziti identity, and no `~/.atrium2`. It is a bare Windows box.

Running hub/room on SG4 (do not disturb the room):

- hub pid 35380: `hub --addr 127.0.0.1:7778 --link 127.0.0.1:7779 --dir ...\hub`. Board build `69a8032`.
  `/_hub/health` -> `rooms:1, only claude-sg4`. Board and link both bind loopback (transport direct).
- room pid 35624: `claude-sg4`, db `C:\Users\claude\.atrium\atrium.db`, http 7781, agent 7777. NEVER restart.

Build: sgg is windows/amd64, same as SG4, so NO cross-compile. `go build ./cmd/atrium2` succeeds into `build.claude/`.

## Plan

### Goal A: board reachable off-box over zrok (code change + safe hub restart)

The hub serves its board on a plain TCP listener (`hub.go`, `net.Listen("tcp", board)`). It has no overlay path for
the board today. The overlay wiring in `internal/daemon/overlay_*.go` is the v1 daemon, not the hub. So Goal A is a
small code change on this branch that mirrors the proved v1 pattern:

1. Add `--board-transport` (none default, or `zrok`) and `--board-share` (`private` default, `public` opt-in) to
   `hub`.
2. When `zrok`, create a zrok share in ProxyBackendMode (the board is HTTP), take the SDK `net.Listener`, and
   `http.Serve` the SAME board handler on it. Release the share on shutdown. This is exactly `startZrokNative` in
   `overlay_native.go`, minus the daemon plumbing.
3. Restart the HUB ONLY, via a detached script, to pick up the new binary and the share. The room stays up.
4. Verify the board answers over the share and record the access line.

DEFAULT IS PRIVATE. A private share needs zrok enabled on clint's end and he runs `zrok access private <token>`.
A public share is a URL with NO login in front of a board that reads files and answers permission prompts. The hub
has no OIDC guard, so public is unauthenticated. Recommend private. Public only on clint's explicit say-so.

### Goal B: second room on sgg (BLOCKED at the last step, prep is safe)

The room dials the hub. Every dial transport needs something sgg does not have yet:

- direct over LAN: forbidden by the SAFETY rule (no raw ports to the LAN without an overlay).
- ziti: needs an enrolled identity on sgg plus a hub-side service and bind policy. Credential + admin action.
- zrok: needs `zrok enable <account token>` run on sgg. Account action.

So the reachability half is blocked on a credential/account action for sgg, which the brief says to stop and report
rather than do unilaterally. What is safe to pre-position now, committed as idempotent scripts:

1. Build atrium2 (done) and stage it to sgg over the existing SSH path.
2. Scripts to mint the room token on SG4 and run `atrium2 join` on sgg once the overlay client exists.

The token's hub dial address must be one sgg can reach (the overlay), NOT 127.0.0.1, so the token is minted AFTER the
transport is chosen.

## Open questions for clint / orchestrator

1. Goal A board share: private (recommended, safe, needs zrok on clint's end) or public (browser-openable, no login)?
2. Goal B sgg overlay: enable zrok on sgg with clint's account token, or enroll a ziti identity? Either is a
   credential action clint has to bless. Which does he want?

## State

- [x] Discovery
- [x] Build atrium2 into build.claude
- [ ] Goal A code change
- [ ] Goal A private share up and verified
- [ ] Goal B staging scripts
- [ ] Goal B room attached (blocked: sgg overlay client)
