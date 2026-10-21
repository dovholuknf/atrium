# Shared by the live scripts in this directory. Dot-source it:
#
#   . "$PSScriptRoot\live-common.ps1"
#
# These are this machine's deploy scripts, kept in the repo so they are reviewed and versioned. They are deployed by
# copying them over C:\Users\claude\.atrium2\scripts\. See docs/archive/one-atrium-cutover.md.
#
# THE ONE BINARY IS ALSO THE HOOK BINARY. C:\Users\claude\.atrium\bin\atrium.exe is what every hook line in
# settings.json runs, several times a second, as well as the atrium (`run --no-room`) and the room (`room`). Two rules
# follow, and every script here keeps them:
#
#   1. A process is matched by its image directory AND its subcommand, never by image name. `Name='atrium.exe'` alone
#      would stop every hook in flight.
#   2. A new binary goes in by staging it as atrium.next.exe and swapping with two renames. A copy over the live name
#      fails while anything runs it, and would widen the gap a hook can land in from microseconds to the whole copy.

$AtriumBin    = 'C:\Users\claude\.atrium\bin\atrium.exe'
$AtriumBinDir = Split-Path $AtriumBin
$Repo         = 'D:\git\github\dovholuknf\atrium'
$AtriumNew    = Join-Path $Repo 'build.claude\atrium.exe'
# A different build to deploy, for a -WhatIf rehearsal from another worktree.
if ($env:ATRIUM_NEW_BUILD) { $AtriumNew = $env:ATRIUM_NEW_BUILD }
$Base         = 'C:\Users\claude\.atrium2'
$HubDir       = Join-Path $Base 'hub'
$RoomDir      = Join-Path $Base 'room'
$RoomDb       = 'C:\Users\claude\.atrium\atrium.db'
$HubHealth    = 'http://127.0.0.1:7778/_hub/health'
$HubRooms     = 'http://127.0.0.1:7778/_hub/rooms'
# What the hub calls the room these scripts restart. A room COUNT says nothing about it: m1mini and sg4-wsl attach to
# the same hub, so "rooms >= 1" passed on 2026-09-29 17:05 while this room was being refused.
$RoomName     = 'claude-sg4'
$RoomShutdown = 'http://127.0.0.1:7781/v1/shutdown'
$RoomHealth   = 'http://127.0.0.1:7781/v1/health'
# A request the room answers from its store. `/v1/health` and the hub's view of the room both answer while the store is
# frozen, which is how a deadlocked room was reported healthy on 2026-09-29.
$RoomServes   = 'http://127.0.0.1:7781/v1/settings'

# The sg4-control room: a second room on this machine, for the orchestrator. Its own binary directory on purpose, so
# Find-Atrium never matches it and a deploy of claude-sg4 never stops it. start-atrium-control.ps1 starts it.
$CtlBinDir    = 'C:\Users\claude\.atrium\ctl-bin'
$CtlDir       = Join-Path $Base 'ctl'
$CtlHealth    = 'http://127.0.0.1:7791/v1/health'
$CtlShutdown  = 'http://127.0.0.1:7791/v1/shutdown'

# The exact command lines. The hub's directory flag is --atrium-dir, because `run` also takes the room's --dir.
# The address rooms are told to dial: ATRIUM_LINK_ADVERTISE (host:port) when set, else this machine's IPv4 address on the
# interface of the default route, so a changed LAN address needs no edit here. A fixed address went stale on 2026-10-14.
function Get-LinkAdvertise {
  if ($env:ATRIUM_LINK_ADVERTISE) { return $env:ATRIUM_LINK_ADVERTISE }
  try {
    # Default routes by cost, skipping a tunnel (the ziti-edge-tunnel adapter, addresses in 100.64.0.0/10).
    $routes = Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop |
      Sort-Object { $_.RouteMetric + $_.InterfaceMetric }
    foreach ($r in $routes) {
      if ((Get-NetAdapter -InterfaceIndex $r.InterfaceIndex -ErrorAction SilentlyContinue).Name -match 'ziti') { continue }
      $ip = Get-NetIPAddress -InterfaceIndex $r.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -notlike '169.254.*' -and $_.IPAddress -notmatch '^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\.' } |
        Select-Object -First 1 -ExpandProperty IPAddress
      if ($ip) { return "${ip}:7779" }
    }
  } catch { }
  # Not fatal: every live script dot-sources this, and only a direct room on another machine needs the address.
  Write-Warning 'cannot derive the hub link address from the default route; advertising 127.0.0.1:7779 (set ATRIUM_LINK_ADVERTISE=host:7779)'
  return '127.0.0.1:7779'
}
$LinkAdvertise = Get-LinkAdvertise
$HubArgs = @('run', '--no-room', '--addr', '127.0.0.1:7778', '--link', '0.0.0.0:7779',
  '--link-advertise', $LinkAdvertise, '--atrium-dir', $HubDir)
# The hub serves the board from the main checkout, so a board change merged to claude/main is live on a browser
# refresh with no build. clint 2026-10-07: "i'd like this hub to be switched over to allowing live edits".
$HubArgs += @('--board', (Join-Path $Repo 'internal\api\web'))
# One board view per worker worktree under D:\worktrees, at http://<worktree-folder>.localhost:7778/, so several UI changes
# are live side by side. See docs/rnd/board-views-design.md.
$HubArgs += @('--board-views', 'D:\worktrees')
# The hub's ziti transports are settings in the gear, and these flags SEED them: a flag wins for that run, and the first
# change made in the gear replaces it. They stay here so a hub with no stored settings still binds all three, since
# m1mini and sg3 attach over ziti only. The identity is enrolled outside the repo and never committed. Only added while
# it is there, and a controller that is down leaves the hub up on direct. See docs/changes/f-hub-transport-settings.md.
$HubZitiIdentity = 'C:\Users\claude\.ziti\identities\atrium\atrium-hub-sg4.json'
if (Test-Path $HubZitiIdentity) {
  $HubArgs += @('--transport', 'direct,ziti', '--atrium-identity', $HubZitiIdentity, '--atrium-service', 'atrium-hub')
  $HubArgs += @('--board-transport', 'ziti', '--board-service', 'atrium-board')
  $HubArgs += @('--ci-hook-service', 'ziti-hook')
}
$RoomArgs = @('room', '--dir', $RoomDir, '--db', $RoomDb, '--http', '127.0.0.1:7781', '--agent', '127.0.0.1:7777')

# $WhatIf is set by the script that dot-sources this, from its own -WhatIf switch.
if (-not (Test-Path variable:WhatIf)) { $WhatIf = $false }

# Say logs a line and shows it. To the host, not the pipeline, so a function that says something still returns
# only what it returns.
function Say($m) {
  $line = "$(Get-Date -Format o)  $LiveTag $m"
  if ($WhatIf) { Write-Host $line; return }
  $line | Tee-Object -FilePath $LiveLog -Append | Write-Host
}

# Wait-NewContextsDone holds a deploy while any card has a new context under way, the way the restart gate holds it
# for a busy board. A restart cuts a clear off between its steps and leaves the card half cycled. Names each card
# while it waits. Returns $true when none is left, $false when -MaxSeconds ran out, and $true when the hub does not
# answer, since a hub that is down is not running a clear and a deploy held forever by it is worse.
$HubNewContexts = 'http://127.0.0.1:7778/_hub/new-contexts'
function Wait-NewContextsDone([int]$MaxSeconds = 1200) {
  $deadline = (Get-Date).AddSeconds($MaxSeconds)
  $said = ''
  while ($true) {
    try { $r = Invoke-RestMethod -Uri $HubNewContexts -TimeoutSec 15 } catch { return $true }
    $runs = @($r.under_way)
    if ($runs.Count -eq 0) { return $true }
    $names = ($runs | ForEach-Object { "$($_.name)@$($_.room) step $($_.n) of 3 ($($_.step))" }) -join ', '
    if ($names -ne $said) { Say "waiting for a new context to finish on: $names"; $said = $names }
    if ((Get-Date) -gt $deadline) { return $false }
    Start-Sleep -Seconds 5
  }
}

# Set-DeployHold sets the room's deploy hold, so every card's next gated call is refused with "end your turn and
# wait", and the room that comes back lifts it and types a continue line into each card that was working. A card idle
# at the time is left alone. Returns $true when the hold is on (or was already), $false when it could not be set.
# See docs/rnd/resume-says-continue-design.md.
$RoomHoldUrl = 'http://127.0.0.1:7781/v1/hold'
function Set-DeployHold([string]$Why = 'deploy-batch') {
  $body = @{ action = 'start'; by = 'deploy-batch'; whys = @($Why) } | ConvertTo-Json -Compress
  try {
    $null = Invoke-RestMethod -Method Post -Uri $RoomHoldUrl -ContentType 'application/json' -Body $body -TimeoutSec 15
    return $true
  } catch {
    if ($_.Exception.Response -and [int]$_.Exception.Response.StatusCode -eq 409) { Say 'the room is already held for a deploy'; return $true }
    Say "could not set the deploy hold: $($_.Exception.Message)"
    return $false
  }
}

# Test-RunnersSurvive says whether a stop of this room leaves every runner running, so a deploy hold has nothing to
# protect. It reads GET /v1/preflight: the pty host is on, it would not die with the room's task (in_job false), and no
# runner lives in the room's own process (one started before the setting went on does, and ends with it). Any doubt,
# an old room with no such route included, answers $false and the hold is kept.
# See docs/rnd/next-room-last-room.md, phase 2.
$RoomPreflightUrl = 'http://127.0.0.1:7781/v1/preflight'
function Test-RunnersSurvive([string]$Url = $RoomPreflightUrl, [hashtable]$Headers = @{}) {
  try { $r = Invoke-RestMethod $Url -Headers $Headers -TimeoutSec 10 } catch { return $false }
  return [bool]($r.pty_host -and $r.pty_host.on -and -not $r.pty_host.in_job -and $r.runners -and $r.runners.in_process -eq 0)
}

# Test-StopWouldRefuse says whether the room's plain shutdown would answer 409, which is what Stop-Room reports as
# REFUSED: the pty host is in the room task's job, so it would die with the room, and at least one runner lives in it.
# Asked before any hold is set, so a deploy that is going to refuse never holds the room. -KillRunners is not refused.
# An unreadable preflight answers $false: the stop itself still decides.
function Test-StopWouldRefuse([string]$Url = $RoomPreflightUrl, [hashtable]$Headers = @{}) {
  if ($KillRunners) { return $false }
  try { $r = Invoke-RestMethod $Url -Headers $Headers -TimeoutSec 10 } catch { return $false }
  return [bool]($r.pty_host -and $r.pty_host.on -and $r.pty_host.in_job -and $r.runners -and $r.runners.host -gt 0)
}

# Get-HostInJobLines is what a refused stop says, from a /v1/preflight body: the cause, the runner counts, and what to do.
# The cause is the room's own words when it gave them (it names the process that started it), else the plain one.
function Get-HostInJobLines($pf) {
  $cause = 'the pty host is inside a job object that refused breakaway, inherited from the process that started the room'
  if ($pf.pty_host -and $pf.pty_host.in_job_cause) { $cause = $pf.pty_host.in_job_cause }
  $held = 'unknown'; $local = 'unknown'
  if ($pf.runners) { $held = $pf.runners.host; $local = $pf.runners.in_process }
  @(
    "REFUSED: $cause, so this stop would end every runner and lose any text typed and not sent. nothing was stopped.",
    "  runners held by the pty host: $held, in the room's own process: $local. typed text is not exposed, so ask whoever is at the terminals.",
    '  to fix it for good: restart the room ONCE from a jobless start (start-atrium-room.ps1 from a shell atrium does not supervise), which ends the runners once and leaves the next pty host outside any job.',
    '  to end the runners now and restart anyway, run again with -KillRunners'
  )
}

# Wait-HoldQuiet waits for every held card to end its turn, the way `atrium_deploy wait` does, up to -MaxSeconds.
# Names the cards still working while it waits. Returns $true when the room is quiet, $false when the bound ran out.
function Wait-HoldQuiet([int]$MaxSeconds = 300) {
  $deadline = (Get-Date).AddSeconds($MaxSeconds)
  $said = ''
  while ($true) {
    try { $r = Invoke-RestMethod -Uri $RoomHoldUrl -TimeoutSec 15 } catch { Say "could not read the hold: $($_.Exception.Message)"; return $false }
    $busy = @($r.busy)
    if ($busy.Count -eq 0) { return $true }
    $names = ($busy | ForEach-Object { $_.title }) -join ', '
    if ($names -ne $said) { Say "waiting for held cards to end their turn: $names"; $said = $names }
    if ((Get-Date) -gt $deadline) { Say "gave up waiting after $MaxSeconds seconds, still working: $names"; return $false }
    Start-Sleep -Seconds 5
  }
}

# Invoke-Step runs one step that changes something, or under -WhatIf says what it would do and changes nothing.
function Invoke-Step([string]$What, [scriptblock]$Do) {
  if ($WhatIf) { Say "WHATIF: $What"; return }
  Say $What
  & $Do
}

# Find-Atrium finds the atrium (`run`) or the room (`room`) by image directory and subcommand.
#
# THE DIRECTORY, NOT THE FILE. A hub-only deploy renames the file the room is running to atrium.old-<ts>.exe, and
# what Windows reports as that process's image path is not something to bet a second room on. Every image in
# .atrium\bin named atrium*.exe counts, and the subcommand picks which. atrium-control.exe runs ` control`, which
# matches neither.
function Find-Atrium([ValidateSet('run', 'room')][string]$Sub) {
  Get-CimInstance Win32_Process |
    Where-Object {
      $_.ExecutablePath -and
      ((Split-Path $_.ExecutablePath) -eq $AtriumBinDir) -and
      ((Split-Path $_.ExecutablePath -Leaf) -like 'atrium*.exe') -and
      ($_.CommandLine -match " $Sub( |$)")
    }
}

# Find-Control finds the sg4-control room by its own binary directory and the `room` subcommand.
function Find-Control {
  Get-CimInstance Win32_Process |
    Where-Object {
      $_.ExecutablePath -and
      ((Split-Path $_.ExecutablePath) -eq $CtlBinDir) -and
      ($_.CommandLine -match ' room( |$)')
    }
}

# Clear-SessionEnv drops what a supervised session exports, so the processes started here do not inherit the
# session that ran the script. ATRIUM_LOCATION above all: a room keeps an inherited one, and would write its address
# to that file instead of the machine's one place.
function Clear-SessionEnv {
  foreach ($n in 'ATRIUM_LOCATION', 'ATRIUM_SHARED_LOCATION', 'ATRIUM_AGENT_NAME', 'ATRIUM_TASK_ID', 'ATRIUM_ROOM',
    'ATRIUM_RUNNER', 'ATRIUM_HOOK_EXE') {
    Remove-Item "Env:$n" -ErrorAction SilentlyContinue
  }
}

function Test-Up([string]$Url) {
  try { Invoke-RestMethod $Url -TimeoutSec 3 | Out-Null; return $true } catch { return $false }
}

# Wait-Hub waits for the atrium's health, and with -Rooms for at least that many attached rooms.
function Wait-Hub([int]$Seconds = 25, [int]$Rooms = 0) {
  $dl = (Get-Date).AddSeconds($Seconds)
  while ((Get-Date) -lt $dl) {
    try {
      $h = Invoke-RestMethod $HubHealth -TimeoutSec 3
      if ($h.ok -and $h.rooms -ge $Rooms) { return $h }
    } catch {}
    Start-Sleep -Milliseconds 600
  }
  return $null
}

# Get-BinLabel names a binary by what the FILE says it is, from `atrium version`: its commit (7 characters) and its
# board hash (8). Returns '' when the file cannot answer: it will not run, it is an old build with no `version`, or it
# reports no board hash. Never ask the hub. The hub reports the build it RUNS, and the file on disk can be newer when
# the room was deployed after the hub.
function Get-BinLabel([string]$Path) {
  $commit = ''; $board = ''
  try {
    foreach ($line in (& $Path version 2>$null)) {
      if ($line -match '^commit\s+([0-9a-f]{7,})') { $commit = $Matches[1].Substring(0, 7) }
      elseif ($line -match '^board\s+([0-9a-f]{8,})') { $board = $Matches[1].Substring(0, 8) }
    }
  } catch { return '' }
  if (-not $board) { return '' }
  if ($commit) { return "$commit-$board" }
  return $board
}

# Save-Revert keeps one copy of the outgoing binary under its own identity, for going back. A file that cannot say
# what it is gets unknown-<timestamp> and a warning, since a wrong name is worse than a plain one and a snapshot that
# cannot be labelled must not fail the deploy.
function Save-Revert {
  $label = Get-BinLabel $AtriumBin
  if (-not $label) {
    $label = "unknown-$(Get-Date -Format yyyyMMddHHmmss)"
    Say "WARNING: $AtriumBin did not answer 'atrium version'. revert snapshot is named $label"
  }
  $revert = Join-Path $AtriumBinDir "atrium.revert-$label.exe"
  Invoke-Step "copy $AtriumBin -> $revert (revert snapshot, $label)" { Copy-Item $AtriumBin $revert -Force }
  Get-ChildItem (Join-Path $AtriumBinDir 'atrium.revert-*.exe') | Where-Object FullName -ne $revert |
    ForEach-Object { $f = $_.FullName; Invoke-Step "remove old revert $f" { Remove-Item $f -Force } }
  return $revert
}

# Test-NewBuildStamped refuses a build that cannot name its commit. An unstamped build reports only its board hash,
# which does not change for a Go-only fix, so the deploy log and the next revert snapshot could not tell it from the
# build it replaced. That is how the snapshot taken on 2026-09-29 09:36 held the deadlocked build.
function Test-NewBuildStamped([string]$Path = $AtriumNew) {
  $label = Get-BinLabel $Path
  if ($label -notmatch '^[0-9a-f]{7}-') {
    Say "FATAL: $Path does not report a commit ('$label'). build it with -ldflags -X ...cli.Commit=<sha>"
    return $false
  }
  # A MODIFIED BUILD IS NOT ITS COMMIT, so the label above would name code that was never committed. Build with
  # build-deploy.ps1, which refuses anything but a clean claude/main and stamps the tree state. A build from before
  # that stamp existed counts untracked files and says modified regardless: rebuild it.
  $commitLine = @(& $Path version 2>$null) | Where-Object { $_ -match '^commit\s' } | Select-Object -First 1
  if ($commitLine -match '\(modified\)') {
    Say "FATAL: $Path says '$commitLine'. it carries uncommitted code, or predates the tree stamp and counted untracked files. build it with build-deploy.ps1"
    return $false
  }
  Say "new build is $label"
  return $true
}

# Get-RoomLink reads what the room says about its own link, from the last [link] attach or failure line in room.err.
# Returns 'up', 'down' or '' when it has said neither yet. The room has no endpoint for this: /v1/settings and
# /v1/health answer from the room itself whether or not the hub has it.
function Get-RoomLink {
  $err = Join-Path $Base 'room.err'
  if (-not (Test-Path $err)) { return '' }
  $last = Select-String -Path $err -Pattern '\[link\] (attached to hub|hub \S+: )' | Select-Object -Last 1
  if (-not $last) { return '' }
  if ($last.Line -match '\[link\] attached to hub') { return 'up' }
  return 'down'
}

# Get-HubRoom is this room's entry in the hub's own connection list, or $null.
function Get-HubRoom {
  try {
    (Invoke-RestMethod $HubRooms -TimeoutSec 3).rooms | Where-Object { $_.name -eq $RoomName } | Select-Object -First 1
  } catch { $null }
}

# How long a room must stay attached, and how long before its second health check. One constant for both.
$StaySeconds = 10

# How long a deploy waits for a room to reattach to the hub. A ziti reattach after a hub restart took about five minutes
# (sg3, 2026-10-15), so this accepts minutes and a verdict of failed means this budget ran out. The one place it is named.
$ReattachBudget = 600
$ReattachSayEvery = 15

# How long a swap waits for the old room to let go of its store (atrium.db) after `atrium stop`. The port check passes
# while the process still holds the file, and the new room then exits "store held by another process" (sgg).
$StoreBudget = 120

# New-StoreWaitScript builds the remote snippet that waits for the old room to let go of its store, which is what
# matters, not the board port. $Kind is 'windows' (PowerShell) or 'mac' (zsh). $DbExpr is the db path as that shell
# writes it. It names what it waits on, and past $Budget seconds prints $StoreTimeout with the pid still holding the
# store and exits 4. Windows: the db opens exclusively. Mac: lsof lists no holder.
$StoreTimeout = 'STORE-WAIT-TIMEOUT'
function New-StoreWaitScript([string]$Kind, [string]$DbExpr, [int]$Budget = $StoreBudget) {
  if ($Kind -eq 'mac') {
    # No double quotes, $ or backticks: the Swap this lands in is itself inside zsh -lic "...", which the ssh shell
    # would expand first. $DbExpr is a plain path, ~ allowed.
    return 'echo ''waiting for the old room to release ' + $DbExpr + ' (up to ' + $Budget + 's)'' ; ' +
      'for i in {1..' + $Budget + '}; do lsof ' + $DbExpr + ' >/dev/null 2>&1 || break ; sleep 1 ; done ; ' +
      'if lsof ' + $DbExpr + ' >/dev/null 2>&1 ; then echo -n ''' + $StoreTimeout + ' '' ; ' +
      'lsof -t ' + $DbExpr + ' | tr ''\n'' '' '' ; echo ; exit 4 ; fi ; '
  }
  return '$db = ' + $DbExpr + ' ; $held = $true ; $n = 0 ; ' +
    '$pids = @(Get-CimInstance Win32_Process -Filter "Name like ''atrium%''" -EA SilentlyContinue | ' +
    'Where-Object { $_.CommandLine -match '' room'' } | ForEach-Object { $_.ProcessId }) ; ' +
    '"waiting for the old room (pid $($pids -join '','')) to release $db (up to ' + $Budget + 's)" ; ' +
    'while ($n -lt ' + $Budget + ') { ' +
    'if (-not (Test-Path -LiteralPath $db)) { $held = $false ; break } ; ' +
    'try { $f = [IO.File]::Open($db, ''Open'', ''ReadWrite'', ''None'') ; $f.Dispose() ; $held = $false ; break } catch {} ; ' +
    'Start-Sleep 1 ; $n++ } ; ' +
    'if ($held) { $still = @($pids | Where-Object { Get-Process -Id $_ -EA SilentlyContinue }) ; ' +
    '''' + $StoreTimeout + ' pid '' + ($still -join '','') ; exit 4 } ; '
}

# Wait-RoomAttached waits for THIS room to be attached, by name, on a connection made after $After, as the hub sees it
# AND as the room sees it, and then for it to STAY that way for $Hold seconds on the same connection. A five-second
# attach that the hub then refused passed the old check at 17:05:43 on 2026-09-29. Returns the hub's entry or $null.
function Wait-RoomAttached([datetime]$After, [int]$Seconds = 150, [int]$Hold = $StaySeconds) {
  $t0 = Get-Date
  $dl = $t0.AddSeconds($Seconds)
  $afterUtc = $After.ToUniversalTime()
  $nextSay = $t0
  while ((Get-Date) -lt $dl) {
    # Polling the hub is silent, and a ziti reattach takes minutes, so say where the wait stands every $ReattachSayEvery s.
    if ((Get-Date) -ge $nextSay) {
      Say "waiting for $RoomName to reattach ($([int]((Get-Date) - $t0).TotalSeconds)s of ${Seconds}s)"
      $nextSay = (Get-Date).AddSeconds($ReattachSayEvery)
    }
    $r = Get-HubRoom
    if ($r -and ([datetime]$r.since).ToUniversalTime() -ge $afterUtc -and (Get-RoomLink) -eq 'up') {
      $since = $r.since
      Say "room $RoomName attached since $since. holding ${Hold}s to see it stay"
      $held = $true
      $end = (Get-Date).AddSeconds($Hold)
      while ((Get-Date) -lt $end) {
        Start-Sleep -Seconds 5
        $now = Get-HubRoom
        $link = Get-RoomLink
        if (-not $now -or $now.since -ne $since -or $link -ne 'up') {
          Say "room $RoomName did not stay attached (hub: $(if ($now) { $now.since } else { 'gone' }), room link: $link)"
          $held = $false
          break
        }
      }
      if ($held) { Say "$RoomName reattached after $([int]((Get-Date) - $t0).TotalSeconds)s"; return $r }
    }
    Start-Sleep -Seconds 2
  }
  return $null
}

# Test-RoomServes asks the room for something it reads from its store, $Tries times over a few seconds.
function Test-RoomServes([int]$Tries = 3) {
  for ($i = 0; $i -lt $Tries; $i++) {
    try { Invoke-RestMethod $RoomServes -TimeoutSec 5 | Out-Null; return $true } catch { Start-Sleep -Seconds 1 }
  }
  return $false
}

# Install-Control puts the same build beside atrium.exe as atrium-control.exe, the copy `atrium control` runs from. An
# MCP client holds it open for as long as a session lives, so it is swapped as atrium.exe is: stage, rename the old one
# aside, rename the new one in. Best effort, because a deploy that already put the room's binary in place must not fail
# on it, and the control server says itself when it is older than its room. Returns $true when it is in place.
function Install-Control([string]$From = $AtriumNew) {
  $ctl = Join-Path $AtriumBinDir 'atrium-control.exe'
  $next = Join-Path $AtriumBinDir 'atrium-control.next.exe'
  $aside = Join-Path $AtriumBinDir "atrium-control.old-$(Get-Date -Format yyyyMMddHHmmss).exe"
  if (-not (Test-Path $From)) { Say "WARNING: no build at $From for atrium-control.exe"; return $false }
  Invoke-Step "copy $From -> $next (staged, nothing has it open)" { Copy-Item $From $next -Force }
  if ($WhatIf) {
    Say "WHATIF: rename $ctl -> $aside"
    Say "WHATIF: rename $next -> $ctl"
    return $true
  }
  if (Test-Path $ctl) {
    $moved = $false
    for ($i = 0; $i -lt 20 -and -not $moved; $i++) {
      try { Rename-Item $ctl $aside -ErrorAction Stop; $moved = $true } catch { Start-Sleep -Milliseconds 400 }
    }
    if (-not $moved) {
      Remove-Item $next -Force -ErrorAction SilentlyContinue
      Say "WARNING: could not rename $ctl aside. atrium-control.exe is left as it was"
      return $false
    }
  }
  $placed = $false
  for ($i = 0; $i -lt 20 -and -not $placed; $i++) {
    try { Rename-Item $next (Split-Path $ctl -Leaf) -ErrorAction Stop; $placed = $true }
    catch { Start-Sleep -Milliseconds 400 }
  }
  if (-not $placed) {
    if (Test-Path $aside) { Rename-Item $aside (Split-Path $ctl -Leaf) -ErrorAction SilentlyContinue }
    Say "WARNING: could not rename $next into place. the old atrium-control.exe is back"
    return $false
  }
  Say "renamed $ctl -> $aside, and $next -> $ctl"
  Get-ChildItem (Join-Path $AtriumBinDir 'atrium-control.old-*.exe') | Sort-Object LastWriteTime -Descending |
    Select-Object -Skip 3 | ForEach-Object { Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue }
  return $true
}

# Install-Atrium stages the new build and swaps it in with two renames. Returns $true when it is in place.
#
# Retries each rename, because a hook starting at that instant can hold the name for a moment. The file moved aside is
# left for the prune below: a running room or a hook may still be executing it, which a rename permits and a delete
# would not.
function Install-Atrium([string]$From = $AtriumNew) {
  if (-not (Test-Path $From)) { Say "FATAL: no new build at $From"; return $false }
  $next = Join-Path $AtriumBinDir 'atrium.next.exe'
  $aside = Join-Path $AtriumBinDir "atrium.old-$(Get-Date -Format yyyyMMddHHmmss).exe"
  Invoke-Step "copy $From -> $next (staged, nothing has it open)" { Copy-Item $From $next -Force }
  if ($WhatIf) {
    Say "WHATIF: rename $AtriumBin -> $aside"
    Say "WHATIF: rename $next -> $AtriumBin"
    return $true
  }
  $moved = $false
  for ($i = 0; $i -lt 20 -and -not $moved; $i++) {
    try { Rename-Item $AtriumBin $aside -ErrorAction Stop; $moved = $true } catch { Start-Sleep -Milliseconds 400 }
  }
  if (-not $moved) { Say "FATAL: could not rename $AtriumBin aside. nothing changed"; return $false }
  $placed = $false
  for ($i = 0; $i -lt 20 -and -not $placed; $i++) {
    try { Rename-Item $next (Split-Path $AtriumBin -Leaf) -ErrorAction Stop; $placed = $true }
    catch { Start-Sleep -Milliseconds 400 }
  }
  if (-not $placed) {
    # Put the old one back. No file at the hook path is an outage for every hook on the machine.
    Rename-Item $aside (Split-Path $AtriumBin -Leaf) -ErrorAction SilentlyContinue
    Say "FATAL: could not rename $next into place. the old binary is back"
    return $false
  }
  Say "renamed $AtriumBin -> $aside, and $next -> $AtriumBin"
  # Best effort: an aside file something still runs cannot be deleted, and is left for next time.
  Get-ChildItem (Join-Path $AtriumBinDir 'atrium.old-*.exe') | Sort-Object LastWriteTime -Descending |
    Select-Object -Skip 3 | ForEach-Object { Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue }
  $null = Install-Control $From
  return $true
}

# Stop-Room asks the room to wind down so it parks its runners and saves what to reopen, and forces only after the wait.
# The room first asks each working card to wrap up and queues a wake on it (r-graceful-room-restart), bounded by its
# restart_wrap_wait_s setting, so the force waits that long plus 45s. -Immediate on the calling script ($Immediate)
# posts ?now=1 and skips the wrap-up.
function Get-WrapWait {
  try { return [int](Invoke-RestMethod $RoomServes -TimeoutSec 5).restart_wrap_wait_s } catch { return 300 }
}
#
# A 409 from the room means the pty host would die with the room's task, so this stop would end every runner and
# whatever is typed and unsent in them. That is never done on its own: Stop-Room says so, changes nothing and returns
# $false, and the caller stops. Only -KillRunners on the calling script ($KillRunners) posts {"runners":true}.
function Invoke-ShutdownPost([string]$Uri, [string]$Body) {
  if ($Body) { Invoke-RestMethod $Uri -Method Post -ContentType 'application/json' -Body $Body -TimeoutSec 8 | Out-Null }
  else { Invoke-RestMethod $Uri -Method Post -TimeoutSec 8 | Out-Null }
}
function Get-ShutdownStatus($err) {
  if ($err.Exception.Response) { return [int]$err.Exception.Response.StatusCode }
  return 0
}
function Stop-Room {
  $room = Find-Atrium room
  if (-not $room) { Say 'no running room found'; return $true }
  $post = $RoomShutdown
  $wait = 45
  if ($Immediate) { $post = "$RoomShutdown`?now=1" } else { $wait = 45 + (Get-WrapWait) }
  $script:StopRoomRefused = $false
  foreach ($p in $room) {
    Invoke-Step "stop room pid $($p.ProcessId): POST $post, wait up to ${wait}s, then force" {
      try { Invoke-ShutdownPost $post '' }
      catch {
        Say "room shutdown post: $($_.Exception.Message)"
        if ((Get-ShutdownStatus $_) -eq 409) {
          if ($KillRunners) {
            Say 'the pty host would die with the room. -KillRunners was passed, stopping with --runners'
            try { Invoke-ShutdownPost $post '{"runners":true}' }
            catch { Say "room shutdown post: $($_.Exception.Message)" }
          } else {
            $script:StopRoomRefused = $true
            try {
              $pf = Invoke-RestMethod $RoomPreflightUrl -TimeoutSec 5
            } catch {
              $pf = $null
              Say "could not read $RoomPreflightUrl for the runner counts: $($_.Exception.Message)"
            }
            foreach ($line in (Get-HostInJobLines $pf)) { Say $line }
          }
        }
      }
      if ($script:StopRoomRefused) { return }
      try { Wait-Process -Id $p.ProcessId -Timeout $wait -ErrorAction Stop } catch {}
      if (Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue) {
        Say "room still up after ${wait}s, force stop"
        Stop-Process -Id $p.ProcessId -Force
      }
    }
    if ($script:StopRoomRefused) { return $false }
  }
  return $true
}

# Stop-Control winds the sg4-control room down the same way Stop-Room does claude-sg4.
function Stop-Control {
  $ctl = Find-Control
  if (-not $ctl) { Say 'no running sg4-control room found'; return }
  foreach ($p in $ctl) {
    Invoke-Step "stop sg4-control pid $($p.ProcessId): POST $CtlShutdown, wait up to 45s, then force" {
      try { Invoke-RestMethod $CtlShutdown -Method Post -TimeoutSec 8 | Out-Null }
      catch { Say "sg4-control shutdown post: $($_.Exception.Message)" }
      try { Wait-Process -Id $p.ProcessId -Timeout 45 -ErrorAction Stop } catch {}
      if (Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue) {
        Say 'sg4-control still up after 45s, force stop'
        Stop-Process -Id $p.ProcessId -Force
      }
    }
  }
}

# Stop-Hub stops the atrium. It holds nothing, so a hard stop is fine.
function Stop-Hub {
  $hub = Find-Atrium run
  if (-not $hub) { Say 'no running hub found'; return }
  foreach ($p in $hub) {
    Invoke-Step "stop hub pid $($p.ProcessId)" {
      Stop-Process -Id $p.ProcessId -Force
      try { Wait-Process -Id $p.ProcessId -Timeout 10 -ErrorAction Stop } catch {}
    }
  }
}

# Backup keeps the previous run's log, since a redirect overwrites it.
function Backup([string]$Name) {
  $p = Join-Path $Base $Name
  if (Test-Path $p) {
    Invoke-Step "keep $p as $Name.<timestamp>" {
      Copy-Item $p (Join-Path $Base "$Name.$(Get-Date -Format yyyyMMdd-HHmmss)") -Force
    }
  }
}

# Get-LiveHosts reads ATRIUM_HOSTS for a hub or room the deploy starts: the User value, else the Machine one, never the
# caller's. It returns the value ($null when unset at both) and the scope it came from. -Read is a seam for the test.
function Get-LiveHosts([scriptblock]$Read) {
  if (-not $Read) { $Read = { param($scope) [Environment]::GetEnvironmentVariable('ATRIUM_HOSTS', $scope) } }
  foreach ($scope in 'User', 'Machine') {
    $v = & $Read $scope
    if ($v) { return [pscustomobject]@{ Value = $v; Scope = $scope } }
  }
  return [pscustomobject]@{ Value = $null; Scope = $null }
}

# Get-LiveHostsSaid is the line the deploy log and hub.err carry for what Get-LiveHosts found.
function Get-LiveHostsSaid($h) {
  if ($h.Value) { return "ATRIUM_HOSTS=$($h.Value), from the $($h.Scope) environment" }
  return 'ATRIUM_HOSTS unset in the User and Machine environments'
}

# Set-LiveHosts applies Get-LiveHosts to this process, which a child inherits.
function Set-LiveHosts($h) {
  if ($h.Value) { $env:ATRIUM_HOSTS = $h.Value } else { Remove-Item Env:ATRIUM_HOSTS -ErrorAction SilentlyContinue }
}

# Initialize-LiveSpawn loads the Win32 calls the starts and the job checks below make.
function Initialize-LiveSpawn {
  if (-not ('AtriumLive.Spawn' -as [type])) {
    Add-Type -Namespace AtriumLive -Name Spawn -MemberDefinition @'
[StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
public struct STARTUPINFO { public int cb; public string lpReserved, lpDesktop, lpTitle; public int dwX, dwY, dwXSize,
  dwYSize, dwXCountChars, dwYCountChars, dwFillAttribute, dwFlags; public short wShowWindow, cbReserved2;
  public System.IntPtr lpReserved2, hStdInput, hStdOutput, hStdError; }
[StructLayout(LayoutKind.Sequential)]
public struct PROCESS_INFORMATION { public System.IntPtr hProcess, hThread; public int dwProcessId, dwThreadId; }
[DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
public static extern bool CreateProcessW(string app, System.Text.StringBuilder cmd, System.IntPtr pa, System.IntPtr ta,
  bool inherit, uint flags, System.IntPtr env, string cwd, ref STARTUPINFO si, out PROCESS_INFORMATION pi);
[DllImport("kernel32.dll")] public static extern bool CloseHandle(System.IntPtr h);
[DllImport("kernel32.dll", SetLastError = true)]
public static extern System.IntPtr OpenProcess(uint access, bool inherit, int pid);
[DllImport("kernel32.dll", SetLastError = true)]
public static extern bool IsProcessInJob(System.IntPtr process, System.IntPtr job, out bool result);
'@
  }
}

# Start-Breakaway starts a command line OUTSIDE any job object this process is in: CREATE_BREAKAWAY_FROM_JOB, no
# window, its own process group, this process's environment. Start-Process cannot do it, so a hub started that way
# belonged to the job of the room whose shell ran the script, and died when that room was stopped (2026-10-14).
# Returns the new pid. Throws when the job does not allow breakaway, rather than start something that would not
# outlive it.
function Start-Breakaway([string]$CommandLine) {
  Initialize-LiveSpawn
  $si = New-Object AtriumLive.Spawn+STARTUPINFO
  $si.cb = [Runtime.InteropServices.Marshal]::SizeOf($si)
  $pi = New-Object AtriumLive.Spawn+PROCESS_INFORMATION
  # CREATE_BREAKAWAY_FROM_JOB | CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW
  $flags = 0x01000000 -bor 0x00000200 -bor 0x08000000
  $sb = New-Object Text.StringBuilder $CommandLine
  if (-not [AtriumLive.Spawn]::CreateProcessW([NullString]::Value, $sb, [IntPtr]::Zero, [IntPtr]::Zero, $false, $flags, [IntPtr]::Zero,
      [NullString]::Value, [ref]$si, [ref]$pi)) {
    throw "could not start outside the job object, Win32 error $([Runtime.InteropServices.Marshal]::GetLastWin32Error()): $CommandLine"
  }
  [AtriumLive.Spawn]::CloseHandle($pi.hProcess) | Out-Null
  [AtriumLive.Spawn]::CloseHandle($pi.hThread) | Out-Null
  return $pi.dwProcessId
}

# Test-ProcessInJob says whether a process is inside ANY job object, which is what ending the terminal tab that started it
# ends. $null when it cannot be asked. CREATE_BREAKAWAY_FROM_JOB leaves only the innermost job, so a process it started
# can still be in the one above (2026-10-20: the room and its pty host, started from a Windows Terminal tab).
function Test-ProcessInJob([int]$ProcId) {
  Initialize-LiveSpawn
  $h = [AtriumLive.Spawn]::OpenProcess(0x1000, $false, $ProcId)
  if ($h -eq [IntPtr]::Zero) { return $null }
  try {
    $in = $false
    if ([AtriumLive.Spawn]::IsProcessInJob($h, [IntPtr]::Zero, [ref]$in)) { return $in }
    return $null
  } finally { [AtriumLive.Spawn]::CloseHandle($h) | Out-Null }
}

# Get-StartRoute picks how a detached process is started. A caller already inside a job goes straight to WMI, the one
# start that is in no job of ours: CreateProcess with breakaway only escapes the innermost job. Outside any job the
# cheaper breakaway start is enough.
function Get-StartRoute($InJob) { if ($InJob -or $null -eq $InJob) { 'wmi' } else { 'breakaway' } }

# Start-Wmi starts a command line through Win32_Process Create: WmiPrvSE is its parent, so it is in no job of ours and
# survives the terminal that ran the script. Hidden. Returns the pid, throws when WMI refuses.
function Start-Wmi([string]$CommandLine) {
  $si = New-CimInstance -ClassName Win32_ProcessStartup -ClientOnly -Property @{ ShowWindow = [uint16]0 }
  $r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $CommandLine; ProcessStartupInformation = $si }
  if ($r.ReturnValue -ne 0) { throw "Win32_Process Create returned $($r.ReturnValue): $CommandLine" }
  return [int]$r.ProcessId
}

# New-LaunchScript writes the .cmd a WMI start runs: WMI hands the child no copy of this process's environment, so the
# ATRIUM_* variables it needs (ATRIUM_HOSTS, ATRIUM_DEBUG_INPUTLAG, ATRIUM_PRIORITY) are set in the script, and nothing
# else is, so no credential in this shell is written to disk. A value that cannot sit inside set "..." is skipped.
function New-LaunchScript([string]$Path, [string]$Command) {
  $lines = @('@echo off')
  foreach ($e in (Get-ChildItem Env: | Where-Object { $_.Name -like 'ATRIUM_*' })) {
    if ($e.Value -match '["\r\n]') { continue }
    $lines += ('set "{0}={1}"' -f $e.Name, ($e.Value -replace '%', '%%'))
  }
  $lines += $Command
  Set-Content -Path $Path -Value $lines -Encoding ASCII
}

# Start-Jobless starts "<exe> args > out 2> err" with no job object over it, by the route Get-StartRoute picks, and says
# when the result is still in one. Returns the pid of the cmd that runs it. Throws only when neither route could start it.
function Start-Jobless([string]$Command, [string]$Tag) {
  $route = Get-StartRoute (Test-ProcessInJob $PID)
  $procId = $null
  if ($route -eq 'wmi') {
    try {
      $launch = Join-Path $Base "launch-$Tag.cmd"
      New-LaunchScript $launch $Command
      $procId = Start-Wmi "cmd.exe /c `"$launch`""
    } catch {
      Say "WARNING: could not start $Tag through WMI ($($_.Exception.Message)). falling back to breakaway, which leaves it in any outer job"
    }
  }
  if ($null -eq $procId) { $procId = Start-Breakaway "cmd.exe /c `"$Command`"" }
  $in = Test-ProcessInJob $procId
  if ($in) {
    Say "WARNING: $Tag (pid $procId) is inside a job object that refused breakaway, inherited from the terminal or task that ran this script. closing that terminal can end it and its pty host. run this from a shell started outside any job (a service or scheduled task) to avoid that"
  } elseif ($null -eq $in) {
    Say "$Tag started as pid $procId (route $route). whether it is in a job could not be read"
  } else {
    Say "$Tag started as pid $procId (route $route), in no job object"
  }
  return $procId
}

# Start-Hub starts the atrium detached, through cmd so the redirect appends and the caller's output can end.
#
# ATRIUM_HOSTS FROM THE USER ENVIRONMENT (Machine when User is unset), never the caller's. It names the extra
# hosts the board answers to, the zrok share's among them, and a deploy started from a session that lacks it
# brought the hub up without it (2026-09-30 19:09, the share broke). Unset in both environments means unset for the
# hub too. The value used is logged here and in hub.err. @runtime's r-new-hosts-setting, a stored setting, replaces
# this.
function Start-Hub {
  $line = "`"$AtriumBin`" $($HubArgs -join ' ') >> $Base\hub.out 2>> $Base\hub.err"
  $h = Get-LiveHosts
  $said = Get-LiveHostsSaid $h
  Say $said
  Invoke-Step "start hub: $AtriumBin $($HubArgs -join ' ')" {
    Clear-SessionEnv
    Set-LiveHosts $h
    Add-Content (Join-Path $Base 'hub.err') ("===== hub start {0} ({1}) =====" -f (Get-Date -Format o), $said)
    $null = Start-Jobless $line 'hub'
  }
}

# Start-Room starts the room detached, and refuses when one already answers: two rooms on one database is the one
# thing the store is not built for.
function Start-Room {
  # Under -WhatIf nothing was stopped, so the live room answering says nothing about the real run.
  if (-not $WhatIf -and (Test-Up $RoomHealth)) {
    Say "a room already answers at $RoomHealth. not starting another"
    return $false
  }
  $h = Get-LiveHosts
  Say (Get-LiveHostsSaid $h)
  Invoke-Step "start room: $AtriumBin $($RoomArgs -join ' ')" {
    Clear-SessionEnv
    Set-LiveHosts $h
    # Through cmd for the redirects, and in no job object, so stopping a room never takes this one with it and closing the terminal never takes the room.
    $null = Start-Jobless "`"$AtriumBin`" $($RoomArgs -join ' ') > $Base\room.out 2> $Base\room.err" 'room'
  }
  return $true
}

# Write-DeployQueue prints the hub's deploy queue, the landed commits not yet live and which deploy each needs, and saves
# it as DEPLOY-QUEUE.md under $Base for HANDOFF to carry. Read BEFORE the deploy, while the hub still reports what is
# live. Never stops a deploy: a hub that does not answer is said and the deploy goes on.
$HubDeployQueue = 'http://127.0.0.1:7778/_hub/deploy-queue?format=md'
function Write-DeployQueue {
  try { $md = (Invoke-WebRequest -Uri $HubDeployQueue -TimeoutSec 20 -UseBasicParsing).Content }
  catch { Say "deploy queue not read: $($_.Exception.Message)"; return }
  if ($md -is [byte[]]) { $md = [Text.Encoding]::UTF8.GetString($md) }
  $out = Join-Path $Base 'DEPLOY-QUEUE.md'
  if (-not $WhatIf) { Set-Content -Path $out -Value $md -Encoding utf8 }
  foreach ($l in ($md -split "`r?`n")) { if ($l.Trim()) { Say "queue: $l" } }
  Say "deploy queue saved to $out for HANDOFF"
}

# Invoke-Capped runs one process with a wall-clock cap and returns @{ Code; Out; TimedOut }. ssh's ConnectTimeout bounds
# only the connect: a session that connects and then stalls has no bound (2026-10-14: an `echo ok` hung 26 minutes and
# a log tail 46, both on m1mini). A process still running at the cap is killed with its whole tree. Output is read
# asynchronously so a full pipe cannot hold it, and stderr is folded into Out.
function Invoke-Capped([string]$Exe, [string[]]$ArgList, [int]$TimeoutSec) {
  $psi = [System.Diagnostics.ProcessStartInfo]::new($Exe)
  foreach ($a in $ArgList) { $psi.ArgumentList.Add($a) }
  $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true; $psi.RedirectStandardInput = $true
  $psi.UseShellExecute = $false
  $p = [System.Diagnostics.Process]::Start($psi)
  $p.StandardInput.Close()
  $o = $p.StandardOutput.ReadToEndAsync(); $e = $p.StandardError.ReadToEndAsync()
  $timedOut = -not $p.WaitForExit($TimeoutSec * 1000)
  if ($timedOut) {
    try { $p.Kill($true) } catch {}
    $null = $p.WaitForExit(5000)
  }
  # A killed ssh can leave a grandchild holding the pipes, so the reads are bounded too.
  $null = [System.Threading.Tasks.Task]::WaitAll(@($o, $e), 5000)
  $text = $(if ($o.IsCompleted) { $o.Result } else { '' }) + $(if ($e.IsCompleted) { $e.Result } else { '' })
  [pscustomobject]@{ Code = $(if ($timedOut) { -1 } else { $p.ExitCode }); TimedOut = $timedOut; Out = $text }
}

# Invoke-CappedRetry is Invoke-Capped that, on a cap overrun only, runs the call once more. A call that exits non-zero
# is an answer and is not retried. A second overrun comes back with TimedOut set, for the caller to fail the step on,
# naming $Label. The attempts are said, so a log shows the hang rather than a silent minute.
function Invoke-CappedRetry([string]$Label, [string]$Exe, [string[]]$ArgList, [int]$TimeoutSec) {
  for ($try = 1; $try -le 2; $try++) {
    $r = Invoke-Capped $Exe $ArgList $TimeoutSec
    if (-not $r.TimedOut) { return $r }
    Say "$Label hung past ${TimeoutSec}s (attempt $try of 2), killed$(if ($try -eq 1) { '. retrying once' })"
  }
  return $r
}

# Test-RoomBackOnHub is the verdict on a room that was restarted, from the hub alone: attached on a connection made
# after $After and, when $Commit is given, running that commit. Never from an ssh call's exit or output.
function Test-RoomBackOnHub([string]$Name, [datetime]$After, [string]$Commit = '') {
  try { $r = (Invoke-RestMethod $HubRooms -TimeoutSec 3).rooms | Where-Object { $_.name -eq $Name } | Select-Object -First 1 }
  catch { return $false }
  if (-not $r -or ([datetime]$r.since).ToUniversalTime() -lt $After.ToUniversalTime()) { return $false }
  if ($Commit -and $r.commit -and -not ($r.commit.StartsWith($Commit.Substring(0, [Math]::Min(7, $Commit.Length))))) { return $false }
  return $true
}

# Resolve-SshRoute finds the ssh name that reaches a room: the bare name first, then "<name>.atrium", which rides the
# hub and still works when the bare name does not resolve or times out. $Probe is called with one candidate and returns
# an object with Code and TimedOut. The result carries the candidate that answered (Host, empty when none did), the
# answers seen in order (Results) and what was Tried, so the caller keeps to one route for every later call.
function Resolve-SshRoute([string]$Name, [scriptblock]$Probe) {
  $tried = @()
  $results = @()
  foreach ($cand in @($Name, "$Name.atrium")) {
    $tried += $cand
    $r = & $Probe $cand
    $results += $r
    if (-not $r.TimedOut -and $r.Code -eq 0) {
      return [pscustomobject]@{ Host = $cand; Tried = $tried; Results = $results }
    }
  }
  return [pscustomobject]@{ Host = ''; Tried = $tried; Results = $results }
}

# The swap stops the room, and over "<room>.atrium" the ssh session rides that room, so it can die mid-swap. On mac
# New-DetachedCommand builds the ssh command that writes $Script to a file on the room machine and starts it there under
# nohup, its output going to ~/.atrium/<Tag>.out and ending in $SwapDone. Read-DetachedCommand reads that file back.
# Windows does not use it: Win32_Process Create answers Access denied over ssh (sgg), and the in-session swap there
# survives the session dropping (sg3), so Invoke-WindowsSwap runs it in the session and lets the hub decide.
$SwapDone = 'SWAP-DONE'
function ConvertTo-Base64Utf8([string]$Text) { [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($Text)) }
function New-DetachedCommand([string]$Os, [string]$Script, [string]$Tag = 'deploy-swap') {
  if ($Os -ne 'mac') { throw "New-DetachedCommand is for mac and linux, not $Os" }
  $body = $Script + ' ; echo ' + $SwapDone
  return 'mkdir -p ~/.atrium ; echo ' + (ConvertTo-Base64Utf8 $body) + ' | base64 --decode > ~/.atrium/' + $Tag + '.sh ; ' +
    'rm -f ~/.atrium/' + $Tag + '.out ; nohup sh ~/.atrium/' + $Tag + '.sh > ~/.atrium/' + $Tag + '.out 2>&1 < /dev/null & echo detached'
}
function Read-DetachedCommand([string]$Os, [string]$Tag = 'deploy-swap') {
  return 'cat ~/.atrium/' + $Tag + '.out 2>/dev/null'
}

# Wait-SwapDone polls while a detached swap runs. $Back says whether the hub sees the room back. $ReadOut returns the
# swap's output file as text or throws when the ssh read fails, which it will while the room is down over "<room>.atrium".
# It returns when the room is back, the output ends in $SwapDone, or $Budget seconds pass. A failed read is not a verdict.
# Invoke-WindowsSwap runs the swap in the ssh session. $Run returns the call's result (Code, TimedOut, Out). A session that
# drops (non-zero exit, no verdict in what it printed) decides nothing, so the hub is waited on for $Budget seconds,
# and the caller goes on to the hub check either way. A hung call is returned as hung and not retried.
function Invoke-WindowsSwap([scriptblock]$Run, [scriptblock]$Back, [int]$Budget, [int]$PollSec = 5) {
  $x = & $Run
  $out = [string]$x.Out
  if ($x.TimedOut -or $x.Code -eq 0 -or $out -match 'PORT-WAIT-TIMEOUT|STORE-WAIT-TIMEOUT|left it running|not attached') {
    return [pscustomobject]@{ Back = $false; Out = $out; TimedOut = [bool]$x.TimedOut }
  }
  $w = Wait-SwapDone $Back { throw 'ssh session dropped' } $Budget $PollSec
  [pscustomobject]@{ Back = $w.Back; Out = $out; TimedOut = $w.TimedOut }
}

function Wait-SwapDone([scriptblock]$Back, [scriptblock]$ReadOut, [int]$Budget, [int]$PollSec = 5) {
  $t0 = Get-Date
  $out = ''
  while ($true) {
    $up = [bool](& $Back)
    try { $read = [string](& $ReadOut); if ($read) { $out = $read } } catch {}
    if ($up -or $out -match [regex]::Escape($SwapDone)) { return [pscustomobject]@{ Back = $up; Out = $out; TimedOut = $false } }
    if (((Get-Date) - $t0).TotalSeconds -ge $Budget) { return [pscustomobject]@{ Back = $false; Out = $out; TimedOut = $true } }
    if ($PollSec -gt 0) { Start-Sleep -Seconds $PollSec }
  }
}
