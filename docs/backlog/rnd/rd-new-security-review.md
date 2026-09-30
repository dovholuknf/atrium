# rd-new-security-review. Find and close the holes: plain HTTP, unauthenticated callers, and "regular curl"

Status: not started. PRIORITY. Two halves: @review audits what is there now, @rnd designs the fix, @runtime builds.
Filed by the orchestrator 2026-09-30 on clint's word:

> we need a security review of our design done as well to find and fix any stupidity holes we have added... non
> https, unauthorized connections shit like that all needs to be patched.... using openziti solves much of that, as
> does room mTLS design but regular 'curl' should prolly be 'bad'... maybe that should just be part of the atrium
> binary and prevent 'regular curl'??? design this...

## Starting facts (2026-09-30, build cc4ee8f3)

- The hub runs `--link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779`: the room link listens on every interface.
- The board (7778, 7781, 7791) and the agent listeners (7777, 7787) are plain HTTP on loopback with no caller identity.
  Anything on the machine, any user, reaches them, and `curl` can exit a card, launch a runner, or approve a
  permission. This session did exactly that by hand to move cards between rooms.
- "Loopback means trusted" breaks the moment a share terminates on the machine: every request over a zrok or OpenZiti
  share presents as loopback (`internal/api/CLAUDE.md` says so for browse).
- Designs already on file that cover part of it: the room mTLS design, overlays (`docs/overlays.md`), and the board
  login in `internal/daemon` (OIDC/basic, room board only, the hub has none).

## @review: the audit

Every listener, every route, and who can reach it today: bind address, TLS or not, what identifies the caller, what it
can change. Severity-ranked, with file:line evidence and the proof (a curl that works and should not). Include the
hook endpoints, the permission gate, launch, exit, file upload/download, browse, settings, the hub's /_hub routes and
the link listener. Note what already holds (safepath, the guest allowlist, loopback-only PUTs).

## @rnd: the design

- A caller identity on every mutating route. Recommend how: a per-user local credential that only the atrium binary
  reads (so `curl` without it is refused, and `atrium <verb>` is the supported client), mTLS between hub and rooms,
  OpenZiti identity where a share is involved. Weigh a Windows named pipe or unix socket with an ACL for the local
  case against a loopback TCP port with a token.
- HTTPS where anything crosses a machine. The link listener first.
- What the hooks do. They run on every tool call and must never fail a session (CLAUDE.md resilience rule 2), so a
  refused hook fails open. Say how that stays safe.
- The board in a browser: how it authenticates without breaking the phone or a share.
- Web Push needs HTTPS and a service worker. If the design brings HTTPS to the board, say whether Web Push becomes
  possible (the growler's phone reminders, see persistent-growler-design.md).
- Stages for @runtime with sizes, ordered by the audit's severities.
