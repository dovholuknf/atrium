## Test plan

## @LETTER@. atrium-control.exe follows the deploy

### @LETTER@1. A deploy swaps the control exe

1. Run a hub deploy on sg4 with an MCP client holding `C:\Users\claude\.atrium\bin\atrium-control.exe` open.
2. List `.atrium\bin`.

**Expected:** `atrium-control.exe` is the new build, the old one sits beside it as `atrium-control.old-<ts>.exe`, at
most three of those remain, and the deploy did not fail on the held file.

### @LETTER@2. A stale control exe says so

1. Start `atrium control` from a build whose commit differs from the running room's.

**Expected:** one line on stderr says the exe is older than its room and to replace it. A matching build or no room
prints nothing.
