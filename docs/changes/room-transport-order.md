# The room link over an ordered list of transports

Design and decisions: `docs/rnd/room-transport-order.md`.

![before: sg3 shows mTLS](img/room-transport-order-before.png)
![after: sg3 shows ziti in the warning colour](img/room-transport-order-after.png)

## A room that looked attached and served nothing

Found on claude-sg4 on 2026-10-14, after the hub machine moved to another network (10.20.40.118). The room joined at
`127.0.0.1:7779`. The hub still advertised `192.168.1.68:7779` (`--link-advertise` in `scripts/live/live-common.ps1`),
and the first cut laid that advertise over the joined address. The control connection had been dialled before the hub
spoke, so it stayed up on 127.0.0.1 and the room looked attached, while every data dial went to 192.168.1.68 and timed
out (`dial tcp 192.168.1.68:7779: i/o timeout` in `room.err`) and the hub's pool for the room stayed at 0.

Fixed three ways:

- **Each direct address is its own rung.** One typed on the machine first, then the joined one, then the hub's
  advertise. The advertise never replaces the join.
- **The ladder stands on the rung, not the kind.** Every data dial uses the exact rung the control connection
  reached. A new list from the hub that leaves that rung out does not move it until the next climb.
- **It is said on both ends.** The room logs a failed data dial (at most once a minute) and its own link state
  carries `data_error`. The hub marks a room whose pool has been empty with no data connection arriving for 30 seconds
  as `"starved": true` in `/_hub/rooms` and `/_hub/inventory`, logs the change both ways, and the rooms tab shows
  "attached, unreachable" in the warning colour instead of "attached".

A later hub transport that cannot listen (a ziti controller that is down) is now left out and logged rather than
stopping the hub, so the live hub stays up on direct when OpenZiti is not answering.

## Putting it on m1mini and sg3

m1mini and sg3 cannot reach the hub (sg4) over direct. They re-attach over OpenZiti, the network on
`https://129.153.47.232:443`, once the steps below are done. Atrium creates nothing in the ziti network.

### 1. Ziti network (admin, once)

Log in with `ziti edge login https://129.153.47.232:443 -u admin` and type the password at the prompt, so it is not in
history. Role attributes rather than names, so another room is one `create identity` and no policy change.

```
ziti edge create service atrium-hub -a atrium-hub-services
ziti edge create service-policy atrium-hub-bind Bind --service-roles '#atrium-hub-services' --identity-roles '#atrium-hubs'
ziti edge create service-policy atrium-hub-dial Dial --service-roles '#atrium-hub-services' --identity-roles '#atrium-rooms'
ziti edge create identity atrium-hub-sg4 -a atrium-hubs -o atrium-hub-sg4.jwt
ziti edge create identity atrium-room-m1mini -a atrium-rooms -o atrium-room-m1mini.jwt
ziti edge create identity atrium-room-sg3 -a atrium-rooms -o atrium-room-sg3.jwt
ziti edge enroll atrium-hub-sg4.jwt -o C:\Users\claude\.ziti\identities\atrium-hub-sg4.json
```

The JWTs and the enrolled identity live in `C:\Users\claude\.ziti\identities\` on sg4 and never in the repo. A JWT is
single use and it expires. When one has expired before its machine enrolled it, mint another:

```
ziti edge delete enrollment <id from: ziti edge list enrollments 'identity.name="atrium-room-sg3"'>
ziti edge create enrollment ott atrium-room-sg3 -o atrium-room-sg3.jwt
```

### 2. Hub (sg4)

The live scripts carry the ziti side. `scripts/live/live-common.ps1` adds `--transport direct,ziti --atrium-identity
C:\Users\claude\.ziti\identities\atrium-hub-sg4.json --atrium-service atrium-hub` to the hub's arguments whenever that
file is there, and `start-atrium-hub.ps1`, `deploy-hub-only.ps1` and `atrium-deploy.ps1` all start the hub through it.

RESTART THE HUB THROUGH THE LIVE SCRIPTS, NEVER `atrium stop`, and from a fresh Windows Terminal window (`wt.exe`) that
no room supervises. The scripts refuse to run under a room. Copy the scripts in first, since the live copies are what
run, then swap the binary and restart the hub alone (claude-sg4 keeps running):

```
pwsh -File D:\git\github\dovholuknf\atrium\scripts\live\install-live-scripts.ps1
pwsh -File C:\Users\claude\.atrium2\scripts\deploy-hub-only.ps1 -NoGate
```

`deploy-hub-only.ps1` installs `D:\git\github\dovholuknf\atrium\build.claude\atrium.exe`, so build the merged branch
there first. `atrium-deploy.ps1` does the hub and claude-sg4 together, with the same arguments. Then, still on sg4:

```
atrium rooms transports direct,ziti
atrium rooms transports
Select-String -Path C:\Users\claude\.atrium2\hub.err -Pattern 'ziti' | Select-Object -Last 5
```

The second prints `direct,ziti`, the order rooms are told. It is read at every attach, so changing it needs no
restart. `hub.err` names the ziti service `atrium-hub` among the room listeners. A line saying `not listening for
rooms over ziti` means the identity or the bind policy is wrong, and the hub carries on over direct.

### 3. Each room (m1mini, sg3)

Neither can reach the hub, so the hub cannot hand them anything: no upgrade offer, no git sync. The binary and the JWT
go across by hand. A JWT is one line of text, so print it on sg4 and paste it into a file on the room:

```
Get-Content C:\Users\claude\.ziti\identities\atrium-room-sg3.jwt
```

Build the binary on the room from `main` once this has landed there (or `scp` it from sg4 where ssh reaches). The key
directory below is the default. Pass `--dir` to every `atrium room` command for a room started with another.

**sg3 (Windows, PowerShell).** A running exe cannot be overwritten on Windows, but it can be renamed, so the new one
goes in with two renames, as `scripts/live/live-common.ps1` does on sg4:

```
cd <atrium checkout>
git pull
go build -o build.claude\atrium.exe .\cmd\atrium
Set-Content $HOME\atrium-room-sg3.jwt '<the pasted jwt>'
atrium stop --url 127.0.0.1:7781
$bin = "$HOME\.atrium\bin"
Copy-Item .\build.claude\atrium.exe "$bin\atrium.next.exe" -Force
Remove-Item "$bin\atrium.old.exe" -ErrorAction SilentlyContinue
Rename-Item "$bin\atrium.exe" atrium.old.exe
Rename-Item "$bin\atrium.next.exe" atrium.exe
atrium version
atrium room transports ziti --identity $HOME\atrium-room-sg3.jwt --service atrium-hub
atrium room transports
atrium room --detach
```

**m1mini (macOS).** `mv` is a rename, so the running binary keeps its image and the next start takes the new one:

```
cd <atrium checkout>
git pull
go build -o build.claude/atrium ./cmd/atrium
printf '%s' '<the pasted jwt>' > ~/atrium-room-m1mini.jwt
atrium stop --url 127.0.0.1:7781
cp build.claude/atrium ~/.atrium/bin/atrium.next && chmod +x ~/.atrium/bin/atrium.next
mv ~/.atrium/bin/atrium.next ~/.atrium/bin/atrium
atrium version
atrium room transports ziti --identity ~/atrium-room-m1mini.jwt --service atrium-hub
atrium room transports
atrium room --detach
```

`atrium stop --url 127.0.0.1:7781` asks the room's own board. Without `--url`, `atrium stop` asks `localhost:7778`,
which on a room machine is nothing, or a hub. If the machine normally starts its room another way (a service, a login
item), start it that way instead of `atrium room --detach`.

`atrium room transports ziti --identity <jwt>` enrolls the JWT into `<room dir>/identities/` and records it, so the JWT
file can be deleted afterwards. An identity `.json` that is already enrolled is used where it is. The settings are in
`<room dir>/transports.json`, by default `~/.atrium/room/transports.json` (`%USERPROFILE%\.atrium\room\transports.json`
on Windows). `atrium room transports` lists direct (the joined address) first, then ziti with the identity path.

### 4. Verify

- Room log: the direct rung fails (`no answer within 10s`), then `[link] attached to hub ziti:atrium-hub as "sg3"`
  (or `"m1mini"`).
- On sg4: `curl http://127.0.0.1:7778/_hub/rooms` shows the room with `"via":"ziti"`, `"fallback":true`, a non-zero
  `"idle"`, and no `"starved"`.
- Board: the room's transport badge reads `ziti` in the warning colour, its state chip says "attached" (not "attached,
  unreachable"), and its settings say "connects over ziti (fallback)".
- When direct is repaired, the room moves back by itself within about four minutes (two good probes, two minutes
  apart) and the badge returns to `mTLS` in the plain colour.

## Test plan

## @LETTER@. Rooms fall back to a later transport, and come back

### @LETTER@1. The hub serves two transports in order

1. Start the hub with `atrium run --transport direct,ziti --atrium-identity <hub.json> --atrium-service atrium-hub`.
2. Run `atrium rooms transports direct,ziti`, then `atrium rooms transports`.

**Expected:** the hub logs a listener for each transport. The second command prints `direct,ziti`.

### @LETTER@2. A room configured for ziti attaches over direct first

1. On a room that reaches the hub directly, run `atrium room transports ziti --identity <room.jwt>`.
2. Run `atrium room transports`, then restart the room.

**Expected:** the ladder lists direct then ziti. The room attaches over direct. Its badge reads `mTLS` in the plain
colour.

### @LETTER@3. Direct does not answer

1. Block the hub's direct link port from that room (a firewall rule), and restart the room.

**Expected:** the room log says direct failed and then `attached to hub ziti:atrium-hub`. `/_hub/rooms` shows
`"via":"ziti","fallback":true`. The badge reads `ziti` in the warning colour with the tip "on ziti because its first
choice did not answer", and the room's settings say "connects over ziti (fallback)".

### @LETTER@4. Direct comes back

1. Remove the firewall rule and wait about four minutes.

**Expected:** the room reattaches over direct by itself. The badge returns to `mTLS` in the plain colour.

### @LETTER@5. A local order wins

1. On the room, run `atrium room transports order ziti,direct` and restart it.
2. Run `atrium room transports order --clear` and restart it again.

**Expected:** after step 1 the room attaches over ziti with no warning colour, since ziti is its first choice. After
step 2 it follows the hub's order again and attaches over direct.

### @LETTER@6. A refusal does not fall back

1. Mark the room for deletion on the hub (`atrium rooms mark <room>`), with both transports reachable.

**Expected:** the room is refused on direct and does not try ziti. It backs off as it always has.

### @LETTER@7. The hub's advertise does not take the room off the address it joined at

1. Start a hub with `--link-advertise` set to an address no room can reach, and a room joined at one it can.
2. Open the room's board through the hub, and run `curl http://127.0.0.1:7778/_hub/rooms`.

**Expected:** the board loads. `/_hub/rooms` shows the room with a non-zero `idle` and no `starved`.
`atrium room transports` lists the joined address first and the advertise after it.

### @LETTER@8. A room whose data dials fail is not shown as attached

1. With a room attached, block its data connections only (stop the room, start it with a join pointed at an address
   that answers once, or reproduce with `TestTheHubFlagsARoomWhoseDataDialsFail`).

**Expected:** within about 30 seconds `/_hub/rooms` shows `"starved":true`, `hub.err` says no data connection has
arrived, the room log says a data connection failed, and the rooms tab shows "attached, unreachable" in the warning
colour. When data connections arrive again the hub logs that and the chip returns to "attached".

### @LETTER@9. A hub whose ziti side cannot listen still starts

1. Start the hub with `--transport direct,ziti` and an identity that cannot bind the service.

**Expected:** `hub.err` says it is not listening for rooms over ziti. The hub is up on direct, and rooms are told
direct only.
