Docs: none, no verb, flag or setting changed. The room's liveness deadline is internal.

## Test plan

## @LETTER@. A room notices a dead hub path

### @LETTER@1. Black-holed control connection

1. Attach a room to a throwaway hub through a TCP proxy.
2. Make the proxy stop forwarding without closing either side.

**Expected:** within about 15 seconds the room logs that the hub has been silent too long, and reattaches.

### @LETTER@2. Healthy connection is left alone

1. Leave a room attached for several minutes.

**Expected:** no spurious reattach, since the hub echoes every 5 second beat.
