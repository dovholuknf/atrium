# The OpenZiti and zrok flow for atrium

Designed with clint on 2026-09-19 by interview. This is the design, not the build. Where it changes a rule written
in `docs/overlays.md`, it says so and the change is deliberate.

Read `docs/overlays.md` (atrium drives an overlay, it does not become one) and `docs/hub-room-requirements.md` (the
hub is a multi-tenant view of rooms, and the room link is a separate surface from the board) first. This document
sits on top of both and resolves the questions they left open about which overlay is used where.

## The one distinction everything else hangs on

There are two places an overlay appears in atrium, they are not the same channel, and they do not get the same list
of transports. Conflating them is the mistake this document exists to prevent.

- **Board exposure.** Reaching the human board, and the terminals on it, from off the machine. A laptop or a phone
  reaching the hub. This is the thing clint needs most: check in from his phone and drive any agent.
- **The room link.** A machine dialling the hub to join as a room, so its agents surface on the one board. The
  room dials out, always, for the reasons in `internal/link/link.go`: the room is the stable half and the hub is
  the disposable half.

Both are first class. Neither waits for the other. But the sensitive one is the room link, because whatever can
reach it can drive the hub, and that is why the two channels carry different transport sets.

| | board exposure | room link |
| --- | --- | --- |
| openziti | yes | yes |
| zrok private | yes | yes |
| zrok public | yes, OIDC login REQUIRED | no, never |
| mTLS (direct) | no | yes, and it is the must-have |
| loopback only | always available, the default | not applicable |

The two asymmetries are the whole security argument:

- **zrok public is allowed for the board and refused for the room link.** A public zrok share is a URL anyone can
  open. In front of the board that is acceptable only because a login sits behind it (see below). In front of the
  room link there is no login and reachability is the authorisation, so a public URL would let any stranger dial
  the hub as a room. `internal/link/zrok.go` already refuses it and that refusal stays.
- **mTLS is the must-have for the room link and is not offered for the board.** A room proves itself with a
  hub-signed certificate over the direct transport (`internal/link/direct.go`), which needs no overlay at all and
  is the LAN and no-network path. A browser on a phone cannot present a client certificate, so mTLS is useless for
  board exposure. The board is never bound wide: it stays on loopback and is reached only over an overlay.

## What exists today, so the design builds on facts

Over **zrok**: board exposure is proved end to end (private and public shares, reserved names for durable
addresses, account-limit reporting). The room link over zrok private is written in `internal/link/zrok.go`, TCP
tunnel not HTTP proxy, private shares only. Both machines must have run `zrok enable` first.

Over **OpenZiti**: nothing is exercised end to end, on either channel, because no atrium machine has ever held an
enrolled identity. Everything up to the bind is built: identity states, token inspection, the "what can I host?"
capability query, and a pre-flight that refuses a start naming the next step. The room link over ziti is written
in `internal/link/ziti.go` (hub binds the service, room dials it, no certificate machinery because the fabric
answers "who is this"). `--board-transport` today supports zrok only.

The gaps this design closes. Ziti board exposure does not exist and must. Ziti has never run because no identity
has existed, so the join flow must be able to produce one. And there has been no single documented end-state story
tying "reach my board from my phone" and "join a remote machine as a room" to a chosen overlay. This document is
that story.

## The credential posture, corrected

`docs/overlays.md` states "atrium never handles an identity". That rule is retired for the overlay case. An atrium
that joins an OpenZiti overlay must hold and use an identity to authenticate and be authorised. There is no way to
be on a ziti network without one, and pretending otherwise forced everything through an external tunneler that owns
the key file. Atrium may now open a ziti identity in process to bind or dial a service.

What does NOT change, and is the part the old rule was protecting:

- **Atrium administers no network.** It creates no ziti services, writes no policies, and edits nothing on a
  controller. The overlay is external and out of scope. Atrium is a consumer of a network somebody else runs. The
  read-only "what can I host?" query stays the only thing that talks to a controller.
- **Atrium still never mints a credential of its own.** It enrolls a token somebody else issued, or opens an
  identity file somebody else produced. Reachability over the overlay, or a hub-signed certificate over direct, is
  the authorisation. Nothing in between.

The overlay itself, the controller, the edge routers, the zrok account, are all assumed to already exist. Atrium
expects to be handed a JWT or an identity file, and no more.

**The service and its policies must already exist too, and atrium checks rather than assumes.** Because atrium
administers no network, an external admin must have created the named ziti service and authorised this identity to
bind it (board exposure) or dial it (the room link) before either flow can work. Atrium does not create any of
that. It uses the existing read-only "what can I host?" capability query and the start pre-flight to refuse, with a
sentence naming the next step, a bind or dial of a service this identity is not authorised for, rather than failing
opaquely at the first bind. The default service name is `atrium`, but the name is only a label: the authorisation
is the admin's, checked here, never minted here.

## Board exposure: the "expose the board" panel

The hub settings already has an "expose the board" section and the flow is poor. This replaces it. One panel, one
section per transport, each disabled by default with an enable toggle, styled succinct so the common case is one
click.

**Shared across every transport: the board login.** The board has no accounts of its own. Identity is delegated to
an OIDC provider and atrium verifies what the provider signed, exactly as `docs/overlays.md` describes under "A
login, and only in front of the published board". One login configuration covers every exposure transport. The
board itself never leaves loopback. Every remote path terminates on this machine and hands the request to the same
loopback board, so when a login is configured it guards all of them at once.

**Where the login is REQUIRED, and where it is only optional.** OIDC is mandatory for exactly one transport, zrok
public, because a public URL is reachable by anyone and reachability cannot be the authorisation. For zrok private
and openziti the overlay is the gate: only a party the overlay already admits can reach the board at all, so a
login is optional there and its absence does not refuse the share. This is the one rule that resolves the two
readings of the paragraph above. The single login, once configured, still guards every enabled transport, and an
operator who wants a login in front of the private and ziti paths gets it from the same configuration. The
difference is only in what is refused at save time.

**The external origin and the OIDC callback.** An OIDC provider validates against exact redirect URIs, so every
enabled exposure needs a known external origin before login can work off the machine. Atrium derives that origin
from the transport: the reserved `atrium-` name gives the zrok public URL, and the bound service address gives the
openziti origin. Atrium displays the exact redirect URI to register with the provider, and a zrok public share
cannot be saved until its reserved URL, and therefore its callback URI, is known. This is why the reserved name is
taken before the share starts rather than discovered from the first share.

The three transports and what each asks for:

- **zrok private.** Enable zrok once (paste the account token, atrium runs `zrok enable`). Starting a private share
  shows the command the other end runs, `zrok access private <token>`. Needs zrok on the far side, so it is the
  path for another machine you control, not a bare phone.
- **zrok public.** The same single "enable zrok" covers both public and private, so they are two entries under one
  enablement. A public share is a URL anyone can open, so **OIDC login is required before a public share can be
  started.** With no login configured the public toggle is refused at save time, not discovered at share time. This
  is the phone path: open the URL, sign in, drive the board.
- **openziti.** Paste a JWT, give a path to a JWT, or give an already-enrolled identity `.json` file. A JWT is
  enrolled in place; a `.json` is used directly, the same two inputs the room-link join takes. Then one field, "what
  service should atrium bind", defaulting to `atrium`. Atrium binds the hub board to that service. The phone runs the
  OpenZiti tunneler for iOS or Android, already enrolled on the network (out of scope for atrium), and reaches the
  service.

Rules that fall out and must be enforced:

- **The board is loopback only, always.** `--addr` refusing a non-loopback host stays. Off-machine reach is
  overlay only. There is no wide board bind and no mTLS board bind.
- **zrok public without OIDC is refused at configuration time.** Empty login plus public share is the whole
  internet with an extra step, and it is rejected before it can be saved.
- **Several transports may be enabled at once.** ziti and both zrok modes can run concurrently, each toggled
  independently. The board answers on all of them.
- **An open board share widens "loopback" for the shutdown endpoint.** Any exposure means `atrium stop` can no
  longer trust a loopback source address, because a request from another continent presents as `127.0.0.1`. The
  daemon must count board exposure as sharing and require `--shutdown-token`, exactly as `docs/overlays.md` and
  `docs/federation-design-v2.md` already require for a share and a forum link.
- **Exposure survives a restart.** A zrok public share reserves an `atrium-` name so its URL is stable across the
  hub restarts that UI changes require. A ziti service is re-bound on start. The address handed out does not rot.

## The room link: `atrium2 join` with a transport

A cold machine installs atrium, then joins. Join takes the atrium join token, which names the room and pins the
hub, plus one transport flag and the material that transport needs:

- `--zrok-private` with the private share access token.
- `--mtls` with a plain hub URL. The direct transport, mutual TLS over TCP, no overlay. The must-have, and the LAN
  and no-network answer.
- `--openziti` with a JWT or an identity `.json` file, and the service to dial, defaulting to `atrium`. If given a
  JWT, atrium enrolls it in place and keeps the resulting identity on the room's own disk. If given a `.json`, it
  uses it directly.

No `--zrok-public`. The room link is never fronted by a public URL.

This extends today's join, where the transport is fixed by what the token encodes. The token still carries the
room name and the hub's trust anchor. The flag and its material say how to reach the hub and how to prove the
right to. For ziti and zrok private, the fabric or the private-share token is the gate and there is no separate
enrolment step on the hub. For mTLS, the join token's one-time secret is spent for a hub-signed certificate, which
is what every later connection presents (`internal/link/direct.go`).

## The two end-state flows

### (a) Publishing the hub board to a phone

The hub is running. In hub settings, "expose the board":

1. Pick the transport. For a bare phone with no client, zrok public. For a phone on the ziti network, openziti.
2. For zrok public: enable zrok if not already (paste account token), confirm OIDC is configured (required),
   toggle public on. Atrium reserves an `atrium-` name and starts the share and shows the URL with a copy button.
3. For openziti: paste or point at a JWT, atrium enrolls it, set the service name or take the default `atrium`,
   toggle on. Atrium binds the board to the service.
4. On the phone: for zrok public, open the URL and sign in. For openziti, open the service address through the
   OpenZiti tunneler. The full board and every terminal are there.

### (b) Joining a remote machine as a room

The machine is cold. The overlay it will use already exists.

1. Install atrium.
2. On the hub, add the room and get its join token (`atrium2 hub room add <name>` mints it against the room name).
3. On the machine, run `atrium2 join <token>` with the transport:
   - `--mtls <hub-url>` for the direct LAN or no-overlay path.
   - `--zrok-private <access-token>` for a zrok private link.
   - `--openziti <jwt-or-json> [--service <name>]` for the ziti path. A JWT is enrolled in place.
4. The room dials the hub, and its agents surface on the one board, transparently, the way rooms already do.

## Sharing below the hub: spike first

clint wants to be able to reach out from three scopes: the hub (done, and the focus), a single room, and a single
agent or session. The card would carry a right-click "access remotely" with the same transport menu and a small
config popup, for example a service name to bind.

Whether a one-off agent share earns its place is unknown. It may never be used, or it may be the useful shape for
handing one session to somebody. So this is not designed here. **Spike it, judge whether it is useful, then design
it only if it is, and implement it later regardless of when.** The hub flow is the thing to build first, and the
session and room scopes reuse the same transport model when and if they are taken.

## Out of scope

- **Standing up the overlay.** The OpenZiti controller and routers, the zrok account and instance, and any mobile
  ziti enrolment are external and assumed to exist.
- **The NetFoundry front door.** A future entry in the same "access remotely" menu, designed when it is reached,
  not now.
- **mTLS for the board, and any wide board bind.** The board is loopback only. It is not a transport choice.
- **zrok public for the room link.** Never.

## Follow-ups worth a proof of concept

- **Prove ziti end to end on both channels.** Nothing has ever run against a fabric because no identity existed.
  The first enrolled identity is the moment `internal/link/ziti.go` and a ziti board bind stop being unexercised.
- **`atrium2 join` enrolling a JWT in place.** The ziti room-link transport today assumes an already-enrolled
  identity path. Enrolling a JWT during join is the new capability, and it is the same shell-out or SDK enrolment
  the board path uses.
- **`--board-transport ziti`.** Board exposure over a ziti service, the headless equivalent of the panel toggle,
  alongside the existing zrok support.
- **Measure the phone experience.** Attach over an overlay is the same handler on a dialled connection, but the
  round trip on a real phone network is a number, not a prediction. Measure it before calling remote terminals
  typeable.
