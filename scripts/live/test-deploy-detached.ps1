# Proves a detached deploy child gets every switch, says a refusal to its log and its card, and that the installer copies
# the files the scripts read. Fakes the hub with a local listener and points the log and the install at a temp folder, so
# nothing real is deployed, said or touched. Usage: pwsh -NoProfile -File scripts/live/test-deploy-detached.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$tmp = Join-Path ([IO.Path]::GetTempPath()) "deploy-detached-$PID"
New-Item -ItemType Directory -Force $tmp | Out-Null

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
$def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq 'Get-DetachedArgv' }, $true) |
  Select-Object -First 1
. ([scriptblock]::Create($def.Extent.Text))

function Params([hashtable]$Bound) { (Get-DetachedArgv 'C:\x\atrium-deploy.ps1' @('hub', 'claude-sg4') 'card1@room1' $Bound) -join ' ' }
$all = @{ Rooms = 'local'; NoBuild = [switch]$true; Immediate = [switch]$true; KillRunners = [switch]$true
  Control = [switch]$true; IKnowItKillsMyShell = [switch]$true; ReportTo = 'card1@room1'; WhatIf = [switch]$true }
$line = Params $all
foreach ($s in '-NoBuild', '-Immediate', '-KillRunners', '-Control', '-IKnowItKillsMyShell') {
  Check "the detached child is given $s" ($line -match "(^| )$s( |$)")
}
Check 'the child is given the resolved rooms and -Detached and the card' ($line -match '-Rooms hub,claude-sg4 -Detached -ReportTo card1@room1')
Check '-WhatIf is never forwarded' ($line -notmatch 'WhatIf')
Check 'a switch that was not set is not forwarded' ((Params @{ NoBuild = [switch]$true; Control = [switch]$false }) -notmatch 'Control')
Check 'a parameter that is not a switch is forwarded with its value' ((Params @{ Other = 'v' }) -match '-Other v$')

# A refusal in the detached child reaches the log and the card.
$port = 17900 + (Get-Random -Maximum 90)
$got = Join-Path $tmp 'said.json'
$job = Start-Job -ArgumentList $port, $got {
  param($p, $out)
  $l = [Net.HttpListener]::new(); $l.Prefixes.Add("http://127.0.0.1:$p/"); $l.Start()
  $c = $l.GetContext()
  $body = [IO.StreamReader]::new($c.Request.InputStream).ReadToEnd()
  [IO.File]::WriteAllText($out, (@{ room = $c.Request.Headers['X-Atrium-Room']; body = $body } | ConvertTo-Json -Compress))
  $c.Response.StatusCode = 200; $c.Response.Close(); $l.Stop()
}
Start-Sleep -Milliseconds 800
$log = Join-Path $tmp 'atrium-deploy.log'
$env:ATRIUM_LIVE_LOG = $log
$env:ATRIUM_LIVE_SAY_URL = "http://127.0.0.1:$port/v1/say"
$env:ATRIUM_LIVE_FAKE_ANCESTORS = '[]'
$env:ATRIUM_LIVE_FAKE_JOB = '0'
$task = $env:ATRIUM_TASK_ID; $env:ATRIUM_TASK_ID = $null
try {
  $ErrorActionPreference = 'Continue'
  $out = & pwsh -NoProfile -File "$PSScriptRoot\atrium-deploy.ps1" -Rooms local -Detached -Control -ReportTo 'card1@room1' 2>&1 | Out-String
  $code = $LASTEXITCODE
} finally {
  $ErrorActionPreference = 'Stop'
  Remove-Item Env:ATRIUM_LIVE_LOG, Env:ATRIUM_LIVE_SAY_URL, Env:ATRIUM_LIVE_FAKE_ANCESTORS, Env:ATRIUM_LIVE_FAKE_JOB -ErrorAction SilentlyContinue
  $env:ATRIUM_TASK_ID = $task
}
$null = Wait-Job $job -Timeout 20
Stop-Job $job -ErrorAction SilentlyContinue; Remove-Job $job -Force -ErrorAction SilentlyContinue
Check 'a refused detached child exits 1' ($code -eq 1)
Check 'the refusal is in atrium-deploy.log' ((Test-Path $log) -and (Get-Content $log -Raw) -match 'refusing: sg4-control is opt-in')
$said = if (Test-Path $got) { Get-Content $got -Raw | ConvertFrom-Json } else { $null }
Check 'the refusal was said to the card, through the room it names' ($said -and $said.room -eq 'room1' -and $said.body -match 'card1' -and $said.body -match 'refusing')

# The installer copies room-ssh.json and the other non-script files, and leaves the README.
$dest = Join-Path $tmp 'installed'
$null = & pwsh -NoProfile -File "$PSScriptRoot\install-live-scripts.ps1" -Dest $dest
Check 'the installer copies room-ssh.json beside the scripts' (Test-Path (Join-Path $dest 'room-ssh.json'))
Check 'the copy is the same as the shipped file' ((Get-Content (Join-Path $dest 'room-ssh.json') -Raw) -ceq (Get-Content "$PSScriptRoot\room-ssh.json" -Raw))
Check 'the installer still copies the scripts' (Test-Path (Join-Path $dest 'atrium-deploy.ps1'))
Check 'the README is not copied' (-not (Test-Path (Join-Path $dest 'README.md')))
$changed = Join-Path $dest 'room-ssh.json'
'{}' | Set-Content $changed
$res = & pwsh -NoProfile -File "$PSScriptRoot\install-live-scripts.ps1" -Dest $dest | Out-String
Check 'a changed json is replaced and said' ($res -match 'changed room-ssh\.json' -and (Get-Content $changed -Raw) -match 'sgg')

Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
