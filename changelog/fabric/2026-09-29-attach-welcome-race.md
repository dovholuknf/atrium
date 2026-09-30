- A room reattaches on its first try after a hub restart. The hub wrote the welcome after firing its attach hooks, and
  a hook that wanted a connection to the room (the input-lag push) could write `{"need":1}` on the control connection
  first. The room read that as a refusal with no reason and redialled every 5 seconds until the welcome won the race,
  which kept the live rooms off the hub for 2 and 5.5 minutes after the 17:05 deploy on 2026-09-29. Every control
  write now goes through one lock that the attach holds until the welcome is written.
  `TestTheWelcomeIsTheFirstFrameEvenWhenAnAttachHookWantsAConnection` fails on the old code.
