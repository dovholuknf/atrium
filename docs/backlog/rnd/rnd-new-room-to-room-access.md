# rnd-new-room-to-room-access: a room may reach another room's machine, by a route the operator picks (SPIKE)

Status: SPIKE WRITTEN, docs/rnd/room-to-room-access-spike.md. Owned by @rnd. Filed by the orchestrator 2026-10-01, from clint: "atrium rooms feel
like they should provide ssh capability to other rooms if they want to. consider if m1mini wanted to modify sg3, or
sg4. in those cases you would need those keys, but all this should just be able to be 'direct ssh if you want that'
or 'via zrok private share' or 'via openziti'."

## Why

Code moves between rooms through the hub (`docs/fabric/git-sync-design.md`) and needs no keys. Changing ANOTHER machine
does: a director on m1mini that has to fix sg3's toolchain, or provision a room on sg4. Today only sg4 holds ssh
access to the other machines, so that work always goes back to sg4. That is one reason the directors could not all
move to m1mini on 2026-10-01.

## Routes to cover, each opt-in per room pair

- Direct ssh: a key per room, authorized on the rooms it may reach. Who makes it, who authorizes it, how it is
  revoked.
- zrok private share: the target room shares its sshd privately, and the source room reaches it with
  `zrok access private`. No inbound port, no key exchange outside ssh's own.
- OpenZiti: an ssh service per room, and an identity per room that may dial it. Policy says who reaches whom.

## Answer in the spike

- The line atrium holds: it never holds somebody else's credential (`CLAUDE.md`, the overlay rules in
  `docs/fabric/overlays.md`). Can atrium drive each route by holding only the NAME of a command or identity that the
  machine's own tooling set up, the way `overlay_reserve.go` uses the token `zrok enable` left on disk?
- Default: off. What turning it on looks like in provision and on the board, and how a room says what it can reach.
- Whether this is a room capability the hub knows about (so a launch can refuse a card that needs a route it lacks),
  which feeds `rnd-new-room-handoff`.
- The hook rule that refuses remote git for agents: does ssh to another room fall under the same policy, and what
  clint wants an agent allowed to do there.
- Cost of each route, and which one clint's own rooms use first.

Output: a short spike doc and a recommendation, reviewed by @review. Nothing built.
