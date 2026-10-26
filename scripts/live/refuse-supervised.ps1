# Shared by the scripts that stop or start atrium. Dot-source it, then call Assert-NotSupervised first thing:
#
#   . "$PSScriptRoot\refuse-supervised.ps1"
#   Assert-NotSupervised
#
# WHY. A room owns its terminals, so stopping a room ends every shell under it, and a deploy running in one of those
# shells ends with it, halfway through. On 2026-10-14 a deploy run from sg4-control restarted the hub and claude-sg4,
# then force-stopped sg4-control, which killed the shell and the deploy. The scripts now refuse to start there.
#
# HOW. Walk this process's ancestors and look at each one's COMMAND LINE for the `room` (or `daemon`) subcommand of an
# atrium binary. Never the image name: agents run under many exes, and atrium.exe is also every hook.
#
# -WhatIf does not excuse it. A dry run from inside a room is exactly how the real one gets pasted there later.
#
# FOR TESTS: ATRIUM_LIVE_FAKE_ANCESTORS is a JSON array of command lines standing in for the real ancestors, and `[]`
# means none. Nothing in production sets it.

# A command line that is an atrium room: an atrium executable, then the subcommand. `hook`, `session` and the rest
# are the same binary and are not rooms.
$script:SupervisorPattern = '(?i)atrium[^\\/"\s]*(\.exe)?"?\s+(room|daemon)(\s|$)'

# Find-SupervisingRoom returns the first ancestor of -StartPid that is a room, or nothing. -Processes is a table of
# ProcessId, ParentProcessId and CommandLine, read from the machine when it is not given.
function Find-SupervisingRoom([int]$StartPid = $PID, $Processes) {
  if ($env:ATRIUM_LIVE_FAKE_ANCESTORS) {
    $fake = @(ConvertFrom-Json $env:ATRIUM_LIVE_FAKE_ANCESTORS)
    $hit = $fake | Where-Object { $_ -match $script:SupervisorPattern } | Select-Object -First 1
    if ($hit) { return [pscustomobject]@{ ProcessId = 0; CommandLine = $hit } }
    return
  }
  if ($null -eq $Processes) { $Processes = Get-CimInstance Win32_Process }
  $byId = @{}
  foreach ($p in $Processes) { $byId[[int]$p.ProcessId] = $p }
  $seen = @{}
  $cur = $byId[$StartPid]
  while ($cur -and -not $seen.ContainsKey([int]$cur.ProcessId)) {
    $seen[[int]$cur.ProcessId] = $true
    if ($cur.CommandLine -and $cur.CommandLine -match $script:SupervisorPattern) { return $cur }
    $cur = $byId[[int]$cur.ParentProcessId]
  }
}

# Find-RoomJob answers whether the job this process is in is a ROOM'S: the job holds a room process and no desktop (no
# terminal, no shell). A parent that has exited breaks the ancestor walk, but not this: on 2026-10-14 a hub whose
# parent was gone died with its room. -JobPids is the pids in the job, read from the machine when not given. Returns
# the room, or nothing.
#
# NOT "any job". On this machine the whole interactive session is one job (hub, both rooms, Windows Terminal, hundreds
# of processes), so membership alone would refuse everything, a fresh window included.
# ATRIUM_LIVE_FAKE_JOB=1 or 0 stands in for the answer, for tests.
function Find-RoomJob([int[]]$JobPids, $Processes) {
  if ($env:ATRIUM_LIVE_FAKE_JOB) {
    if ($env:ATRIUM_LIVE_FAKE_JOB -eq '1') { return [pscustomobject]@{ ProcessId = 0; CommandLine = '(a faked room job)' } }
    return
  }
  if ($null -eq $JobPids) { $JobPids = Get-CurrentJobPids }
  if (-not $JobPids) { return }
  if ($null -eq $Processes) { $Processes = Get-CimInstance Win32_Process }
  $byId = @{}
  foreach ($p in $Processes) { $byId[[int]$p.ProcessId] = $p }
  foreach ($id in $JobPids) {
    $r = $byId[[int]$id]
    if (-not $r -or -not $r.CommandLine -or $r.CommandLine -notmatch $script:SupervisorPattern) { continue }
    # The session's job holds the terminal and the shell, a room's does not. Descent is no test: the hub of 2026-10-14
    # had a parent that had exited, and every process tree ends at an exited parent.
    $whole = $true
    foreach ($m in $JobPids) {
      $mp = $byId[[int]$m]
      if ($mp -and $mp.CommandLine -match '(?i)WindowsTerminal\.exe|explorer\.exe') { $whole = $false; break }
    }
    if ($whole) { return $r }
  }
}

# Get-CurrentJobPids lists the pids in the job object this process is in, empty when it is in none.
function Get-CurrentJobPids {
  if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) { return @() }
  if (-not ('AtriumLive.JobList' -as [type])) {
    Add-Type -Namespace AtriumLive -Name JobList -MemberDefinition @'
[DllImport("kernel32.dll")] public static extern bool QueryInformationJobObject(System.IntPtr job, int cls, byte[] buf, int len, out int ret);
'@
  }
  if (-not [AtriumLive.JobList].GetMethod('QueryInformationJobObject')) {
    throw 'AtriumLive.JobList in this shell is from an older refuse-supervised.ps1 and cannot be reloaded. open a fresh pwsh and run this again'
  }
  # JobObjectBasicProcessIdList: two counts, then the ids as pointer-sized values. Null job means this process's own.
  $size = 8 + 8 * 4096
  $b = New-Object byte[] $size; $n = 0
  if (-not [AtriumLive.JobList]::QueryInformationJobObject([IntPtr]::Zero, 3, $b, $size, [ref]$n)) { return @() }
  $cnt = [BitConverter]::ToUInt32($b, 4)
  return @(0..($cnt - 1) | ForEach-Object { [int][BitConverter]::ToUInt64($b, 8 + 8 * $_) })
}

# Get-SupervisorName names the room a room command line is, from its --dir: sg4-control's is ...\ctl, claude-sg4's is
# ...\room. Anything else is $null, which Assert-NotSupervised treats as every room.
function Get-SupervisorName([string]$CommandLine) {
  if ($CommandLine -notmatch '(?i)--dir\s+"?([^"\s]+)') { return $null }
  switch -Regex ($Matches[1].TrimEnd('\', '/')) {
    '(?i)[\\/]ctl$' { return 'sg4-control' }
    '(?i)[\\/]room$' { return 'claude-sg4' }
  }
  return $null
}

# Assert-NotSupervised ends the script with exit 1 and a plain message when it runs under a room. With -Targets, the
# rooms this run stops, it refuses only when the room it runs under is one of them: a shell in sg4-control may
# restart the hub, but not sg4-control. A room it cannot name counts as a target.
function Assert-NotSupervised([string[]]$Targets) {
  $room = Find-SupervisingRoom
  if (-not $room) { $room = Find-RoomJob }
  if ($room -and $PSBoundParameters.ContainsKey('Targets')) {
    $name = Get-SupervisorName $room.CommandLine
    if ($name -and $Targets -notcontains $name) { return }
    if ($name) {
      Write-Host "refusing: this shell runs under $name, and this run restarts $name, which would kill this shell halfway."
      Write-Host "leave $name out, or run it from a terminal that is not under atrium: wt.exe pwsh -NoProfile"
      exit 1
    }
  }
  $room = Find-SupervisingRoom
  if ($room) {
    Write-Host ("refusing: this shell is not under atrium's control, and this script stops or starts atrium. It is " +
      "running inside an atrium room (pid $($room.ProcessId): $($room.CommandLine)), and stopping that room ends this " +
      "shell and this script with it, halfway through.")
  } elseif ($jobRoom = Find-RoomJob) {
    Write-Host ("refusing: this process is inside a Windows job object, which is how a room holds what it started, even " +
      "when no room is among its parents. Stopping the room ends the job, and this script and anything it starts with it.")
  } else { return }
  Write-Host 'run it from a terminal that is not under atrium, such as a fresh window: wt.exe pwsh -NoProfile'
  exit 1
}

# Test-ControlOptIn answers why sg4-control cannot be included, or $null when it can. It takes both switches or
# neither: -Control alone and -IKnowItKillsMyShell alone are refused. -Named is that sg4-control was asked for by name.
function Test-ControlOptIn([switch]$Control, [switch]$IKnowItKillsMyShell, [switch]$Named) {
  if ($Control -and $IKnowItKillsMyShell) { return $null }
  if ($Control -or $IKnowItKillsMyShell -or $Named) {
    return ('sg4-control is opt-in, because stopping it kills the shell of the orchestrator that holds its terminal. ' +
      'Include it with both -Control and -IKnowItKillsMyShell, or with neither.')
  }
  return $null
}

# Assert-ControlOptIn ends the script with exit 1 when Test-ControlOptIn refuses.
function Assert-ControlOptIn([switch]$Control, [switch]$IKnowItKillsMyShell, [switch]$Named) {
  $why = Test-ControlOptIn -Control:$Control -IKnowItKillsMyShell:$IKnowItKillsMyShell -Named:$Named
  if (-not $why) { return }
  Write-Host "refusing: $why"
  exit 1
}

# Resolve-DeployRooms turns -Rooms into the fixed-order list of rooms to deploy. `local` and `all` leave sg4-control
# out. It is in only with both switches, or named in -Rooms alongside both. Throws on an unknown room, and on
# sg4-control asked for without both switches.
function Resolve-DeployRooms([string[]]$Rooms = @('local'), [switch]$Control, [switch]$IKnowItKillsMyShell) {
  $remote = 'sg3', 'm1mini', 'sgg'
  $known = @('hub', 'claude-sg4', 'sg4-control') + $remote
  $want = @()
  foreach ($r in ($Rooms -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })) {
    switch ($r) {
      'local' { $want += 'hub', 'claude-sg4' }
      'all' { $want += $known | Where-Object { $_ -ne 'sg4-control' } }
      default {
        if ($known -notcontains $r) { throw "unknown room '$r'. known: $($known -join ', '), local, all" }
        $want += $r
      }
    }
  }
  $named = $want -contains 'sg4-control'
  $why = Test-ControlOptIn -Control:$Control -IKnowItKillsMyShell:$IKnowItKillsMyShell -Named:$named
  if ($why) { throw $why }
  if ($Control) { $want += 'sg4-control' }
  return @($known | Where-Object { $_ -in $want })
}
