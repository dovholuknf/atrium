Docs: docs/fabric/room-over-zrok.md says the room creates its own access

## Test plan

## @LETTER@. A fresh zrok join string works with no access made by hand

### @LETTER@1. Join

1. On a hub over zrok, `atrium rooms add zr1 --transport zrok`.
2. On a room machine that has never run `zrok access private` for that share, `atrium room join <the string>`.

**Expected:** the join and the attach work within a minute. `zrok overview` lists one access for the room's
environment.
