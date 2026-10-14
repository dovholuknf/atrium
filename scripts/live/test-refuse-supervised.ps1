# Proves the live scripts refuse to run under an atrium room, and that sg4-control is opt-in. Fakes every process: it
# starts and stops nothing real, and every script it runs is given -WhatIf and a fake ancestor, so the worst a broken
# script does is say what it would have done. Usage: pwsh -NoProfile -File scripts/live/test-refuse-supervised.ps1
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\refuse-supervised.ps1"
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
function Proc($id, $parent, $cmd) { [pscustomobject]@{ ProcessId = $id; ParentProcessId = $parent; CommandLine = $cmd } }

# --- the ancestor walk, on a fake process table ---
$room = '"C:\Users\claude\.atrium\bin\atrium.exe" room --dir C:\Users\claude\.atrium2\room --http 127.0.0.1:7781'
$table = @(
  (Proc 100 90 'pwsh -NoProfile -File deploy.ps1'),
  (Proc 90 80 'pwsh.exe'),
  (Proc 80 1 $room),
  (Proc 70 1 '"C:\x\atrium.exe" room --dir C:\Users\claude\.atrium2\ctl'),   # a room that is not an ancestor
  (Proc 1 0 'System')
)
Check 'a room two levels up is found' ([bool](Find-SupervisingRoom -StartPid 100 -Processes $table))
Check 'the match carries the room pid' ((Find-SupervisingRoom -StartPid 100 -Processes $table).ProcessId -eq 80)
Check 'a room that is not an ancestor is ignored' (-not (Find-SupervisingRoom -StartPid 90 -Processes @(
  (Proc 90 1 'pwsh.exe'), (Proc 70 1 $room))))

# By command line, never by image name: an unrelated exe running the room subcommand matches, and an atrium.exe that
# is a hook does not.
$renamed = @((Proc 5 4 'pwsh'), (Proc 4 1 '"C:\agents\runner.exe" C:\bin\atrium.exe room --dir C:\r'))
Check 'a room under any image name matches by command line' ([bool](Find-SupervisingRoom -StartPid 5 -Processes $renamed))
$hook = @((Proc 5 4 'pwsh'), (Proc 4 1 '"C:\Users\claude\.atrium\bin\atrium.exe" hook --event PreToolUse'))
Check 'atrium.exe running a hook is not a room' (-not (Find-SupervisingRoom -StartPid 5 -Processes $hook))
$prose = @((Proc 5 4 'pwsh'), (Proc 4 1 'node tool.js --note "the room subcommand"'))
Check 'the word room in another command is not a room' (-not (Find-SupervisingRoom -StartPid 5 -Processes $prose))
$loop = @((Proc 5 4 'pwsh'), (Proc 4 5 'pwsh'))
Check 'a parent loop ends' (-not (Find-SupervisingRoom -StartPid 5 -Processes $loop))

# --- which rooms a deploy names ---
$def = Resolve-DeployRooms -Rooms @('local')
Check "local is hub and claude-sg4 only, got $($def -join ',')" (($def -join ',') -eq 'hub,claude-sg4')
$all = Resolve-DeployRooms -Rooms @('all')
Check 'all leaves sg4-control out' ($all -notcontains 'sg4-control' -and $all -contains 'sgg')
$both = Resolve-DeployRooms -Rooms @('local') -Control -IKnowItKillsMyShell
Check 'both switches add sg4-control, last of the local ones' (($both -join ',') -eq 'hub,claude-sg4,sg4-control')
Check '-Control alone refuses' ($null -ne (Test-ControlOptIn -Control))
Check '-IKnowItKillsMyShell alone refuses' ($null -ne (Test-ControlOptIn -IKnowItKillsMyShell))
Check 'neither is fine' ($null -eq (Test-ControlOptIn))
Check 'both is fine' ($null -eq (Test-ControlOptIn -Control -IKnowItKillsMyShell))
Check 'naming sg4-control in -Rooms needs both' ($null -ne (Test-ControlOptIn -Named))
Check 'naming sg4-control with both is fine' ($null -eq (Test-ControlOptIn -Named -Control -IKnowItKillsMyShell))
$threw = $false
try { $null = Resolve-DeployRooms -Rooms @('sg4-control') } catch { $threw = $true }
Check 'Resolve-DeployRooms refuses sg4-control by name without the switches' $threw

# --- the scripts themselves, run for real with -WhatIf and a fake ancestor ---
$fakeRoom = '"C:\Users\claude\.atrium\bin\atrium.exe" room --dir C:\Users\claude\.atrium2\room'
function Run([string]$Script, [string[]]$More, [string]$Ancestors, [string]$Job = '0') {
  $env:ATRIUM_LIVE_FAKE_JOB = $Job
  $env:ATRIUM_LIVE_FAKE_ANCESTORS = $Ancestors
  try { $out = & pwsh -NoProfile -File (Join-Path $PSScriptRoot $Script) -WhatIf @More 2>&1 | Out-String }
  finally { Remove-Item Env:ATRIUM_LIVE_FAKE_ANCESTORS, Env:ATRIUM_LIVE_FAKE_JOB -ErrorAction SilentlyContinue }
  [pscustomobject]@{ Code = $LASTEXITCODE; Out = $out }
}
$scripts = @{
  'atrium-deploy.ps1' = @('-NoBuild', '-Rooms', 'hub'); 'deploy-batch.ps1' = @(); 'stop-atrium.ps1' = @()
  'start-atrium.ps1' = @(); 'start-atrium-hub.ps1' = @(); 'start-atrium-room.ps1' = @(); 'start-atrium-control.ps1' = @()
}
foreach ($s in $scripts.Keys | Sort-Object) {
  $r = Run $s $scripts[$s] (ConvertTo-Json -InputObject @($fakeRoom) -Compress)
  Check "$s refuses under a room" ($r.Code -eq 1 -and $r.Out -match 'not under atrium')
  Check "$s says what to do" ($r.Out -match 'wt\.exe')
}
foreach ($s in 'atrium-deploy.ps1', 'stop-atrium.ps1', 'start-atrium.ps1') {
  $r = Run $s @('-Control') '[]'
  Check "$s -Control alone refuses" ($r.Code -eq 1 -and $r.Out -match 'IKnowItKillsMyShell')
}


# --- the job object: a parent that has exited breaks the ancestor walk, but the job still holds the process ---
$jobTable = @((Proc 80 1 $room), (Proc 100 90 'pwsh.exe'), (Proc 7 1 '"C:\Windows\explorer.exe"'),
  (Proc 8 1 '"C:\Program Files\WindowsTerminal.exe"'))
Check 'a parent that has exited leaves the ancestor walk empty' (-not (Find-SupervisingRoom -StartPid 100 -Processes $jobTable))
Check 'a job with a room and an orphan of it is a room job' ([bool](Find-RoomJob -JobPids 80, 100 -Processes $jobTable))
Check 'a job with a room and the desktop is the session job, not a room job' (-not (Find-RoomJob -JobPids 80, 100, 7 -Processes $jobTable))
Check 'a job with a room and the terminal is not a room job' (-not (Find-RoomJob -JobPids 80, 8 -Processes $jobTable))
Check 'a job with no room in it is not' (-not (Find-RoomJob -JobPids 100 -Processes $jobTable))
$env:ATRIUM_LIVE_FAKE_JOB = '1'
Check 'Find-RoomJob sees a faked job' ([bool](Find-RoomJob))
$env:ATRIUM_LIVE_FAKE_JOB = '0'
Check 'Find-RoomJob sees a faked no-job' (-not (Find-RoomJob))
Remove-Item Env:ATRIUM_LIVE_FAKE_JOB
$jobScripts = 'atrium-deploy.ps1', 'deploy-batch.ps1', 'stop-atrium.ps1', 'start-atrium.ps1', 'start-atrium-hub.ps1',
  'start-atrium-room.ps1', 'start-atrium-control.ps1'
foreach ($s in $jobScripts) {
  $r = Run $s $scripts[$s] '[]' '1'
  Check "$s refuses inside a job object even with no room ancestor" ($r.Code -eq 1 -and $r.Out -match 'job object' -and $r.Out -match 'wt\.exe')
}
if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
