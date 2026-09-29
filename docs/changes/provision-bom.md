## Test plan

## @LETTER@. Provisioning under a profile that writes a BOM

### @LETTER@1. A BOM in the caller

1. `pwsh -NoProfile -Command '$OutputEncoding = [Text.UTF8Encoding]::new($true); & ./scripts/provision-room.ps1 m1mini -Restart'`

**Expected:** `provision state ok provisioned before as m1mini`, and the plan ends `provision done ok`. Nothing on the
room changes.

### @LETTER@2. A probe that lost its first lines

1. Run a copy of `provision-room.ps1` with the `$OutputEncoding = [Text.UTF8Encoding]::new($false)` line removed, the
   same way as @LETTER@1.

**Expected:** `provision state fail the remote script lost its first lines ...` and exit 3, not exit 6 and "a room
answers on 7781, which this script did not put there".

### @LETTER@3. The attach wait

1. Run `provision-room.ps1 <room> -Restart` without `-Yes`.

**Expected:** the attach line says it would wait up to 120s (180s when the hub links over zrok).
