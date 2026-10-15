## Test plan

## @LETTER@. The C toolchain test is host independent

### @LETTER@1. Run it on a machine with C:\msys64

1. On a Windows machine that has MSYS2 at C:\msys64 and signs commits, run `pwsh -NoProfile -File scripts/test-room-toolchain-c.ps1`

**Expected:** every check passes, including the payload sizes under 7800.
