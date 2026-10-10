## Test plan

## @LETTER@. A worker is told not to end a turn on a running command

### @LETTER@1. Launch text

1. Launch a worker with any prompt and read the prompt it receives.
2. Read the descriptions of atrium_done and atrium_blocked.

**Expected:** the launch prompt ends by telling the worker to run tests in the foreground, to wait in the same turn for
a command it must background, and never to end a turn while a command it started is still running. Both tool
descriptions say the last part too, and the blocked one reads "Nothing else is needed. Give `reason`".
