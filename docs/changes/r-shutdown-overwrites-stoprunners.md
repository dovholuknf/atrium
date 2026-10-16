The first shutdown request claims the `runners` value by compare-and-set (`Daemon.stopAsked`). A later request that
differs gets 409 and the message says which value is in force. Refusing was chosen over accepting and logging because
a stop already winding down host runners cannot honestly be told afterwards to do something else. A later request that
agrees joins as before, and `?now=1` still skips and ends the wrap-up. The in-job refusal runs first and claims nothing.

## Test plan

## @LETTER@. A second stop cannot change what the first does to runners

### @LETTER@1. Plain then --runners

1. With a wrap-up running, `atrium stop`, then `atrium stop --runners`.

**Expected:** the second is refused with 409 saying a plain stop is in force, and host runners stay up.

### @LETTER@2. --runners then plain

1. `atrium stop --runners`, then `atrium stop`.

**Expected:** 409 naming `--runners`, and every runner is ended.
