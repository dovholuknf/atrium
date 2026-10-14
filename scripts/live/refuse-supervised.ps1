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

# Assert-NotSupervised ends the script with exit 1 and a plain message when it runs under a room.
function Assert-NotSupervised {
  $room = Find-SupervisingRoom
  if (-not $room) { return }
  Write-Host ("refusing: this shell is not under atrium's control, and this script stops or starts atrium. It is running " +
    "inside an atrium room (pid $($room.ProcessId): $($room.CommandLine)), and stopping that room ends this shell " +
    "and this script with it, halfway through.")
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
