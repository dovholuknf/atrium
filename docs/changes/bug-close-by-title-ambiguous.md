## Test plan

## @LETTER@. A name two cards share is refused by close, exit and cull

### @LETTER@1. Close by an ambiguous name

1. Have two live cards with the same alias.
2. Call `atrium_close` with that alias and `confirm` true.

**Expected:** the call is refused, naming both cards with id, room, status and age. Both cards stay open.

### @LETTER@2. Close by id

1. Call `atrium_close` with the id of one of them.

**Expected:** exactly that card closes. `atrium_exit` and `atrium_cull` refuse the shared alias the same way.
