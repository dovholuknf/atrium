Docs: none, makes the advertised schema match what the docs already say

## Test plan

## @LETTER@. The stdio launch tools advertise what the room reads

### @LETTER@1. Scratch through stdio control

1. From a session using the stdio `atrium control`, call `atrium_launch_worker` with `scratch: true`, an `item` and a
   `cwd` that is not a git checkout.

**Expected:** the launch is made, not rejected as an unexpected property and not refused for lacking scratch.

### @LETTER@2. A deploy swaps the control exe

1. Run a hub deploy on sg4 with an MCP client holding `C:\Users\claude\.atrium\bin\atrium-control.exe` open.
2. List `.atrium\bin`.

**Expected:** `atrium-control.exe` is the new build, the old one sits beside it as `atrium-control.old-<ts>.exe`, at
most three of those remain, and the deploy did not fail on the held file.

### @LETTER@3. A stale control exe says so

1. Start `atrium control` from a build whose commit differs from the running room's.

**Expected:** one line on stderr says the exe is older than its room and to replace it. A matching build or no room
prints nothing.
