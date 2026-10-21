Docs: none, makes the advertised schema match what the docs already say

## Test plan

## @LETTER@. The stdio launch tools advertise what the room reads

### @LETTER@1. Scratch through stdio control

1. From a session using the stdio `atrium control`, call `atrium_launch_worker` with `scratch: true`, an `item` and a
   `cwd` that is not a git checkout.

**Expected:** the launch is made, not rejected as an unexpected property and not refused for lacking scratch.
