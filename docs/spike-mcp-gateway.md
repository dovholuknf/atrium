# Atrium and mcp-gateway: absorb, supervise, separate, or rooms

**Status: a spike. Nothing here is built and nothing else in this repo was changed to write it.** Written
2026-09-15 to answer one question the operator asked and one he described while asking it.

Read `docs/ai-platform-fit.md` first. It already assessed mcp-gateway as an integration target and parked it
behind a single question. This document is the next layer down: not "should atrium gate the gateway" but "should
atrium BE the gateway, run it, or leave it alone", plus the fourth thing the operator actually described.

---

## Recommendation

**Three, with two, inside four.** Stated plainly:

1. **Leave clint's own mcp-gateway separate. Do not absorb it, and do not supervise it either.** On the account
   the operator works in, atrium supervising the gateway buys a restart button and costs a dependency in the
   wrong direction. Option 3 is not inertia here. It is the right answer for that account.
2. **Supervise it, but only inside a room.** A room with no gateway has no tools, and a room reaching the
   operator's gateway has walked through its own wall. The sidecar is the correct shape there and nowhere else.
3. **Refuse absorption outright.** The reasoning is below, and it does NOT turn on the letter of the credential
   rule, which absorption survives. It turns on three things the rule was protecting.
4. **The fourth option is the one worth designing, and most of it already exists.** A room is a leaf, atrium
   already has leaves, and `internal/daemon/rooms.go` already implements the check-in. What is new is one
   operating system user boundary and one decision about the filesystem that decides whether the whole thing is
   real or decorative.

**The single most load-bearing finding, and it is not about any of the four options:** on Windows, loopback is
not a boundary between local accounts, and there is no primitive in either program that makes it one. A room
user can open `http://127.0.0.1:8088` and spend every credential the gateway holds, and can open
`http://127.0.0.1:7777` and post a permission decision to the operator's own daemon. Nothing in mcp-gateway or
atrium authenticates a local caller, both listen on loopback TCP, and Windows Filtering Platform exempts
loopback so the firewall cannot close it. See "What the boundary is made of" below. That fact decides how much a
Windows room is worth, and it is the reason WSL2 shows up as a recommendation rather than as a footnote.

---

## The state of the evidence

**mcp-gateway's source IS on this machine** and was read: `D:/git/github/openziti/mcp-gateway`. So this is not
reasoning from a documented purpose. Where a claim below comes from source it names the file.

What could NOT be read is the operator's LIVE gateway configuration. `C:/Users/claude/.claude.json:7966-7968`
registers `mcp-gateway` as `{"type":"http","url":"http://127.0.0.1:8088"}`, so it is running as a plain loopback
listener rather than over zrok or Agora, but the config file that names its backends was not found under the
home directory. **Which credentials that gateway actually holds today is therefore unknown**, and everything
below about credential custody is read from `etc/mcp-gateway.yml`, the shipped example, which puts
`GITHUB_TOKEN`, `DATABASE_URL` and `Authorization: Bearer sk-...` into backend `env` and `headers` blocks.

Nothing was executed. No probe was sent to 8088.

## What mcp-gateway is, stated so the argument means something

It is an MCP aggregator. It holds a list of backends, connects to each one over stdio, HTTPS, zrok or an Agora
tunnel, namespaces their tools as `backendID + separator + tool` (`aggregator/namespace.go`), and serves the
union as one MCP server. A stdio backend is a subprocess the gateway spawns with an environment the gateway's
config supplies.

Two properties matter here and only one of them is what its README advertises.

**Advertised: reachability.** "Zero-trust access to MCP tools over OpenZiti... dark services that never listen on
public IPs." The problem it names is exposing MCP servers to reach them remotely.

**Emergent, and the one the operator cares about: custody.** Because the gateway spawns the backend and supplies
its environment, the backend's token lives in the gateway's config and the gateway's process, and the MCP client
never sees it. That is real, and it is a consequence of the architecture rather than a goal stated anywhere in
that repo. Worth knowing, because a property nobody wrote down is a property nobody promised to keep.

**And here is the precision the operator's framing is missing.** The gateway keeps the SECRET away from the
agent. It does not keep the AUTHORITY away from the agent. Every claude session running as clint has
`mcp-gateway` registered at user scope, so every one of them can call `github_create_issue` and spend that token
right now. What was removed is exfiltration of the string, not use of the access. A compromised or merely
careless agent cannot mail the token to anybody, and can still do anything the token can do.

That distinction is the hinge of this whole document. Containment that stops an agent holding a secret is worth
much less than containment that stops it using one, and only the fourth option even attempts the second.

---

## The line, quoted, and whether absorbing crosses it

The operator reached the conclusion mid-sentence and asked for it to be tested hardest. Here is the line.

`CLAUDE.md:376-377`, in the "Out of scope (intentional)" section, immediately after the note that
`overlay_reserve.go` calls the zrok REST API with the token `zrok enable` put on disk:

> The rule that holds is the stronger one below: atrium may hold the NAME of a command that has a credential, and
> never somebody else's credential.

It appears twice more, and both restatements sharpen it. `docs/overlays.md:333`:

> **What did not move, and it is the part the rule was protecting.** Atrium owns no credentials. There is no user
> table, no password and nothing to hash.

And `docs/overlays.md:390-391`, refusing to store an OIDC refresh token:

> A refresh token is a long-lived bearer credential belonging to the person who signed in. Keeping one in
> atrium's database would be atrium holding somebody else's credential, which is the rule this whole feature was
> shaped around.

### The ruling: absorbing survives the letter of the rule and violates what the rule protects

**It survives the letter, and pretending otherwise would be the easy answer.** The rule's operative words are
"somebody ELSE'S credential". A `GITHUB_TOKEN` in the operator's own mcp-gateway config is the operator's own
credential, on the operator's own machine, already readable by every process running as him. It is not a guest's
refresh token and it is not a network administrator's identity. Atrium has already crossed a smaller version of
this line once, deliberately, and written down that it did: `overlay_reserve.go` reads the zrok account token off
disk and spends it against a REST API. By the letter, absorbing mcp-gateway is that same move at larger scale on
the same class of secret.

So the honest finding is that the operator's stated reason does not carry the conclusion. His conclusion is still
correct, for three reasons that are firmer than the one he gave.

**First: the store is not a place secrets can live, because atrium copies it casually.** Absorbed backends mean
backend environments in atrium's SQLite. That database is the thing whose failure halts the daemon, and it is the
thing `atrium preview` opens a COPY of (`docs/multi-tenant-decision.md`, and `docs/preview-design.md`). A file
routinely duplicated so somebody can look at a board must not contain API tokens. Nothing about the copy is
careless today because there is nothing in there worth copying carefully.

**Second: the board can be published, and the reasoning that made that safe assumes the store holds no secrets.**
`docs/overlays.md` serves the board on a public zrok address behind an OIDC sign-in, and justifies a signed
rather than encrypted cookie with "nothing in it is secret: a subject is not a credential". The whole published
board design rests on atrium having nothing to steal. Put tokens in the store and the sentence stops being true,
and every decision downstream of it has to be re-argued: the cookie, the guest allowlist, the lent session, the
file download endpoint. That is not a merge, it is a security review of a feature that shipped.

**Third, and decisive: absorbing puts the credential and the agent in the same process tree under the same
operating system user, which is the exact opposite of the containment the operator is trying to buy.**
`internal/daemon/launch.go:643` starts every runner's environment from `os.Environ()`, the daemon's own. Today
that is safe because the daemon's environment has nothing interesting in it. Absorb the gateway and the daemon's
process is where the tokens are, one `childEnvFrom` mistake from being in every runner's environment, and the
process separation that gives mcp-gateway its custody property today is gone. Deleting the boundary in order to
manage it better is the failure mode.

**Ruling: refuse option 1.** Not because it holds somebody else's credential, but because it moves the operator's
own credentials into a database atrium duplicates, behind a listener atrium publishes, inside the process that
spawns the agents.

### The strongest case FOR absorbing, and why it still loses

It is not nothing, and `docs/ai-platform-fit.md` already called it "the higher prize". Atrium's distinguishing
asset is a synchronous gate that can pause a tool call indefinitely. That gate today only reaches Claude Code,
because it rides Claude Code's `PreToolUse` hook. mcp-gateway is where tool calls actually get dispatched for
every MCP client on the machine. One process holding both would gate every MCP tool call from every runner,
harness-agnostic, with no hook to install.

It loses because **you can have that prize without the merge.** The gateway grows a call to atrium's existing
`POST /permission` beside its existing `backend.policy.Prepare` check, the dependency points one way, neither
imports the other, and atrium learns nothing about MCP. That is exactly what `docs/ai-platform-fit.md` section 2
already specified. Absorption buys the same capability and additionally buys the aggregator, the zrok and Agora
transports, the backend subprocess supervision, and the credential custody problem above. The prize is available
for less.

---

## Option 2 on its own merits: atrium supervises mcp-gateway as a sidecar

**On the operator's own account: do not.** The mechanism already exists, which is what makes the temptation
worth naming. `internal/daemon/fixtures.go` starts terminals with the daemon, and a harness row is a command, a
working directory and an environment. Making mcp-gateway a fixture is configuration, not code, and it would take
about five minutes.

What it buys is a restart button and a card that goes red. What it costs:

- **Lifecycle inversion.** The gateway currently outlives atrium, which means restarting atrium does not drop
  every MCP tool in every session on the machine. Make it a supervised child and it does. `CLAUDE.md` resilience
  guarantee 5 says a kill is not a stop precisely because closing a pty takes the attached process with it. The
  operator already has a standing rule against restarting atrium unasked, and this widens the blast radius of
  every restart from "the sessions atrium supervises" to "every MCP client on the machine".
- **A startup ordering problem that did not exist.** Claude Code sessions that are not atrium's discover the
  gateway at 8088. If atrium owns it, the gateway is up only when atrium is, and a session started during an
  atrium restart silently has no tools.
- **It does not improve custody at all.** Same user, same machine, same loopback listener. The credentials are in
  the same place they were, the agents can reach them exactly as before. This is the version of "atrium manages
  mcp-gateway" that looks like progress and moves nothing.

**Inside a room: yes, and it stops being optional.** See below.

---

## Option 4: rooms

### What the operator described

An atrium that runs as clint, runs mcp-gateway, and runs a number of rooms that are totally independent of the
clint-owned atrium process. That is not any of the three options. It is a topology, and the three options become
a question asked once per room instead of once per machine.

### The word is already taken, and that is convenient rather than a problem

`internal/daemon/rooms.go` is built and shipped. A room there is a remote atrium that dials the operator's
daemon every twenty seconds, reports its cards, and is cached in memory and nowhere else.
`docs/federation-design-v2.md` calls the two sides leaf and forum. `docs/transparent-rooms.md` records the open
decision about how invisible a room should be.

**A room on the same machine under a different operating system user is the same object with a shorter network
path.** Keep the word. Where the distinction matters, say local room. Almost everything already written about
federation applies unchanged, and the parts that do not apply get EASIER rather than harder, which is unusual
enough to be worth checking carefully. It is checked below.

### What a room is, concretely

Six things, and the list is the definition:

1. **A dedicated unprivileged local account.** Not clint, not an administrator, not root. One per room, not one
   per project, because the account is the boundary and boundaries you mint casually stop being boundaries.
2. **Its own home directory**, which is where `internal/claudeconf/hooks.go` writes hooks
   (`os.UserHomeDir()`), so the room's Claude Code configuration is its own and touching it cannot break the
   operator's sessions.
3. **Its own atrium daemon**, its own SQLite store, its own two ports, its own address file. Note that
   `LocationPath()` in `internal/daemon/whereami.go` is per-user by construction (`%LocalAppData%`, or
   `XDG_RUNTIME_DIR`), so two daemons under two accounts already do not collide and already do not see each
   other's location files. This was not built for rooms and it is exactly right for them.
4. **Its own mcp-gateway, or none.** This is where option 2 becomes correct.
5. **Its own credential set**, which is normally empty or deliberately narrow. This is the entire point.
6. **An outbound link to the operator's daemon**, which is the forum. Leaves dial out. The forum holds nothing
   durable.

### What a room can see

**Not the operator's home directory.** Not his ssh agent, not his DPAPI vault or Credential Manager, not his
`~/.claude/settings.json`, not his `~/.zrok2/environment.json`, not atrium's database.

**And here is the decision that decides whether a room is real or decorative: what does it do about the repos?**

A coding agent exists to edit code, and the code lives under `D:/git` and `D:/worktrees` owned by clint. Two
answers, and only one of them is a boundary:

- **Grant the room write access to the operator's working tree** with an ACL or a group. This is the version
  everybody reaches for and it destroys the thing being built. An agent that can write to `D:/git/...` can write
  to `.git/hooks/post-commit`, which runs as clint the next time clint commits. The wall is built and then
  drilled through, and the hole is a shape nobody looks at.
- **The room clones, and work comes back through git.** The room has its own clone in its own home, its own
  remote, and hands results back as a branch, a push, or a patch. Read-only ACLs on a reference checkout are
  fine. Write-back is a pull request or `git format-patch`, never a shared directory.

**Recommendation: through git, and refuse the shared directory version outright.** It is also the thing that
makes a room usable without solving anything else, because git is already how work leaves a worktree here. Note
the second-order cost honestly: a room needs a credential to push, so "no credentials in a room" becomes "one
narrow credential in a room, scoped to one repository, which is not the operator's". That is a real weakening and
it is the right one, because the alternative weakening is filesystem write access to everything.

### How a card in the clint board reaches a runner inside a room

Four channels, and their difficulty is already known from federation:

| What | How it crosses | State |
| --- | --- | --- |
| The card itself | Room checks in every 20s, forum caches in memory, never on disk | **Built.** `rooms.go` |
| A permission request | Forwarded up the link the room holds open, decided on the operator's board | Designed. `federation-design-v2.md` stage 2 |
| A queued prompt, a launch, an action | Same link, same direction | Designed, not built |
| Attach to the terminal | **Redirect** to the room's own board | Built as a concept, trivial locally |

**Attach is where the local room beats the remote room, and it is worth being explicit because it is the one
place this topology is strictly better.** `docs/transparent-rooms.md` treats relayed attach as the hard open
question: a pty cannot leave the machine that made it, so transparency there means the forum proxies a websocket,
which breaks the `docs/overlays.md` rule that nothing is proxied. **On one machine that problem evaporates.** The
room's board is at `http://127.0.0.1:<roomport>` and the operator's browser can simply open it. The redirect is
not a consolation prize locally, it is a correct answer: same browser, same machine, no relay, no proxy, no
keystroke latency through a twenty second heartbeat, and the rule in `overlays.md` stays intact.

So the answer to "how does a card reach a runner in a room" is: **everything except typing crosses the link, and
typing is a link the browser opens itself.**

### What crosses the boundary, enumerated

Enumerate it, because `internal/daemon/overlay_guest.go` already established that the honest shape of a boundary
in this codebase is a separate handler with an allowlist, default deny, and the hazard it documents is an
already-allowed route that grew a parameter.

**Crosses, room to forum:** card id, title, status, waiting-since, runner name, worktree path, the room's name,
a pending permission request with its tool, command and details, a finish and its recap.

**Crosses, forum to room:** a permission decision, a queued message, a launch request, a stop or shelve.

**Does not cross:** pty bytes, the room's database, any file, any credential, any environment variable, the
room's filesystem listing, the room's browse roots. Note that `internal/api/browse.go` and the file endpoints are
per-daemon, so a room's files are reachable only from a room's own board, which is the correct default and should
be written down before somebody adds a convenience.

**The one that needs a decision rather than a default:** a permission request carries `details`, which is the
diff or the file content the tool would write. That is room content arriving on the operator's board, which is
what makes the gate useful and is also the one place room data lands in the forum's process. It must stay in
memory, like the card cache, and must not be written to the forum's store.

### Where mcp-gateway goes in this topology

**Each room gets its own gateway, started and supervised by the room's own atrium, with that room's credentials.**
This is option 2, and inside a room it is correct for the reason it was wrong outside one: the room's daemon and
the room's gateway are the same trust domain and the same lifecycle, and the room has no other way to get one
started.

The alternative, one shared gateway that rooms are allowed to reach, is worth stating so it can be rejected on
the record: **a room reaching the operator's gateway is a contained agent with uncontained authority.** It cannot
read the token and it can spend it. Every argument for building the room in the first place applies against
letting it dial 8088.

Unless the gateway can tell rooms apart. Which is exactly the question `docs/ai-platform-fit.md` parked:

> **Atrium could not name the caller, and naming the caller is the point.**

That was filed as a reason to hold off on gating the gateway. **The room design promotes it from an integration
nicety to the thing the containment depends on.** `gateway/session.go:22-27` still holds
`ClientContext{RemoteAddr, UserAgent, Headers}` and authenticates nobody, and `extractHeaders` whitelists three
headers, none of which is a principal. Verified in source as of this writing, so the parked question is still
parked.

There is a cheap partial answer worth naming: **one gateway instance per room, each on its own port or socket,
each with its own config and its own narrow credentials.** No caller identity is needed, because the listener IS
the identity. It costs one process per room and a config file per room, and it gets the property without waiting
on somebody else's repo.

---

## What the boundary is made of

The answer differs by platform and the difference is not cosmetic. **Linux can build this properly. Windows
cannot, at the layer that matters, without a virtual machine.**

### Linux

**The strong parts, all kernel-enforced and none of them atrium's code:**

- uid and gid, file mode and POSIX ACLs. A room cannot read the operator's home if the operator's home is
  `0700`, and that is worth checking rather than assuming, because many distributions ship `0755`.
- A systemd user instance per room, plus `loginctl enable-linger <room>`, so the room's daemon survives nobody
  being logged in. `docs/packaging.md` already identified this as the thing Linux can buy and Windows cannot:
  "Windows has no `enable-linger`."
- Optional and additive, in ascending strength: systemd sandboxing directives on the room unit
  (`ProtectHome=`, `PrivateTmp=`, `ReadOnlyPaths=`), then bubblewrap or a user namespace, then a container,
  then a virtual machine. `docs/multi-tenant-decision.md` already costed the container step at four packages
  rewritten, because the pty moves inside and `attach.go` has to be written against a stream. A room does not
  need that step. An unprivileged account with linger is enough, and the cheapness is the argument.

**The weak part, and it is the same weak part on both platforms: loopback is not a boundary.** Any local uid can
connect to `127.0.0.1:8088` and to `127.0.0.1:7777`. Neither mcp-gateway nor atrium authenticates a local
caller, and atrium's posture is documented: the hub trusts the agent name off the wire, and
`store.Register` auto-creates a card for a name it has never seen.

**On Linux this is closable, two ways, and one is much better:**

- `nftables` or `iptables` on the `OUTPUT` chain with `-m owner --uid-owner`, refusing room uids a route to the
  operator's ports. It works and it is a rule somebody will flush.
- **A unix domain socket with mode 0600 owned by the operator.** The boundary becomes filesystem permissions,
  which is the same primitive already doing the rest of the work, and it cannot be flushed. **mcp-gateway cannot
  do this today:** `cmd/mcp-gateway/run.go:105` is `net.Listen("tcp", cmd.listen)` and TCP is the only local
  transport `--listen` offers. Atrium is the same. So this is a one-line change in each program and it is the
  single highest-value change named in this document.

### Windows

**The strong parts, also kernel-enforced:**

- A separate local account is a separate SID, profile, registry hive, DPAPI master key and Credential Manager
  vault. NTFS DACLs are a real file boundary. A room user genuinely cannot read the operator's protected
  secrets, and this is the half that works.

**Getting the room's daemon to run at all, which is where `docs/packaging.md` has to be read inverted:**

That document rejected a Windows service for atrium and shipped a logon task, for three reasons, and the middle
one was session 0: "the user profile is not loaded unless the service loads it itself, so DPAPI, the credential
manager, the ssh agent and the per-session PATH are all absent or different. Every claude session it spawned
would inherit that."

**For the operator's own daemon that is fatal. For a room it is the specification.** A room WANTS a process with
none of the operator's ambient credentials. The three objections invert:

- The stored password becomes acceptable, because it is a password the operator mints for an account he created,
  not his own, and there is no Entra or Microsoft-account problem for a local account he provisions.
- Session 0 becomes a feature rather than a defect.
- The service control handler objection stands, so the mechanism is a **scheduled task set to "run whether user
  is logged on or not"**, not a service. That gets a batch logon, which survives logout, which is the property a
  logon task cannot give a room.
- **Do not use "do not store password" (S4U).** An S4U token carries no network credentials, so the room could
  not push a branch, and the git-based handback above is the only way work leaves a room.

**The weak part, and on Windows it is not closable at the network layer.** Windows Filtering Platform exempts
loopback traffic, so Windows Firewall cannot block a local account from reaching `127.0.0.1:7777` or
`127.0.0.1:8088`, and there is no `--uid-owner` equivalent. The Windows primitives that would work are a **named
pipe with a DACL**, which neither program speaks, or not listening on loopback at all.

**So on Windows today, a room contains the filesystem and the credential store, and does not contain the
network.** A room agent that thinks to try it reaches the operator's board and the operator's gateway. That is
not a theoretical gap, it is two URLs.

**Therefore, the Windows recommendation: put the room in WSL2.** It is a real network namespace, so the loopback
hole closes by construction, and it brings every Linux primitive above onto the operator's Windows machine. The
room's leaf dials the forum across the WSL network rather than over loopback, which the existing rooms code
already handles because it was written for a remote machine in the first place. The honest cost is that the
room's repos and toolchain live in the Linux filesystem, and reaching across `\\wsl$` for performance reasons is
exactly the shared-directory mistake refused above.

Named and rejected, briefly, so nobody re-derives them: AppContainer is impractical for an arbitrary developer
toolchain. A Job object limits resources and is not a security boundary. Windows Sandbox is ephemeral and a room
needs to persist. A full VM works and costs a VM.

---

## What it costs

**Option 1, absorb.** The aggregator, four transports, backend subprocess supervision, plus a security review of
the published board and the preview copy, plus a new answer to where secrets live in a SQLite file. Weeks, and
the review is the part that does not compress.

**Option 2 on the operator's account.** Five minutes of configuration and a permanently wider restart blast
radius. Cheap and negative.

**Option 3.** Zero. It is what runs now.

**Option 4, rooms.** The honest breakdown, because most of it is already paid for:

- *Already built:* the leaf and forum protocol, the check-in, the in-memory card cache, per-user address files,
  and a start script that already refuses to launch atrium as the wrong account
  (`scripts/start-atrium.ps1`). Atrium even already has `SharedLocationPath` and the `shared_location` setting,
  written because "the daemon runs as one account and the things that call it do not". That is this problem,
  solved once already for a different reason.
- *Designed, not built:* permission forwarding from a room, which is federation stage 2, and the queued
  message and launch paths. Days, per `docs/multi-tenant-decision.md`.
- *New and small:* a unix socket or named pipe listen option in atrium and in mcp-gateway. One change each.
- *New and operational rather than engineering:* provisioning an account per room, a gateway config per room, a
  narrow push credential per room, and a git handback habit. This is the real cost, it is recurring, and it is
  the thing that decides whether rooms get used or quietly abandoned after two.
- *Per-room Claude Code cost:* `docs/architecture-v2.md:447` notes claude-code "holds the subscription
  credentials". Every room account needs its own Claude Code login. Whether that is permitted and what it costs
  is not an engineering question and is listed below as undetermined.

---

## What would have to be true for me to change my mind

**On refusing absorption:**

- If atrium's store stopped being something that gets copied. If `atrium preview` worked against a live daemon
  rather than a copy of its database, and the board could not be published, the first two objections weaken
  considerably. The third does not.
- If the gateway's credential custody turned out to be nothing, because his live config holds no credentials at
  all and every backend is a local filesystem or tool server. Then absorption is a code-size question rather
  than a security one, and I would still say no, but only on focus grounds, which is a weaker argument that
  loses to a motivated afternoon.

**On the sidecar:**

- If mcp-gateway grew a supervision or health story that atrium is better placed to run, or if the gateway
  started crashing often enough that a restart button had value. Neither is true now.

**On rooms:**

- **If the repos cannot move.** If the work genuinely has to happen in the operator's own worktrees, then the
  git handback is refused, the shared directory is the only option, and rooms should not be built at all,
  because a room with write access to `D:/git` is a boundary that exists on a diagram.
- **If mcp-gateway grows a caller identity.** Then one shared gateway serving all rooms becomes correct, the
  per-room gateway becomes redundant, and the whole topology gets cheaper. This is the same question
  `docs/ai-platform-fit.md` parked and it is still the highest-leverage unknown.
- **If a supervisor appears that does not need a logon session.** `docs/multi-tenant-decision.md` says it plainly:
  a runner in a container with its own credentials, its own home and a proxied terminal, with `attach.go` written
  against a stream. If that exists, a room stops being an operating system account and becomes a container, and
  everything about the Windows half of this document is obsolete.
- **If the Windows loopback hole turns out to be closable** by something short of WSL2. Then Windows rooms become
  as real as Linux rooms and the WSL2 recommendation drops to a preference.

---

## What could not be determined

- **Whether the maintainer of mcp-gateway would accept a caller identity, or a unix socket listener.** Both are
  small changes in that repo and neither is a decision readable from its code. `docs/ai-platform-fit.md` flagged
  this same gap and it has not moved.
- **Which credentials the running gateway actually holds.** Its live config file was not found. Everything here
  about custody is read from `etc/mcp-gateway.yml`, the shipped example.
- **Whether a Windows scheduled task with a stored password loads enough of the room account's profile for
  claude-code and for a ConPTY to work in a non-interactive session.** The profile is loaded by a batch logon,
  and ConPTY should not require an interactive window station, but neither was tested and both are load-bearing
  for the Windows half. Test this before building anything.
- **Whether the Windows Filtering Platform loopback exemption can be overridden.** It is documented Windows
  behavior and it was not tested here, and if it can be overridden the WSL2 recommendation weakens.
- **Whether one Claude Code subscription covers several local accounts on one machine.** Licensing, not
  engineering, and it gates the fourth option entirely.
- **Whether federation stage 2 works at all.** Stage 1 is in `rooms.go` and permission forwarding is design.
  Every claim here about a permission request crossing the link is a claim about a document, not about code.
- **The live behavior of anything.** Nothing was run. No request was made to 8088, 7777 or 7778.
