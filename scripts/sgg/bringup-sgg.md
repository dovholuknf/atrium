# Bringing a second room up on sgg

Runbook for Goal B. It attaches a second room, on the machine clint calls sgg, to this hub, so the board shows two
rooms. sgg comes up empty because it has no claude, codex or other harness yet. That is expected. The point is a
second attached room.

## What is true about sgg

- Windows 10.0.26200, amd64, PowerShell 7. Reached over SSH as `sgg` (user `localai`, key `id_ed25519`).
- No overlay client: no zrok environment, no enrolled ziti identity. This is the blocker.
- Same platform as SG4, so the binary built in `build.claude/` runs there with no cross-compile.

## The blocker, in one line

A room DIALS the hub. Every dial path needs something sgg does not have, and each is a credential or account action
that is clint's to make:

- **direct over the LAN** is off the table. The safety rule for this session is no raw ports to the LAN without an
  overlay.
- **zrok** needs `zrok enable <account token>` run on sgg, and the hub's LINK transport also running over zrok. The
  zrok account is additionally returning 500 on any share create right now, so this path is doubly blocked until that
  clears.
- **ziti** needs an enrolled identity on sgg, plus a hub-side ziti service `atrium-hub` with a bind policy the hub's
  identity satisfies and a dial policy sgg's identity satisfies. All administered on clint's controller.

## Step 0, safe now: stage the binary

    pwsh -File scripts\sgg\stage-sgg.ps1

Copies `build.claude\atrium2.exe` to `sgg:C:\Users\localai\.atrium2\bin\atrium2.exe`. Starts nothing.

## Path A: over ziti (recommended once a service exists)

On clint's controller (clint or an admin):

1. Create service `atrium-hub`, a bind policy for the hub's identity, a dial policy for sgg's identity.
2. Issue an enrollment token for sgg. On sgg: `ziti enroll --jwt sgg.jwt --out C:\Users\localai\.atrium2\sgg.json`.

On SG4, run the hub's LINK over ziti (this is a hub-only restart, room untouched). Adjust
`restart-hub-with-board-share.ps1` or start the hub with `--transport ziti --identity <hub-id> --service atrium-hub`.

On SG4, mint the room token (ziti join strings carry the service, no secret):

    C:\Users\claude\.atrium2\bin\atrium2.exe hub room add sgg --transport ziti --service atrium-hub

On sgg, join once, then it runs from saved creds afterwards:

    C:\Users\localai\.atrium2\bin\atrium2.exe join <token> `
      --identity C:\Users\localai\.atrium2\sgg.json `
      --dir C:\Users\localai\.atrium2\room `
      --db  C:\Users\localai\.atrium2\room\atrium.db `
      --http 127.0.0.1:7781 --agent 127.0.0.1:7777

## Path B: over zrok (once the account can create shares again)

1. On sgg: `zrok enable <clint account token>`. This is clint reusing his account on a second machine.
2. On SG4, restart the hub with the LINK over zrok: `--transport zrok`. The hub reserves the link share and holds it,
   so the room token must come from that RUNNING hub, not from `hub room add` (the CLI refuses a zrok join string on
   purpose, because it does not own the share).
3. Take the join string the running hub prints for `sgg`, and on sgg run `atrium2 join <token> --dir ... --db ...
   --http 127.0.0.1:7781 --agent 127.0.0.1:7777`.

## Verify

On SG4:

    (Invoke-WebRequest http://127.0.0.1:7778/_hub/health -UseBasicParsing).Content

Expect `"rooms":2`. The new room shows on the board as `sgg`, empty, which is correct.

## Optional secondary: a local model on sgg

Only after A and B. Install ollama on sgg and register it as a harness so the sgg room can run a local model. Not
attempted here.
