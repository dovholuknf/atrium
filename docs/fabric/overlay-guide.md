# zrok and OpenZiti with atrium: the guide

Three parts. Part 1 is the shortest true path for each thing you might want, with commands to paste. Part 2 is how it
works and what is still broken. Part 3 is what atrium should do instead of making you read Part 1.

Every command and flag here was checked against the cobra definitions in `internal/cli` on 2026-10-21. Anything not
checked is marked **(unverified)**. The reference pages behind this one are `docs/fabric/overlays.md`,
`docs/fabric/room-over-zrok.md` and `docs/fabric/hub-and-rooms.md`.

Two names worth knowing before you start:

- **zrok v2 installs its CLI as `zrok2`.** Older docs and atrium's own log lines say `zrok`. Use whichever your
  install put on the PATH. This guide writes `zrok2`.
- **The hub's directory is `~/.atrium/hub`** unless you passed `--atrium-dir`. Every `atrium rooms` command below
  takes `--atrium-dir` too, and needs the same one the hub used.

---

# Part 1: the easy way

## 1a. The board over zrok

You want to open the board from another machine, and both machines have zrok.

1. On the hub machine, once: `zrok2 enable <your account token>`, then `zrok2 status` to see it enabled.
2. Start the hub with the board on a private share:

   ```
   atrium run --board-transport zrok
   ```

   `--board-share private` is the default and the right one. `public` is refused until a login is set under the
   gear, `expose the board`, `public share login`.
3. The hub log prints the line you need:

   ```
   [hub] serving the board on a private zrok share: zrok access private <token>
   ```

4. On the other machine (zrok enabled there too): `zrok2 access private <token>`. It prints a local address
   **(unverified: the default bind address is zrok's, not atrium's)**. Open that address in a browser.

**How you know it worked:** the board loads at the address `zrok2 access` printed. The board itself shows nothing about
the share (see `bug-zrok-share-invisible-on-board` in Part 2), so the log line is your only marker on the hub side.

**When it does not:**

- Log says `could not put the board on a zrok share, serving loopback only: this machine has no zrok environment yet`.
  Step 1 was skipped on the hub machine. The hub still serves loopback, it just did not share.
- The board loaded yesterday and not today. **The board's private token is new on every hub start.** The share is
  deleted at stop and created fresh at start. Read the new token off the log and rerun `zrok2 access private`.

## 1b. The board over OpenZiti

You want the board reachable on an OpenZiti network you (or someone) administers.

1. Somebody with admin on the network creates a service (say `atrium-board`), a bind policy that lets the hub's
   identity host it, and a dial policy for whoever should reach it. Atrium never creates any of these.
2. Get the hub machine an enrolled identity. Either enroll it with the ziti CLI yourself (the exact command depends on
   your ziti version, **unverified** here) or paste the JWT into the gear's OpenZiti block, which enrolls it for you.
3. Start the hub:

   ```
   atrium run --atrium-identity /path/to/hub.json --board-transport ziti --board-service atrium-board
   ```

   `--board-service` defaults to `atrium`. The live hub uses `atrium-board` (`scripts/live/live-common.ps1`).
4. On the other machine, a ziti tunneler (Ziti Desktop Edge or similar) with an identity the dial policy admits.
   Open the service's intercept address in a browser.

**How you know it worked:** the gear's transports list has a row `Board over ziti` that reads `bound`. `binding` means
it is still trying, `off` means it is turned off.

**When it does not:**

- Row reads `refused: no ziti identity is set`. Pass `--atrium-identity`, or set the identity on the row itself.
- Row stays `binding` with a permission error. The service does not exist or the hub's identity has no bind policy
  for it. The gear's **what can I host?** lists what this identity may bind, which tells the two apart.

## 1c. A second machine's room joins the hub over zrok private

The room machine and the hub machine share no network. Both have zrok.

On the **hub** machine:

1. `zrok2 enable <your account token>` once.
2. Start the hub listening for rooms on zrok as well as direct:

   ```
   atrium run --transport direct,zrok
   ```

   The log says `reserved a private zrok share` the first time, and the token lands in `~/.atrium/hub/zrok-share`
   (mode 0600). The gear gets a `Room link over zrok` row. (`room-over-zrok.md` tested `--transport zrok` alone,
   which also works but leaves no gear row.)
3. Name the room and print its join string:

   ```
   atrium rooms add laptop --transport zrok
   ```

   One line, good once, for an hour, for the name `laptop` only. Lost it? `atrium rooms token laptop`.

On the **room** machine:

4. `zrok2 enable <your account token>` once. Same account is fine, it gets its own environment.
5. **The step the old doc leaves out:** run `zrok2 access private <share token>` once, with the token from the hub's
   `~/.atrium/hub/zrok-share`. You may stop it with control-c as soon as it prints. Without this the dial fails with
   `service '<token>' not found` (`bug-zrok-room-needs-access-private`).
6. Paste the join line:

   ```
   atrium room join <the string>
   ```

   It enrolls and starts the room. From then on, `atrium room` alone starts it.

**How you know it worked:** the Rooms tile for `laptop` wears the green zrok rocket chip reading `zrok`. Its path chips
show `zrok` solid (`on`). On the room, `atrium room transports` lists `zrok private share`.

**When it does not:**

- `atrium rooms add` says `no hub is running over zrok from ...`. The hub has not bound its share yet, or runs from a
  different `--atrium-dir`. Start the hub (step 2) first, with the same directory.
- The room log repeats `the zrok controller is not answering. not dialling again for Ns`. That line lies about the
  cause (`bug-zrok-room-backoff-wrong-cause`). Look for the real error every sixth line. Nine times in ten it is step
  5 not done, or the hub's share changed and the string is stale: mint a new one with `atrium rooms token laptop`.

## 1d. A second machine's room joins over OpenZiti

1. On the network: a service for the room link (default `atrium-hub`), a bind policy for the hub's identity and a dial
   policy for the room's. Atrium creates none of this.
2. On the hub machine:

   ```
   atrium run --transport direct,ziti --atrium-identity /path/to/hub.json --atrium-service atrium-hub
   atrium rooms add laptop --transport ziti --service atrium-hub
   ```

3. On the room machine, with an enrolled identity `.json` or the raw enrollment JWT:

   ```
   atrium room join <the string> --openziti /path/to/room.json
   ```

   A JWT is enrolled in place. The service rides in the join string.

**How you know it worked:** the Rooms tile wears the OpenZiti disc chip reading `ziti`, and the gear's
`Room link over ziti` row reads `bound`.

**When it does not:**

- Gear row says `refused: no ziti identity is set`. The hub got no `--atrium-identity`.
- The room cannot dial. Usually the room identity has no dial policy for the service. This path is the least
  exercised of the four: treat a failure as possibly atrium's and say so in a backlog item.

## 1e. A second machine's room joins over direct TCP

The room can reach the hub machine on a port (same LAN, a VPN, an ssh tunnel).

1. On the hub machine, bind the room link wide and say what address rooms dial:

   ```
   atrium run --link 0.0.0.0:7779 --link-advertise hub.lan:7779
   atrium rooms add laptop --link 0.0.0.0:7779 --link-advertise hub.lan:7779
   ```

   `rooms add` reads `--link` and `--link-advertise` itself rather than from the running hub, so give both again.
2. On the room machine: `atrium room join <the string>`.

**How you know it worked:** the Rooms tile wears the padlock chip reading `mTLS`, and the path chip `mTLS` is solid.

**When it does not:**

- `rooms add` says `--link is bound wide to 0.0.0.0 but --link-advertise is not set`. Add `--link-advertise`.
- The join hangs or is refused. A firewall on the hub machine blocks 7779, or the advertised name does not resolve
  from the room. `nc -vz hub.lan 7779` from the room tells you which.

---

# Part 2: how it works

## Atrium drives an overlay, it does not become one

Both SDKs hand atrium a `net.Listener` (to serve on) or a `net.Conn` (to dial with). That is the whole interface. The
board share serves the same handler loopback serves, so nothing is proxied and no traffic passes through atrium that
was not already going to it. Atrium never decides who may connect: on zrok that is whoever holds a private token, on
OpenZiti it is the network's policies, and atrium never edits either.

## Atrium never holds anyone's credential

The rule in `AGENTS.md`: atrium may hold the NAME of a command that has a credential, never somebody else's credential.

- **The zrok account token** lives in zrok's own environment file, `~/.zrok2/environment.json`. Atrium reads whether
  the environment is enabled and never shows or stores the token itself. The gear's enable button
  (`EnableZrok`, `internal/daemon/overlay_setup.go`) carries the token you paste straight into that file and keeps no
  copy. The room-link and board-share code just loads that environment.
- **An OpenZiti enrollment JWT** is written to a temporary file (never a process argument, which `ps` would show),
  enrolled into an identity, and deleted. The identity `.json` is the network's, and atrium keeps its path.
- **The share token and the join string are atrium's own**, not someone else's. They are covered next.

## Mutual TLS inside the overlay

The overlay is the road, not the lock. A room that joins over zrok or ziti dials the overlay, then runs TLS inside it,
spends a one-time secret to get a certificate the hub signs, and from then on presents that certificate on every
connection. The name in the certificate is the room's identity. So a leaked zrok share token gets you a TCP pipe to a
hub that will refuse you without a certificate. Rooms from before this (no certificate over an overlay) still attach
until `atrium rooms legacy refuse`.

## Where the share token and the join string live

| Thing | Where | Who can read it |
| --- | --- | --- |
| hub's room-link share token | `~/.atrium/hub/zrok-share`, mode 0600 | the hub user, beside the CA key |
| board share token (private) | nowhere. In memory and in the log line, new each start | whoever reads the hub log |
| join string | printed once by `rooms add` or `rooms token`. The hub keeps a hash | whoever you paste it to |
| room's saved join (share token, pin) | the room's key dir, `~/.atrium/room/` | the room user |
| room's per-machine rung settings | `transports.json` in the room's key dir | the room user |

A join string carries the hub's link address or share token, the CA fingerprint, the room's name and a one-time
secret. Good once, for an hour. `atrium rooms token <name>` mints a fresh one and retires the old.

## Fallback rungs and what the chips mean

A hub can listen on several transports. Each room is told the order at attach, default `ziti,direct,zrok`, and tries
them best first, moving back up when a better one answers again. `atrium rooms transports <list>` sets the hub-wide
order. `atrium room transports order <list>` overrides it on one room machine. A transport the hub does not listen on
is skipped.

On the Rooms tile:

- **The transport chip** says how the room is attached now: padlock `mTLS`, OpenZiti disc `ziti`, rocket `zrok`, or
  `here` for the hub's own machine. **Amber with `fallback`** means the room is on a later rung because its first
  choice did not answer. Working, one repair away from its first choice.
- **The path chips**, one per kind (`direct` or `mTLS`, `ziti`, `zrok`), are the room's own view of each road:
  `on` solid (the control connection is here), `up` outlined (answered a probe), `down` amber with the error in the
  tip, `unlistened` grey (the hub is not listening on it), `none` grey (this room has no way to dial it), `off` (turned
  off on purpose). A trailing `!` means dials to its controller keep failing. Pressing a ziti or zrok chip turns that
  road off or on. The room refuses to turn off its last working road.

## What survives a hub restart, and a deploy

| | hub restart | deploy of the live hub |
| --- | --- | --- |
| room-link zrok share | survives. The hub reuses `zrok-share` while zrok says it exists | survives the same way |
| a joined room | survives. It notices the closed socket and dials again, no new string | survives |
| room-link ziti | survives. Identity and service do not change | survives. Live passes `--transport direct,ziti` |
| board over ziti | survives | survives. Live passes `--board-transport ziti --board-service atrium-board` |
| board over zrok (private) | **new token each start**. Rerun `zrok2 access private` | not used on live |
| gear transport rows | survive. A flag seeds a row, a change in the gear is stored and wins next start | survive |

A stop deletes nothing on the room-link share. `atrium rooms retire-share` is the one thing that ends it, and after it
every zrok room needs `atrium rooms token <name>` and a rejoin.

## Known gaps

Each is an open item on the hub backlog. Read them before you trust zrok for anything that matters.

- **`bug-zrok-room-needs-access-private`** (high). A room cannot dial the hub's share until `zrok2 access private
  <token>` has run once from its environment. `room-over-zrok.md` says the token is the only gate, and it is not.
  Workaround is step 5 of 1c.
- **`bug-zrok-hub-no-rebind-on-share-loss`** (high). Delete or retire the share under a running hub and the hub logs
  the listener stopped and then does nothing. Every zrok room is cut off silently until a hub restart, and then each
  needs a new string.
- **`bug-zrok-hub-exits-when-controller-down`** (high). If the zrok controller cannot be asked at start, `atrium run
  --transport zrok` exits and takes the board with it. The doc and the code comment promise a retry.
- **`bug-zrok-room-backoff-wrong-cause`** (medium). Every room dial failure reads `the zrok controller is not
  answering`, whatever really failed.
- **`bug-zrok-join-errors-vague`** (medium). Used, expired and wrong-secret strings share one error, and a stale share
  token gives a generic `not found`. None of them says to run `atrium rooms token`.
- **`bug-zrok-share-invisible-on-board`** (medium). No board shows the share, its token (even masked), the zrok
  environment or whether the share is alive. With `--transport zrok` alone the gear has no zrok row at all.
- **`bug-second-room-runner-hooks-hit-first-room`** (high). A second room on one machine (a zrok test room beside the
  real one, say) launches a runner whose hooks land on the first room. Its card looks dead while the work happens
  elsewhere, and the first room answers its permission prompts. Whether `--isolated` avoids it is **unverified**.

---

# Part 3: what atrium should do instead

A sketch for a later design, not the design. The goal: nobody reads Part 1. The board walks you through it, and it
looks good doing it.

## What the user sees

1. **One button: "reach this from somewhere else".** On the Rooms view and in the gear. It asks one question with
   three big cards: *open the board from my phone or laptop*, *add another machine's agents*, *lend one session to
   someone*. Each card says in a line what it costs.
2. **Pick a road.** Three tiles: *same network* (direct), *zrok* (rocket), *OpenZiti* (disc). Atrium has already
   looked: tiles it can use right now glow, tiles that need setup say what, in one line. Direct is greyed when the
   hub cannot be reached wide, with why.
3. **zrok not set up? Set it up here.** A panel with a link to get an account token, a paste box, a description field
   prefilled `atrium on <machine>`, and an **enable** button. A short animation as the environment comes up, then a
   tick and the account block (environments, shares, names in use). This is `EnableZrok`, which exists in the room
   today, moved to the front of the flow.
4. **OpenZiti not set up?** Paste the enrollment JWT. Atrium reads its claims first and shows which network it is for
   and when it expires, then enrolls. Then **what can I host?** as a list of clickable services. Nothing to type.
5. **Adding a machine: the join card.** Name the room. Atrium mints the join string and shows it as a big copyable
   block and a QR code, with a ticking one-hour countdown. Under it, the exact line to paste on the other machine,
   including the `zrok2 enable` and (until it is fixed) the `zrok2 access private` step, generated for that machine's
   shell.
6. **The waiting room.** The tile for the new room appears at once, dashed, labelled *waiting for laptop*. Each stage
   lights as it happens: *share reached*, *certificate signed*, *attached*. A failure stops on the stage that failed,
   with the real cause and the one command that fixes it.
7. **Done.** The tile goes solid with its transport chip. One line: *laptop is on zrok. It will reconnect on its own.*

The board share is the same flow ending in *open this on the other machine* plus the `zrok2 access private` line and a
QR code, and a standing chip on the header while the share is up.

## The hard parts

- **The account token is a credential the user pastes, and atrium must not keep it.** The line in `overlays.md` and
  `AGENTS.md` holds: the token passes through the room's process in one request body, goes straight into zrok's own
  environment file, and is never written to atrium's database, logs, events, exports or the board. The board learns
  only *enabled, against which instance*. The paste box is a password field, sent once, cleared on send. Same for a
  ziti JWT, which is already written to a temp file and deleted.
- **The board has no login, so enabling cannot be open to anyone who reaches it.** Anyone on a shared board could
  enable an environment or mint a join string. Join strings are CLI-only today for exactly this reason
  (`hub-and-rooms.md`, "Managing rooms"). The walkthrough must be allowed only on loopback at the hub's (or the
  room's) own machine, refused over any share or overlay, with the refusal saying where to go instead. That loopback
  check must read where a request really arrived from, since an overlay presents as `127.0.0.1` (`overlays.md`, "A
  share makes loopback stop meaning anything").
- **Which machine?** Enabling zrok on the room machine has to happen on the room machine, which has not joined yet,
  so the hub's board cannot reach it. The flow needs a bootstrap: a one-line command for the new machine that runs
  `atrium room join` with the enable step built in, or a first-run page on the new room's own `:7781` board that takes
  the token and the join string together. That is the real design question.
- **The zrok `access private` step** should not exist for the user. Fix `bug-zrok-room-needs-access-private` in the
  dial, and the walkthrough loses a step.
- **Honest progress needs states the hub does not report yet.** The share's liveness and the zrok row are
  `bug-zrok-share-invisible-on-board`. The rebind is `bug-zrok-hub-no-rebind-on-share-loss`. A walkthrough that shows
  a green tick over a dead share is worse than Part 1.
- **OpenZiti still needs someone with admin on the network.** Atrium must not create services or policies. The best
  it can do is show the exact services and policies the admin must create, as text to send them.
