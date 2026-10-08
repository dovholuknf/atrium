## Test plan

## @LETTER@. No PowerShell off Windows

### @LETTER@1. Linux CI starts no pwsh

1. Run `scripts/ci-linux.sh` on a machine with docker.
2. Check the build output for `pwsh`.

**Expected:** the run passes, the environment banner has no pwsh line, and the PowerShell parse step reports that it
was skipped.

### @LETTER@2. Windows still parses its scripts

1. Run `scripts/ci.sh` in the Windows job.

**Expected:** the `powershell` check runs `scripts/check-powershell.ps1` and passes.
