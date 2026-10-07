# Decision 18: what proves a room's name over an overlay (rd-003)

Status: built. The direct transport's mutual TLS runs inside every overlay (`internal/cli/roomjoinflags.go`), and a room
without it is allowed or refused with `atrium rooms legacy`.

Origin: research and a recommendation, 2026-09-30, by @rnd. Nothing is built. Answers the open question in decision
18 of `docs/decisions.md`. Owned by @fabric when it is built.

## The answer, in one paragraph

**Run the direct transport's mutual TLS inside every overlay, and let the overlay decide only who may reach the
hub.** The room's name then comes from the certificate the hub signed, on every transport, exactly as it does over
direct today. No new kind of credential is invented: it is the same one-time secret and the same hub-signed
certificate `internal/link/direct.go` already mints, carried over a ziti or zrok connection instead of a TCP one.
OpenZiti and zrok get the same answer, because neither can prove which room is calling (section 2), and the
question "OpenZiti or zrok" stops being about identity and becomes only about reach (section 5).

## 1. The gap, restated

Over direct, spending the join secret is the proof and the answer in one step (`direct.go`, `ServeEnrolment`), and
every later connection presents a certificate carrying the name the hub signed. Over ziti and zrok,
`ZitiAuthenticated` and `ZrokAuthenticated` return true for every connection (`internal/link/ziti.go`, the last
function), and the name is the `room` field of the hello, which the room writes about itself. So anybody the overlay
lets through can attach as any room on the list.

## 2. What each overlay can and cannot tell the hub

**zrok private: nothing.** A connection arrives through the hub's own private share. The hub learns that somebody
holding the share token reached it, and not who. This is what decision 7 already said.

**OpenZiti: less than it looks.** The SDK hands the hosting side `SourceIdentifier()` on an accepted connection
(`sdk-golang@v1.2.8/ziti/edge/network/conn.go:485`), and it is tempting to read it as the dialling identity. It is
not a proof. It comes from the `CallerIdHeader` on the dial, which the SDK's dial options let the CLIENT set
(`DialOptions.CallerId`, `ziti/edge/conn.go:232`, written into the connect message by `NewConnectMsg`,
`ziti/edge/messages.go:249-261`), and the edge router copies that header from the client's
request into the circuit (`ziti@v1.6.0/router/xgress_edge/listener.go:54-60`, `peerHeaderRequestMappings`) for the
hosting side to read (`router/xgress_edge/dialer.go:94-99`). So it is a label the dialler chose, carried faithfully,
the same kind of claim as the hello's `room` field. What the fabric DOES prove is that the dialler holds an enrolled
identity with a dial policy for the service. It does not say which one to the application.

**The one fabric-level proof OpenZiti has is the service.** If each room dials its own service (`atrium-sg3`,
`atrium-m1mini`) and each service's dial policy admits exactly that room's identity, then the service a connection
arrived on names the room, and the fabric enforces it. It works, and it is set aside in section 4, because it costs
a service and a policy per room on a network atrium does not administer.

## 3. The recommendation: mTLS inside the overlay

The overlay is for REACH: who may open a connection to the hub at all. The certificate is for IDENTITY: which room
this connection is. They were fused over direct only because TCP gives no reach control of its own.

What changes:

- **The hub** wraps whatever listener the transport gives it in the same `tls.NewListener` config `Direct.Listen`
  builds (`direct.go:98-104`: the hub certificate with its CA, `VerifyClientCertIfGiven`, TLS 1.3). So every
  transport's accepted connection is a `*tls.Conn`, `peerCert` reads the name from it, and `Hub.Authenticated` is
  `DirectAuthenticated` for all three. `ZitiAuthenticated` and `ZrokAuthenticated` go away.
- **The room** wraps whatever connection the transport dials with `tls.Client` over `Direct.clientTLS()`. That config
  already ignores the address and checks only the issuer (`direct.go:235-245`, `issuedBy`), so it needs no hostname
  and works unchanged over a ziti service name or a zrok share.
- **Enrolment** is `Direct.Enrol` with its `tls.Dialer` replaced by the transport's dial plus `tls.Client`. The join
  string for an overlay room gains the same one-time secret and CA pin a direct join string already carries. The
  hub mints it with `atrium rooms add`, as it does for direct.
- **The hello's `room` field** becomes what it already is over direct: a label, overridden by the certificate, with a
  mismatch logged (`protocol.go`, the comment on `hello.Room`).

What it costs:

- **One re-join per existing overlay room**, with a new join string, since today's overlay join strings carry no
  secret. A room on the old string is refused with a sentence saying so, rather than attaching unproven.
- **TLS inside an already encrypted overlay.** OpenZiti encrypts end to end and zrok's tunnel is TLS to the edge. A
  second layer costs a handshake per connection and some CPU, which is noise next to a terminal stream. The link's
  data pool reuses connections, so the handshake is paid per pooled connection, not per request.
- **The hub's key material now matters on overlay hubs too.** A hub that was ziti-only never needed a CA. It now
  keeps the same `Keys` directory a direct hub keeps. That is already written and already backed up with the store.

What it keeps:

- **The overlay's own gate stays.** A ziti dial policy or a zrok share token still decides who can reach the hub.
  A leaked join secret is useless to somebody the overlay does not admit, and a stolen room certificate is useless
  without reach. Two independent things have to fail.
- **"Atrium never mints a credential of its own" still holds in the sense it was written.** The rule in
  `docs/fabric/ziti-zrok-flow-design.md` is about overlay credentials: atrium enrolls a JWT somebody issued and
  administers no network. The room certificate is not an overlay credential. It is the credential decision 7
  already gave every direct room, and extending it to the other transports invents nothing.

## 4. The alternatives decision 18 named, and why each is set aside

- **A store-minted secret spent on first attach, remembered afterwards.** That is this recommendation, and "a new
  kind of credential" was the worry. It is not new: remembering it afterwards is exactly the certificate, which
  direct already issues. The worry was about inventing a scheme, and this reuses the one that exists.
- **The OpenZiti identity itself, recorded on first attach and refused if it changes.** Not available as a proof,
  because what the hosting side can read is client-set (section 2). And it has nothing to offer zrok private.
- **A service per room on OpenZiti.** A real fabric-enforced proof, and the cleanest where a network admin is happy
  to create them. Set aside as the default because atrium administers no network, and a room joining would mean a
  controller change. It can sit on top of the recommendation later as a second check (the service a connection came
  in on must match the certificate's name) for operators who want the fabric to say so too.
- **A shared secret sent on every hello.** A bearer token by another name, as decision 18 said. Anything that
  records one hello can replay it. The certificate is the same secret without that flaw.

## 5. Then OpenZiti or zrok, for the room link

With identity taken out of the overlay, the choice is only about reach:

- **OpenZiti** where a network exists. Nothing listens on a port, revoking a room's reach is a policy change, and the
  hub needs no public anything. It needs a controller, a router, an enrolled identity per machine, and a service
  with bind and dial policies, all administered outside atrium. Nothing has run end to end on it yet
  (`docs/fabric/ziti-zrok-flow-design.md`, "What exists today").
- **zrok private** where there is no network to administer. Both ends run `zrok enable` on an account, and the hub
  runs a private share. It is proved end to end for the board, and the room link over it is written. Its reach
  control is coarser: anybody holding the share token can reach the hub, which is exactly why the certificate
  matters more here.
- **Direct mTLS** on a LAN or wherever the hub's port is reachable, unchanged.

Recommendation: keep all three, make the certificate the identity on all three, and prefer OpenZiti for a fleet
that already has a network, zrok private for a machine joining from somewhere with none. zrok public for the room
link stays refused, as it is today.

## 6. Build, for @fabric, in order

1. `internal/link`: a wrapper that takes any `Listener` and any `Dialer` and applies the direct TLS configs to them.
   `Direct` becomes TCP plus that wrapper. `Ziti` and `Zrok` gain it.
2. Enrolment over the overlay: `Enrol` takes the transport's dial. `ServeEnrolment` is unchanged, since it already
   runs on an accepted connection.
3. `internal/cli/transport.go`: `openHub` wires `DirectAuthenticated` and `ServeEnrolment` for every transport, and the
   join string for ziti and zrok carries the secret and the CA pin.
4. The hub's startup warning in `atrium_run.go` ("rooms over %s prove they may connect, not which room they are")
   goes, because it is no longer true, and `certs.go`'s matching comment with it.
5. Tests: over a pipe standing in for an overlay, a room with a certificate for `alpha` that says `beta` in its hello
   attaches as `alpha`. One with no certificate is refused for control and data. Enrolment over the pipe spends the
   secret once and a second spend fails. An old overlay join string is refused with the re-join sentence.
6. Rollout: a hub deploy, then each overlay room re-joins once. Direct rooms are untouched.

Added by @fabric's review (2026-09-30, filed as f-022):

- **Wrap only the ROOM-LINK listener.** The hub also serves the board over zrok and ziti on a separate listener,
  and a browser has no client certificate, so the board listener must never get the wrapper. A test pins it.
- **Every connection kind rides the wrapped dial**: control, data, enrol, upgrade, announce, relay, and f-019's
  `git`. Nothing may open its own path to the hub. `Room.DialGit` goes through the room's transport `Dialer`.
- **Deadlines.** `hearHello` and the TLS handshake both set deadlines on the underlying connection, so the ziti edge
  connection must honour `SetDeadline`. One test over a real edge connection, or a check against the SDK, before
  rollout.
- **Provisioning.** `provision-room.ps1`'s ziti and zrok join paths mint the new join string (secret plus CA pin)
  through `atrium rooms add`, and refuse the old form with the re-join sentence.
- **Cost.** sg3 and m1mini attach direct (provision reports rooms dialling `192.168.1.68:7779`), and no room is known
  to be on an overlay link today, so the re-join cost is probably zero. @fabric confirms from the hub's room list
  before rollout.

Decision 18 then becomes: **the certificate the hub signed proves a room's name on every transport. The overlay
proves only that the caller may reach the hub.**

## What I could not verify

- **Nothing was run.** The ziti findings are read from the SDK and router source in the module cache (sdk-golang
  v1.2.8, which atrium's `go.mod` pins, and ziti v1.6.0). A newer router could set `CallerIdHeader` itself from the
  API session. I found no code doing so in v1.6.0, and even if a later one does, the recommendation does not depend
  on it.
- **Whether any room attaches over ziti or zrok today.** f-005 lists sg3 and m1mini with no transport named, and
  sgg over LAN mTLS. If no room uses an overlay link yet, the re-join cost is zero.
- **zrok v2's tunnel internals.** Only that the hub accepts through its own private share, as decision 7 says.
