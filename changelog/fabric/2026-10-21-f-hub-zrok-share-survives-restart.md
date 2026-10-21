The hub keeps its private zrok share across restarts. It reads the token it saved in `zrok-share`, reuses that share
when it is still on its zrok environment, and reserves a new one only when it is gone, so a room already joined over zrok
reattaches after a deploy with no new join string. A clean stop no longer deletes the share: `atrium rooms retire-share`
does, and every zrok room then needs `rooms token`. `--transport` naming zrok seeds a "Room link over zrok" row in the
gear's Transports section, which turns on and off with no restart. The live scripts pass nothing for it: the operator
turns it on in the gear. f-hub-zrok-share-survives-restart. Hub restart.
