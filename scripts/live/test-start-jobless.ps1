# Proves the live start helpers leave no job object over what they start: a caller already in a job starts through the
# task scheduler, then WMI, one outside any job uses breakaway, each refusal falls back loudly, and a result still in a
# job is said plainly. Fakes every start. The one real process is a cmd.exe running a launch script in a temp folder.
# Usage: pwsh -NoProfile -File scripts/live/test-start-jobless.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Get-StartRoute', 'New-LaunchScript', 'Get-PathRecord', 'Get-LoginPath', 'Start-Jobless') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}

$Base = Join-Path ([IO.Path]::GetTempPath()) "start-jobless-$PID"
New-Item -ItemType Directory $Base | Out-Null
$ToolchainDir = $Base
try {
  Check 'in a job the route is the task scheduler' ((Get-StartRoute $true) -eq 'task')
  Check 'outside any job the route is breakaway' ((Get-StartRoute $false) -eq 'breakaway')
  Check 'a job that could not be read is treated as a job' ((Get-StartRoute $null) -eq 'task')

  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq 'Start-Task' }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create(($def.Extent.Text -replace '^function Start-Task', 'function Start-RealTask')))
  $sched = [System.Collections.Generic.List[string]]::new()
  $script:taskXml = $null; $script:runFails = $false; $script:state = '"atrium-live-control","Running","0"'
  function Start-Sleep {}
  function schtasks.exe {
    $sched.Add("$($args[0]) $($args[[array]::IndexOf($args, '/TN') + 1])")
    $global:LASTEXITCODE = 0
    switch ($args[0]) {
      '/Create' { $script:taskXml = Get-Content -LiteralPath $args[-1] -Raw }
      '/Run' { if ($script:runFails) { $global:LASTEXITCODE = 1; 'ERROR: Access is denied.' } }
      '/Query' { '"TaskName","Status","Last Result"'; $script:state }
    }
  }
  $launchCmd = Join-Path $Base 'launch-control.cmd'
  $n = Start-RealTask $launchCmd 'control'
  $x = [xml]$script:taskXml
  Check 'the task has no trigger, so never at boot, logon or on a repeat' ($x.Task.Triggers -eq $null -and $script:taskXml -notmatch '(?i)Trigger|Repetition|Boot|Logon(?!Type)')
  Check 'the task runs once now and is deleted after its run begins' (($sched -join ',') -eq '/Create atrium-live-control,/Run atrium-live-control,/Query atrium-live-control,/Delete atrium-live-control' -and $n -eq 'atrium-live-control')
  Check 'the task starts hidden, in a headless console' ($x.Task.Actions.Exec.Command -eq 'conhost.exe' -and $x.Task.Actions.Exec.Arguments -eq "--headless cmd.exe /c `"$launchCmd`"" -and $x.Task.Settings.Hidden -eq 'true')
  Check 'the task has no time limit, so the scheduler never stops the room' ($x.Task.Settings.ExecutionTimeLimit -eq 'PT0S')
  Check 'the task xml is removed' (-not (Test-Path ([IO.Path]::ChangeExtension($launchCmd, 'xml'))))
  $sched.Clear(); $script:runFails = $true
  $threw = $false
  try { $null = Start-RealTask $launchCmd 'control' } catch { $threw = "$_" -match 'Access is denied' }
  Check 'a task that would not start throws and is deleted too' ($threw -and ($sched -join ',') -eq '/Create atrium-live-control,/Run atrium-live-control,/Delete atrium-live-control')
  $sched.Clear(); $script:runFails = $false; $script:state = '"atrium-live-control","Ready","267011"'
  $threw = $false
  try { $null = Start-RealTask $launchCmd 'control' } catch { $threw = "$_" -match 'did not run in 10s' }
  Check 'a task that never runs (no console logon) throws, so the start falls back, and is deleted' ($threw -and $sched[-1] -eq '/Delete atrium-live-control')
  Remove-Item Function:schtasks.exe, Function:Start-Sleep

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
  $script:taskFails = $true
  function Test-ProcessInJob([int]$ProcId) { $script:inJob[$ProcId] }
  function Start-Task([string]$Launch, [string]$Tag) {
    $script:calls.Add("task $Tag $Launch")
    if ($script:taskFails) { throw 'Access is denied' }
    return "atrium-live-$Tag"
  }
  function Start-Wmi([string]$CommandLine) {
    $script:calls.Add("wmi $CommandLine")
    if ($script:wmiFails) { throw 'Win32_Process Create returned 2' }
    return 200
  }
  function Start-Breakaway([string]$CommandLine, [string]$Dir) { $script:calls.Add("breakaway $CommandLine in $Dir"); return 300 }

  function Reset([bool]$SelfInJob, [bool]$WmiFails, $WmiPidInJob, $BreakawayPidInJob) {
    $said.Clear(); $script:calls.Clear()
    $script:wmiFails = $WmiFails
    $script:inJob = @{ [int]$PID = $SelfInJob; 200 = $WmiPidInJob; 300 = $BreakawayPidInJob }
  }

  function Get-Launch([string]$Tag) { Get-ChildItem -LiteralPath $Base -Filter "launch-$Tag-*.cmd" | Select-Object -First 1 -ExpandProperty FullName }

  Reset $true $false $false $true
  $script:taskFails = $false
  $p = Start-Jobless '"x.exe" room --log-dir logs' 'control' $Base
  Check 'from a job it starts through the task scheduler and nothing else' ($p -eq 0 -and $calls.Count -eq 1 -and $calls[0] -like 'task control *launch-control-*.cmd')
  Check 'the task start is said with the task name' (($said -join "`n") -match 'started through the task scheduler as task atrium-live-control, outside this shell''s job')
  Check 'the task runs the launch script with the command last' ((Get-Content (Get-Launch 'control'))[-1] -eq '"x.exe" room --log-dir logs')
  Check 'the launch script runs the command in the root folder' ((Get-Content (Get-Launch 'control')) -contains "cd /d `"$Base`"")
  Reset $true $false $false $true
  $threw = $false
  try { $null = Start-Jobless '"x.exe" room --log-dir logs' 'control' '' } catch { $threw = "$_" -match 'no working directory' }
  Check 'a start with no working directory is refused before anything starts' ($threw -and $calls.Count -eq 0)
  $script:taskFails = $true

  # f-038: the previous start's cmd.exe still runs its launch script when the next start writes one.
  Get-ChildItem -LiteralPath $Base -Filter 'launch-*.cmd' | Remove-Item -Force
  $old = Join-Path $Base 'launch-control.cmd'
  $ready = Join-Path $Base 'ready'; $stop = Join-Path $Base 'stop'; $hit = Join-Path $Base 'hit.txt'
  New-LaunchScript $old "pwsh -NoProfile -c `"New-Item '$ready' | Out-Null; while (-not (Test-Path '$stop')) { [Threading.Thread]::Sleep(50) }`""
  Set-Content (Join-Path $Base 'launch-preview-board.cmd') 'rem another tag'
  $prev = Start-Process cmd.exe -ArgumentList '/c', "`"$old`"" -PassThru -WindowStyle Hidden
  $deadline = (Get-Date).AddSeconds(30)
  while (-not (Test-Path $ready) -and (Get-Date) -lt $deadline) { [Threading.Thread]::Sleep(50) }
  Reset $true $false $false $true
  $script:taskFails = $false
  $null = Start-Jobless "echo relaunched > `"$hit`"" 'control' $Base
  $first = Get-Launch 'control'
  New-Item $stop | Out-Null
  $exited = $prev.WaitForExit(30000)
  Check 'the previous start''s cmd ran its room' (Test-Path $ready)
  Check 'the previous start''s cmd ends with its room and runs nothing the next start wrote' ($exited -and -not (Test-Path $hit))
  Check 'a start writes a launch script of its own name and removes the old one for its tag' ($first -and -not (Test-Path $old))
  Check 'another tag''s launch script is kept' (Test-Path (Join-Path $Base 'launch-preview-board.cmd'))
  Reset $true $false $false $true
  $null = Start-Jobless '"x.exe" room --log-dir logs' 'control' $Base
  Check 'the next start leaves one launch script for its tag' (@(Get-ChildItem -LiteralPath $Base -Filter 'launch-control*.cmd').Count -eq 1 -and -not (Test-Path $first))
  $script:taskFails = $true

  Reset $true $false $false $true
  $p = Start-Jobless '"x.exe" room --log-dir logs' 'room' $Base
  Check 'a task refusal falls back to WMI, said as a WARNING' (($said -join "`n") -match 'WARNING: could not start room through the task scheduler \(Access is denied\)\. trying WMI')
  Check 'from a job with no task it starts through WMI and returns that pid' ($p -eq 200 -and $calls.Count -eq 2 -and $calls[0] -like 'task room *' -and $calls[1] -like 'wmi cmd.exe /c *launch-room-*.cmd*')
  Check 'it says the result is in no job' (($said -join "`n") -match 'in no job object')
  Check 'with no record it says so and names room-toolchain.ps1' (($said -join "`n") -match 'no PATH record at .*path\.txt.*room-toolchain\.ps1')

  New-LaunchScript $cmd '"x.exe" room' @('C:\tools\go\bin', 'C:\tools\100%jq', 'C:\bad"dir')
  $body = Get-Content $cmd -Raw
  Check 'the launch script puts the record ahead of PATH' ($body -match [regex]::Escape('set "PATH=C:\tools\go\bin;C:\tools\100%%jq;%PATH%"'))
  New-LaunchScript $cmd '"x.exe" room'
  Check 'no record sets no PATH' ((Get-Content $cmd -Raw) -notmatch 'set "PATH=')

  Set-Content (Join-Path $Base 'path.txt') "C:\tools\go\bin`r`n`r`n  C:\tools\jq  `r`n"
  $oldPath = $env:Path
  try {
    $env:Path = "C:\Windows;C:\tools\jq"
    Reset $true $false $false $false
    $null = Start-Jobless '"x.exe" room --log-dir logs' 'room' $Base
    $launch = Get-Content (Get-Launch 'room') -Raw
    $login = @(Get-LoginPath | Where-Object { @('C:\tools\go\bin', 'C:\tools\jq') -notcontains $_ } | ForEach-Object { $_ -replace '%', '%%' })
    $want = (@('C:\tools\go\bin', 'C:\tools\jq') + $login) -join ';'
    Check 'a WMI start carries the room''s record, then Machine and User PATH from the registry' ($launch -match [regex]::Escape("set `"PATH=$want;%PATH%`""))
    $userKey = (Get-Item -LiteralPath 'HKCU:\Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    $userDirs = @($userKey -split ';' | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_.Trim()) } | Where-Object { $_ })
    Check 'the launch script holds every registry User PATH entry' (@($userDirs | Where-Object { $launch.Contains(($_ -replace '%', '%%')) }).Count -eq $userDirs.Count)
    Check 'a breakaway start inherits the record first, once each' ($env:Path -eq 'C:\tools\go\bin;C:\tools\jq;C:\Windows')
    Check 'it says the record it used' (($said -join "`n") -match [regex]::Escape('PATH record first: C:\tools\go\bin;C:\tools\jq'))
  } finally { $env:Path = $oldPath }

  Reset $false $false $false $true
  $p = Start-Jobless '"x.exe" room --log-dir logs' 'room' $Base
  Check 'outside any job it uses breakaway' ($p -eq 300 -and $calls.Count -eq 1 -and $calls[0] -eq "breakaway `"x.exe`" room --log-dir logs in $Base")

  Reset $true $true $false $false
  $p = Start-Jobless '"x.exe" room --log-dir logs' 'room' $Base
  Check 'a WMI refusal falls back to breakaway' ($p -eq 300 -and $calls.Count -eq 3 -and $calls[2] -like 'breakaway *')
  Check 'the fallback is said as a WARNING with the reason' (($said -join "`n") -match 'WARNING: could not start room through WMI \(Win32_Process Create returned 2\)')

  Reset $true $true $false $true
  $p = Start-Jobless '"x.exe" room --log-dir logs' 'room' $Base
  Check 'a result still in a job says so plainly and says what to do' (($said -join "`n") -match 'inside a job object that refused breakaway, inherited from the terminal or task that ran this script' -and ($said -join "`n") -match 'outside any job')
} finally {
  Remove-Item $Base -Recurse -Force -ErrorAction SilentlyContinue
  Remove-Item Env:ATRIUM_HOSTS, Env:ATRIUM_PCT, Env:ATRIUM_QUOTE, Env:LIVE_TEST_SECRET -ErrorAction SilentlyContinue
}

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
