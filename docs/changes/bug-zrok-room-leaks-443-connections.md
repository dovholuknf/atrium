Docs: none, a fix that changes no verb, flag, endpoint or setting

## Test plan

## @LETTER@. A redialling zrok room holds a fixed number of sockets

### @LETTER@1. Hub down for ten minutes

1. Attach a room over zrok, then stop the hub.
2. On the room's machine count the room's ESTABLISHED :443 connections each minute for ten minutes
   (`Get-NetTCPConnection -OwningProcess <pid> -State Established` on Windows, `ss -tnp` elsewhere).

**Expected:** the count stays flat while the room redials. The room log says `[ziti] context built (zrok), 1 live` once.
