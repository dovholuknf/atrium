# Proves the live start helpers leave no job object over what they start: a caller already in a job starts through WMI,
# one outside any job uses breakaway, a WMI refusal falls back loudly, and a result still in a job is said plainly. Fakes
# every start, so nothing real is launched.
# Usage: pwsh -NoProfile -File scripts/live/test-start-jobless.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Get-StartRoute', 'New-LaunchScript', 'Start-Jobless') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}

$Base = Join-Path ([IO.Path]::GetTempPath()) "start-jobless-$PID"
New-Item -ItemType Directory $Base | Out-Null
try {
  Check 'in a job the route is wmi' ((Get-StartRoute $true) -eq 'wmi')
  Check 'outside any job the route is breakaway' ((Get-StartRoute $false) -eq 'breakaway')

  $env:ATRIUM_HOSTS = 'a.example,b.example'
  $env:ATRIUM_PCT = '50%'
  $env:ATRIUM_QUOTE = 'has "quote"'
  $env:LIVE_TEST_SECRET = 'hunter2'
  $cmd = Join-Path $Base 'launch.cmd'
  New-LaunchScript $cmd '"x.exe" room > out 2> err'
  $body = Get-Content $cmd -Raw
  Check 'the launch script sets ATRIUM_HOSTS' ($body -match 'set "ATRIUM_HOSTS=a.example,b.example"')
  Check 'a percent sign is doubled for cmd' ($body -match 'set "ATRIUM_PCT=50%%"')
  Check 'a value with a quote is skipped' ($body -notmatch 'ATRIUM_QUOTE')
  Check 'nothing outside ATRIUM_* reaches the disk' ($body -notmatch 'hunter2|LIVE_TEST_SECRET')
  Check 'the command is last' ((Get-Content $cmd)[-1] -eq '"x.exe" room > out 2> err')

  $script:inJob = @{}
  $script:calls = [System.Collections.Generic.List[string]]::new()
  $script:wmiFails = $false
  function Test-ProcessInJob([int]$ProcId) { $script:inJob[$ProcId] }
  function Start-Wmi([string]$CommandLine) {
    $script:calls.Add("wmi $CommandLine")
    if ($script:wmiFails) { throw 'Win32_Process Create returned 2' }
    return 200
  }
  function Start-Breakaway([string]$CommandLine) { $script:calls.Add("breakaway $CommandLine"); return 300 }

  function Reset([bool]$SelfInJob, [bool]$WmiFails, $WmiPidInJob, $BreakawayPidInJob) {
    $said.Clear(); $script:calls.Clear()
    $script:wmiFails = $WmiFails
    $script:inJob = @{ [int]$PID = $SelfInJob; 200 = $WmiPidInJob; 300 = $BreakawayPidInJob }
  }

  Reset $true $false $false $true
  $p = Start-Jobless '"x.exe" room > out 2> err' 'room'
  Check 'from a job it starts through WMI and returns that pid' ($p -eq 200 -and $calls.Count -eq 1 -and $calls[0] -like 'wmi cmd.exe /c *launch-room.cmd*')
  Check 'it says the result is in no job' (($said -join "`n") -match 'in no job object')

  Reset $false $false $false $true
  $p = Start-Jobless '"x.exe" room > out 2> err' 'room'
  Check 'outside any job it uses breakaway' ($p -eq 300 -and $calls.Count -eq 1 -and $calls[0] -like 'breakaway cmd.exe /c "*x.exe*')

  Reset $true $true $false $false
  $p = Start-Jobless '"x.exe" room > out 2> err' 'room'
  Check 'a WMI refusal falls back to breakaway' ($p -eq 300 -and $calls.Count -eq 2 -and $calls[1] -like 'breakaway *')
  Check 'the fallback is said as a WARNING with the reason' (($said -join "`n") -match 'WARNING: could not start room through WMI \(Win32_Process Create returned 2\)')

  Reset $true $true $false $true
  $p = Start-Jobless '"x.exe" room > out 2> err' 'room'
  Check 'a result still in a job says so plainly and says what to do' (($said -join "`n") -match 'inside a job object that refused breakaway, inherited from the terminal or task that ran this script' -and ($said -join "`n") -match 'outside any job')
} finally {
  Remove-Item $Base -Recurse -Force -ErrorAction SilentlyContinue
  Remove-Item Env:ATRIUM_HOSTS, Env:ATRIUM_PCT, Env:ATRIUM_QUOTE, Env:LIVE_TEST_SECRET -ErrorAction SilentlyContinue
}

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
