## Test plan

## @LETTER@. The deploy script picks the right ssh user and says why a room failed

### @LETTER@1. Route and user selection

1. Run `powershell -NoProfile -File scripts/live/test-ssh-route.ps1` and `pwsh -NoProfile -File scripts/live/test-ssh-route.ps1`.

**Expected:** every line passes, including the user on each candidate, the file and hub-record lookups and both failure
wordings.

### @LETTER@2. A refused key is not offline

1. With no room deployed, run `atrium-deploy.ps1 -Rooms sgg` on a machine whose key sgg refuses, or with `room-ssh.json`
   naming a user that is not on sgg.

**Expected:** the result line reads `sgg: failed - ssh refused as <user>`, not `offline`.
