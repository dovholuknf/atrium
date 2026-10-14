# Every transport at once, instead of a ladder

Status: asked for by clint on 2026-10-14, NOT built. A follow-up to `docs/rnd/room-transport-order.md`.

## What is asked

Today a room climbs a ladder: it tries its transports in order, stands on the first that answers, and probes the
ones above it to move back up. Only one is in use at a time, so the board can only say which one that is (the
transport badge, in the warning colour when it is a fallback).

Wanted instead: a room connects over EVERY transport it has, at once, and keeps each up. The room tile shows one
status badge per transport, the way it shows the mTLS badge today, each saying whether that path is up. A broken
path is visible while the room is still fine on another, instead of only when it becomes the one in use.

## Questions to answer before building

- Which connection carries the data pool. All of them (spread the idle pool across paths), or the first in order
  that is up, with the others holding a control connection only.
- How the hub keys a room with several control connections. Today a second attach replaces the first. With every
  path up, the hub holds one room with several links and must not read the second as a reconnect.
- What one badge per transport says when the hub does not listen on that transport, or the room has no identity
  for it. Probably no badge, rather than a red one.
- Whether the probes in the ladder (two good probes, two minutes apart) go away, since every path is then live.
- What it costs: a ziti control connection per room is a held circuit on the OCI router, cheap but not free.

## Not changed by this

The order still matters (which path carries data first), a refusal still stops every path, and the room's name
still comes from its hub-signed certificate on every path.
