Docs: none, hooks already read `ATRIUM_HUB_URL` ahead of the address file

## Test plan

## @LETTER@. A second room's runner reports to that room

### @LETTER@1. Two rooms on one machine

1. With a room on agent port 7777 running, start a second room with its own `--dir` and `--agent 127.0.0.1:7787`.
2. Launch a worker from the second room and let it make tool calls and end a turn.

**Expected:** its activity, permission requests and turn end show on the second room's card. The first room shows no
mirror card for it.
