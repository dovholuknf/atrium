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

## Results

### Goal A code: DONE and committed

- `hub --board-transport zrok --board-share private|public` serves the same board handler on a zrok proxy share.
  Loopback is untouched. See `cmd/atrium2/hubshare.go` and the wiring in `cmd/atrium2/hub.go`.
- A share failure is NON-FATAL: the hub logs it and serves loopback anyway. Proved on a throwaway hub instance
  (spare ports 7900/7901, own temp dir), which came up on loopback while the share create failed. The live hub was
  never touched and stayed healthy throughout (`rooms:1, claude-sg4`).
- Tests green: `go test ./cmd/atrium2/... ./internal/link/...`.

### Goal A activation: BLOCKED by the zrok account (external)

Creating ANY zrok share on this account fails right now with `[POST /share][500] shareInternalServerError`. This is
NOT atrium: the plain `zrok2 share private ... --backend-mode proxy` CLI returns the same 500. TCP to
api-v2.zrok.io:443 is fine and `zrok2 status` works, so the account is enabled and the controller is reachable, but
share allocation fails. The account holds dozens of reserved names left by other work (docusaurus/docpreview), which
is a plausible capacity cause, but the documented limit answers are 401 and 409, not 500, so this may be the hosted
instance. I did NOT delete any reserved names: they belong to other sessions, not this one.

To finish Goal A, once shares can be created again:

    pwsh -File scripts\sgg\restart-hub-with-board-share.ps1 -ShareMode private

That is a detached, hub-only restart (room stays up) that deploys the new binary and brings the share up. If share
create still fails it degrades to loopback, so running it is never worse than the plain hub.

### Goal B: prep DONE, rooms:2 BLOCKED on an overlay link for sgg (credential action)

Done on the orchestrator's go-ahead:

- Binary staged to sgg: `scripts/sgg/stage-sgg.ps1` copied `atrium2.exe` to `sgg:C:\Users\localai\.atrium2\bin` and
  ran `version` there. (Fixed the script to pin Windows OpenSSH: PATH `scp` here is the msys64 build, which cannot
  talk to a Windows remote.)
- Room row minted: `atrium2 hub room add sgg --dir ...\hub` (with db-lock retry). The room shows on the board as
  `sgg`, `never connected`.

Why rooms:2 is not reached: `/_hub/health` counts ATTACHED rooms, so sgg has to actually dial the hub link to count.
The minted token is transport `direct` and encodes a LOOPBACK link address, which sgg cannot reach, so it will not
attach. A room dials the hub, and every reachable-link path needs setup that is clint's to do:

- zrok: `zrok enable` on sgg (credential) AND the hub link over zrok, which is also blocked by the account 500.
- ziti: an enrolled identity on sgg (credential) AND a hub-side `atrium-hub` service with bind/dial policies (admin).

Both are scripted as a runbook in `scripts/sgg/bringup-sgg.md`. Once the overlay exists, the sgg row must be
re-minted over that transport (`hub room rm sgg` then `hub room add sgg --transport <t>`), because a room's transport
is fixed when the row is added and the current row is `direct`.

## Open questions for clint / orchestrator

1. zrok account: share create returns 500 account-wide. Free reserved-name capacity (delete stale docusaurus/
   docpreview reservations) or look at the hosted instance? This blocks both goals over zrok.
2. Goal A board share, once zrok works: private (recommended, safe, needs zrok on clint's end) or public
   (browser-openable, NO login in front of a board that reads files and answers prompts)?
3. Goal B sgg overlay: enroll a ziti identity for sgg (recommended, needs a hub-side `atrium-hub` service + policies),
   or enable zrok on sgg with clint's account token? Either is a credential action clint has to bless.
4. Hub restart: the orchestrator owns hub build/restart by convention (memory), and the messaging MCP is down, so I
   have NOT restarted the live hub. Should the orchestrator run `restart-hub-with-board-share.ps1`, or is it fine for
   this session to do it once question 2 is answered?

## State

- [x] Discovery (overlay + sgg)
- [x] Build atrium2 into build.claude (windows/amd64, no cross-compile)
- [x] Goal A code change (board over zrok, non-fatal), committed
- [x] Goal A code verified on a throwaway hub (share path exercised, degrades cleanly)
- [ ] Goal A share up on the live hub (blocked: zrok account 500 + restart/private-public decision)
- [x] Goal B staging + bring-up scripts, committed
- [x] Goal B binary staged to sgg (ssh/scp, verified `version`)
- [x] Goal B room row `sgg` minted on the hub (shows `never connected`)
- [ ] Goal B room attached / rooms:2 (blocked: no overlay link sgg can dial; direct token is loopback)
