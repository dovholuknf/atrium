The deploy script now reaches sgg. It reads each room's ssh user from the hub's room record or `scripts/live/room-ssh.json`
instead of assuming the caller's, so sgg is tried as `localai`. A host that answers and refuses the key is reported as
"ssh refused as <user>" and not "offline", and the ssh connect timeout is 25s, since a cold connect to sgg takes about 10s.
