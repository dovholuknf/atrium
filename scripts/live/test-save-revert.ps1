# Proves Save-Revert names the snapshot after the file it copies. Runs in a temp directory and touches no live
# hub, room or bin directory. Usage: pwsh scripts/live/test-save-revert.ps1 -A <exe> -B <exe>
# Two different real binaries: the snapshot name must follow whichever one sits at the hook path.
param([Parameter(Mandatory)][string]$A, [Parameter(Mandatory)][string]$B)

$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\live-common.ps1"

$tmp = Join-Path ([IO.Path]::GetTempPath()) "save-revert-$(Get-Random)"
New-Item -ItemType Directory $tmp | Out-Null
$AtriumBinDir = $tmp
$AtriumBin = Join-Path $tmp 'atrium.exe'
$LiveTag = '[test]'
$LiveLog = Join-Path $tmp 'test.log'
$HubHealth = 'http://127.0.0.1:1/never'   # must not be consulted
$fail = 0

function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}

foreach ($src in $A, $B) {
  $want = Get-BinLabel $src
  Copy-Item $src $AtriumBin -Force
  Save-Revert
  $snaps = @(Get-ChildItem $tmp -Filter 'atrium.revert-*.exe')
  Check "one snapshot for $(Split-Path $src -Leaf)" ($snaps.Count -eq 1)
  Check "name is atrium.revert-$want.exe, got $($snaps[0].Name)" ($want -and $snaps[0].Name -eq "atrium.revert-$want.exe")
  Check 'snapshot bytes equal the source' ((Get-FileHash $snaps[0].FullName).Hash -eq (Get-FileHash $src).Hash)
}

# A file that cannot answer: not a program at all.
Set-Content $AtriumBin 'not an executable'
Save-Revert
$snaps = @(Get-ChildItem $tmp -Filter 'atrium.revert-*.exe')
Check "unanswering file gets unknown-<ts>, got $($snaps[0].Name)" ($snaps.Count -eq 1 -and $snaps[0].Name -match '^atrium\.revert-unknown-\d{14}\.exe$')

Remove-Item $tmp -Recurse -Force
if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
