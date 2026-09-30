The launch cap is per room. A launch counts only the live workers already on the room it goes to, against that
room's own cap, so a full sg3 no longer refuses a launch onto sg4. The caps are set on the hub's machine with
`PUT /_hub/launch-caps {"default": 5, "rooms": {"claude-sg4": 10, "sg3": 5}}` (403 from anywhere else) and read with
a GET. A room not listed gets `default`, and with none set, `ATRIUM_LAUNCH_CAP` or 10 as before. (room-launch-cap)
