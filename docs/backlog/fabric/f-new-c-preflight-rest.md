# f-new-c-preflight-rest

What the first f-c-preflight attempt built and this one left out on purpose. All of it is in the parked branch
`claude/f-c-preflight-full` (64b9670f), `scripts/room-toolchain-c.ps1` unless noted. Nothing here is reviewed.

- Network probes before a download: `New-NetPlan`, `Get-NetVerdict`.
- 7-Zip self-extractor exit diagnostics: `Get-SfxExitMessage`, `Format-UnpackDiagnostics`.
- Verdict on a held (locked) target file: `Get-HeldVerdict`.
- Whether this account may grant itself access at all: `Test-CanGrant`.
- Preflight of every step's directories, not only `-Msys2Dir` (vcpkg, checkout, downloads): `New-PreflightPlan`, `Get-PathLevels`, `Get-DriveKey`, `Select-TargetFacts`, `ConvertTo-DiskTargets`.
- Space verdicts for several drives at once: `Get-SpaceVerdicts` (this change has one drive and one constant, `$script:Msys2NeedBytes`).
- The fuller disk verdict, with `Get-AncestorPath` and `Get-DiskVerdict` (this change has `Test-Msys2Target`).
- Identity plans beyond set-what-was-given: `Get-IdentityPlan`, `Format-IdentityChange`.
- The preflight stage that runs all of it first in `scripts/room-toolchain.ps1`: `Invoke-CPreflight`.
