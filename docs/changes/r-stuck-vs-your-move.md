## Test plan

## @LETTER@. A question for the human is not stuck

### @LETTER@1. A worker waits on you in its terminal

1. Launch a worker that ends its turn on an Open Questions block, such as an interview or a walk waiting for you to start.
2. Wait past ten minutes without answering.

**Expected:** its card in NEEDS YOU shows no stuck mark and the launcher gets no silent-stop notice.

### @LETTER@2. A worker that stops with nothing to say

1. Launch a worker that ends a turn with no question and no atrium_done or atrium_blocked.

**Expected:** the card wears the stuck mark and the launcher is told, as before.

Before and after shots are in `docs/changes/img/r-stuck-vs-your-move/`.
