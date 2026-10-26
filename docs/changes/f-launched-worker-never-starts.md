## Test plan

## @LETTER@. A launched worker that drops its prompt gets it again

### @LETTER@1. Retyped once

1. Launch a worker whose launch prompt the runner does not submit, for example by clearing its input box before the
   prompt goes in.
2. Wait about 30 seconds with nothing typed.

**Expected:** the prompt is typed into the worker under `[atrium] your launch prompt, typed again because it never
reached you:` and the worker takes a turn. The room log says it was typed again. It is never typed a second time.

### @LETTER@2. Not retyped after a turn

1. Launch a worker normally and let it start working.

**Expected:** nothing is typed into it by atrium.
