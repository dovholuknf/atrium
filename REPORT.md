# start-atrium link check

## Change
- scripts/live/live-common.ps1 `Wait-RoomLinked` (before `$StaySeconds`): polls the hub's `/_hub/rooms` (127.0.0.1:7778,
  the connection list the board uses) for up to 30s. Returns '' when linked, else the reason: hub did not answer (not
  the room's fault) or hub is up but does not list the room.
- scripts/live/start-atrium-control.ps1: after the local port answers, prints `sg4-control up, linked to the hub` or
  `sg4-control answers locally but is NOT linked to the hub after 30s: <reason>` and exits 1.
- scripts/live/start-atrium-room.ps1: same for claude-sg4 once the hub is healthy.
- Remote rooms already wait for the hub's list in start-atrium-remote.ps1, unchanged.

## Tested (read-only, nothing started)
    live existing: []   (claude-sg4 linked)
    live missing: [the hub is up but does not list it (it lists: claude-sg4, m1mini, sg3, sg4-control, sgg, sgg-zrok)]
    dead hub: [the hub did not answer at http://127.0.0.1:1/_hub/rooms, so the link could not be checked]

## How clint verifies
    pwsh -NoProfile -Command ". scripts/live/live-common.ps1; $w = Wait-RoomLinked 'sg4-control' 5; if ($w) { \"NOT linked: $w\" } else { 'sg4-control up, linked to the hub' }"
