## Test plan

## @LETTER@. The notify job can mint an OIDC token

### @LETTER@1. Notify logs no token warning

1. Push a branch and open the notify job's log of the run.

**Expected:** the ziti-hook step no longer logs `no OIDC token available`.

### @LETTER@2. A down webhook does not fail the run

1. Run CI while the ziti-hook service has no terminator.

**Expected:** the ziti-hook step shows a warning and the notify job ends green.
