# Deploy claude/main everywhere in one command: build, the remote rooms, the hub with claude-sg4, then sg4-control.
#
#   pwsh -NoProfile -File C:\Users\claude\.atrium2\scripts\atrium-deploy.ps1
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms hub              # hub only, rooms untouched
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms sg3,m1mini,sgg   # remote rooms only
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms all -WhatIf      # say every step, change nothing
#
# -Rooms takes any of hub, claude-sg4, sg4-control, sg3, m1mini, sgg, or `local` (hub, claude-sg4, sg4-control, the
# default) or `all`. The order is fixed whatever order they are given in: remote rooms first, because they touch no
# terminal here, then the hub and claude-sg4 (deploy-batch.ps1, or deploy-hub-only.ps1 when claude-sg4 is not named),
# then sg4-control, which holds the orchestrator's terminal.
#
# THE RESULT is one line per room, printed last and logged:
#
#   hub: success
#   sgg: failed - offline, ssh could not connect
#
# Run from an atrium card, the same lines are said to that card when the deploy ends, so the card that asked for it
# hears the result with nothing to read. -ReportTo <card>@<room> names another card.
#
# RUN FROM AN ATRIUM CARD IT DETACHES ITSELF. Stopping sg4-control ends the terminal that started it, so when
# ATRIUM_TASK_ID is set and a room here is named, it starts itself again as a hidden process and returns at once. The
# steps are logged to C:\Users\claude\.atrium2\atrium-deploy.log.
#
# Remote rooms are built for their platform with the same stamp, copied over with scp, stopped with `atrium stop`, and
# swapped with the old binary kept as atrium.prev. m1mini runs through `zsh -lic`, or the room cannot find claude.
param([string[]]$Rooms = @('local'), [switch]$NoBuild, [switch]$WhatIf, [switch]$Immediate, [switch]$Detached,
  [string]$ReportTo)

$ErrorActionPreference = 'Continue'
$LiveTag = 'DEPLOY'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'atrium-deploy.log'

$remoteNames = 'sg3', 'm1mini', 'sgg'
$known = @('hub', 'claude-sg4', 'sg4-control') + $remoteNames
$want = @()
foreach ($r in ($Rooms -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })) {
  switch ($r) {
    'local' { $want += 'hub', 'claude-sg4', 'sg4-control' }
    'all' { $want += $known }
    default {
      if ($known -notcontains $r) { Say "FATAL: unknown room '$r'. known: $($known -join ', '), local, all"; exit 1 }
      $want += $r
    }
  }
}
$want = @($known | Where-Object { $_ -in $want })
$local = @($want | Where-Object { $_ -in 'hub', 'claude-sg4', 'sg4-control' })

if (-not $ReportTo -and $env:ATRIUM_TASK_ID) {
  $ReportTo = if ($env:ATRIUM_ROOM) { "$env:ATRIUM_TASK_ID@$env:ATRIUM_ROOM" } else { $env:ATRIUM_TASK_ID }
}

if ($env:ATRIUM_TASK_ID -and $local.Count -and -not $Detached -and -not $WhatIf) {
  $argv = @('-NoProfile', '-File', $PSCommandPath, '-Rooms', ($want -join ','), '-Detached')
  if ($ReportTo) { $argv += '-ReportTo', $ReportTo }
  if ($NoBuild) { $argv += '-NoBuild' }
  if ($Immediate) { $argv += '-Immediate' }
  Start-Process pwsh -ArgumentList $argv -WindowStyle Hidden
  Say "detached: deploying $($want -join ', '). the result is said to this card when it ends"
  exit 0
}
if ($Detached) {
  Clear-SessionEnv
  Remove-Item Env:ATRIUM_NEW_BUILD -ErrorAction SilentlyContinue
  # Let the card that started this end its turn before anything stops.
  Start-Sleep -Seconds 10
}
Say "deploying $($want -join ', ')"

$results = [ordered]@{}
foreach ($n in $want) { $results[$n] = 'failed - not reached' }

# Report says the result lines, logs them, and says them to $ReportTo through the hub. The hub routes `card@room`.
function Report {
  $lines = foreach ($k in $results.Keys) { "${k}: $($results[$k])" }
  $lines | ForEach-Object { Say $_ }
  if (-not $ReportTo) { return }
  $text = "atrium-deploy$(if ($WhatIf) { ' (WhatIf)' }):`n" + ($lines -join "`n")
  # The hub routes /v1/* to the room in X-Atrium-Room, and refuses a request that names none.
  $card, $room = $ReportTo -split '@', 2
  $headers = @{}
  # A bare card id means the local room, which is where this script runs.
  if (-not $room) { $room = 'sg4-control' }
  $headers['X-Atrium-Room'] = $room
  $body = @{ to = $card; from = 'atrium-deploy'; text = $text; kind = 'fyi'; wake = $true } | ConvertTo-Json -Compress
  # Retried while the hub or the room is coming back. A 4xx is an answer, not an outage.
  for ($i = 0; $i -lt 30; $i++) {
    try {
      $null = Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:7778/v1/say' -ContentType 'application/json' `
        -Headers $headers -Body $body -TimeoutSec 15
      Say "result said to $ReportTo"
      return
    } catch {
      $code = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
      if ($code -ge 400 -and $code -lt 500) { break }
      Start-Sleep -Seconds 10
    }
  }
  Say "could not say the result to ${ReportTo}: $($Error[0].Exception.Message)"
}

# Test-Unheld answers whether a room that came back has lifted its deploy hold, within 30s. A room left held refuses
# every launch and every agent's next call, so it is not a success however healthy it looks (2026-10-12: claude-sg4).
function Test-Unheld([string]$Url, [hashtable]$Headers = @{}) {
  if ($WhatIf) { return $true }
  for ($i = 0; $i -lt 6; $i++) {
    try { if (-not (Invoke-RestMethod $Url -Headers $Headers -TimeoutSec 10).hold) { return $true } } catch {}
    Start-Sleep -Seconds 5
  }
  return $false
}
$heldMsg = 'failed - came back but its deploy hold is still on, so it refuses launches'

# 1. Build, once, from a clean claude/main. build-deploy.ps1 refuses anything else.
if (-not $NoBuild) {
  & "$PSScriptRoot\build-deploy.ps1" -WhatIf:$WhatIf
  if ($LASTEXITCODE -ne 0) {
    foreach ($n in $want) { $results[$n] = 'failed - build failed, see build-deploy.log' }
    Report
    exit 1
  }
}
$commit = git -C $Repo rev-parse HEAD
$ver = git -C $Repo describe --tags --exact-match 2>$null
if (-not $ver) { $ver = 'dev' }
$pkg = 'github.com/dovholuknf/atrium/internal/cli'
$ldflags = "-X $pkg.Version=$ver -X $pkg.Commit=$commit -X $pkg.Tree=clean"

$scp = 'C:\WINDOWS\System32\OpenSSH\scp.exe'
$ssh = 'C:\WINDOWS\System32\OpenSSH\ssh.exe'
$sshOpts = @('-o', 'ConnectTimeout=25', '-o', 'BatchMode=yes')
$built = @()

# 2. Remote rooms. Each: reach it, cross-build, copy, stop, swap, start detached, check it is back on the hub.
# `atrium stop` returns before the room lets go of its board port, and a start that finds the port answering
# leaves it alone and exits. So each swap waits for 127.0.0.1:7781 to stop listening, as long as the room's wrap-up can
# take (restart_wrap_wait_s, read from the room here, 300 by default) plus a margin. A port still held when that runs
# out prints $portTimeout and exits before the swap, so the old room is neither swapped nor detached over.
$portWait = (Get-WrapWait) + 60
$portTimeout = 'PORT-WAIT-TIMEOUT'
$waitFree = '$n = 0 ; while ($n -lt ' + $portWait + ') { $c = New-Object Net.Sockets.TcpClient ; ' +
  'try { if (-not $c.ConnectAsync(''127.0.0.1'', 7781).Wait(500)) { break } } catch { break } ' +
  'finally { $c.Dispose() } ; Start-Sleep 1 ; $n++ } ; ' +
  'if ($n -ge ' + $portWait + ') { ''' + $portTimeout + ''' ; exit 3 } ; '
$winEnv = '$e = Join-Path $HOME ''.atrium\toolchain\room-env.ps1'' ; if (Test-Path $e) { . $e } ; ' +
  '$b = Join-Path $HOME ''.atrium\bin\atrium.exe'' ; '
$winSwap = $winEnv + '& $b stop ; ' +
  $waitFree +
  'Move-Item -Force $b ($b -replace ''atrium.exe$'', ''atrium.prev.exe'') ; ' +
  'Move-Item -Force (Join-Path $HOME ''atrium.new.exe'') $b ; & $b room --detach'
$sg3Detach = 'C:/Users/claude/.atrium/bin/atrium.exe room --detach --dir C:/Users/claude/.atrium/room ' +
  '--db C:/Users/claude/.atrium/atrium.db'
$macWait = 'for i in {1..' + $portWait + '}; do lsof -iTCP:7781 -sTCP:LISTEN >/dev/null || break ; sleep 1 ; done ; ' +
  'if lsof -iTCP:7781 -sTCP:LISTEN >/dev/null ; then echo ' + $portTimeout + ' ; exit 3 ; fi ; '
$macDetach = '~/.local/bin/atrium room --detach --dir /Users/claude/.atrium/room --db /Users/claude/.atrium/atrium.db ' +
  '--agent 127.0.0.1:7777 --http 127.0.0.1:7781'
# Retry is run when the detach after the swap found the old room still answering. It waits for the port again, then detaches.
# Log is how to read the room's own log over ssh, for Get-RoomLink.
$remote = @{
  'sg3'    = @{ GOOS = 'windows'; GOARCH = 'amd64'; Dest = 'atrium.new.exe'
    Swap = 'C:/Users/claude/.atrium/bin/atrium.exe stop ; ' + $waitFree +
      'Move-Item -Force C:/Users/claude/.atrium/bin/atrium.exe C:/Users/claude/.atrium/bin/atrium.prev.exe ; ' +
      'Move-Item -Force C:/Users/claude/atrium.new.exe C:/Users/claude/.atrium/bin/atrium.exe ; ' + $sg3Detach
    Retry = $waitFree + $sg3Detach
    Log = 'Get-Content C:/Users/claude/.atrium/room/room.log -Tail 300' }
  'sgg'    = @{ GOOS = 'windows'; GOARCH = 'amd64'; Dest = 'atrium.new.exe'; Swap = $winSwap
    Retry = $winEnv + $waitFree + '& $b room --detach'
    Log = 'Get-Content (Join-Path $HOME ''.atrium\room\room.log'') -Tail 300' }
  'm1mini' = @{ GOOS = 'darwin'; GOARCH = 'arm64'; Dest = '.local/bin/atrium.new'
    Swap = 'zsh -lic "~/.local/bin/atrium stop ; ' + $macWait +
      'mv -f ~/.local/bin/atrium ~/.local/bin/atrium.prev && ' +
      'mv -f ~/.local/bin/atrium.new ~/.local/bin/atrium && chmod +x ~/.local/bin/atrium && ' + $macDetach + '"'
    Retry = 'zsh -lic "' + $macWait + $macDetach + '"'
    Log = 'tail -n 300 ~/.atrium/room/room.log' }
}

# Get-RoomLink here shadows live-common's, which reads the local room.err. For a remote room, $RoomName is that room's
# name while Wait-RoomAttached runs, and its link lines are in its own room.log on its own machine, read over ssh.
$localRoomLink = ${function:Get-RoomLink}
function Get-RoomLink {
  if (-not $remote.Contains($RoomName)) { return & $localRoomLink }
  $lines = & $ssh @sshOpts $RoomName $remote[$RoomName].Log 2>$null
  $last = $lines | Select-String -Pattern '\[link\] (attached to hub|hub \S+: )' | Select-Object -Last 1
  if (-not $last) { return '' }
  if ($last.Line -match '\[link\] attached to hub') { return 'up' }
  return 'down'
}
foreach ($name in $remoteNames) {
  if ($want -notcontains $name) { continue }
  $r = $remote[$name]
  $null = & $ssh @sshOpts $name 'echo ok' 2>&1
  if ($LASTEXITCODE -ne 0) { $results[$name] = 'failed - offline, ssh could not connect'; Say "${name}: offline"; continue }
  $exe = if ($r.GOOS -eq 'windows') { 'atrium.exe' } else { 'atrium' }
  $out = Join-Path $Repo "build.claude\deploy\$($r.GOOS)-$($r.GOARCH)\$exe"
  if ($built -notcontains "$($r.GOOS)-$($r.GOARCH)") {
    Invoke-Step "build $($r.GOOS)/$($r.GOARCH) -> $out" {
      $env:CGO_ENABLED = '0'; $env:GOOS = $r.GOOS; $env:GOARCH = $r.GOARCH
      $b = & go -C $Repo build -ldflags $ldflags -o $out ./cmd/atrium 2>&1
      $code = $LASTEXITCODE
      Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
      if ($code -ne 0) { $b | ForEach-Object { Say "  $_" }; Remove-Item $out -ErrorAction SilentlyContinue }
    }
    if (-not $WhatIf -and -not (Test-Path $out)) { $results[$name] = 'failed - cross-build failed'; continue }
    $built += "$($r.GOOS)-$($r.GOARCH)"
  }
  Invoke-Step "scp $out -> ${name}:$($r.Dest)" { & $scp -q @sshOpts $out "${name}:$($r.Dest)" }
  if (-not $WhatIf -and $LASTEXITCODE -ne 0) { $results[$name] = 'failed - scp failed'; continue }
  $start = Get-Date
  $swapOut = @()
  Invoke-Step "ssh ${name}: stop, wait up to ${portWait}s for the port, swap (old kept as atrium.prev), start" {
    $script:swapOut = @(& $ssh @sshOpts $name $r.Swap 2>&1 | ForEach-Object { "$_" })
    $swapOut | ForEach-Object { Say "  ${name}: $_" }
  }
  if ($WhatIf) { $results[$name] = 'success (WhatIf)'; continue }
  if ($swapOut -contains $portTimeout) {
    $results[$name] = "failed - the room still held its port after ${portWait}s, so nothing was swapped or started"
    continue
  }
  # The old room can outlive the port check by a moment and answer the detach: wait for the port again, detach once more.
  if (($swapOut -join "`n") -match 'left it running|not attached') {
    Say "${name}: detach found the old room still running. retrying once"
    $retryOut = @(& $ssh @sshOpts $name $r.Retry 2>&1 | ForEach-Object { "$_" })
    $retryOut | ForEach-Object { Say "  ${name}: $_" }
    if ($retryOut -contains $portTimeout) {
      $results[$name] = "failed - the room still held its port after ${portWait}s more, so it was not started"
      continue
    }
  }
  $saved = $RoomName
  $RoomName = $name
  $back = Wait-RoomAttached $start
  $RoomName = $saved
  $results[$name] = if (-not $back) { 'failed - restarted but not back on the hub' }
    elseif (-not (Test-Unheld 'http://127.0.0.1:7778/v1/hold' @{ 'X-Atrium-Room' = $name })) { $heldMsg }
    else { 'success' }
}

# 3. The hub, with claude-sg4 when it is named. Run as child scripts, so their own checks and reverts apply.
if ($want -contains 'claude-sg4') {
  & "$PSScriptRoot\deploy-batch.ps1" -WhatIf:$WhatIf -Immediate:$Immediate
  $code = $LASTEXITCODE
  $ok = if ($WhatIf) { 'success (WhatIf)' } else { 'success' }
  switch ($code) {
    0 { $results['claude-sg4'] = $ok; if ($results.Contains('hub')) { $results['hub'] = $ok } }
    2 { $results['claude-sg4'] = 'failed - serves locally but is not on the hub, see deploy-batch.log'
      if ($results.Contains('hub')) { $results['hub'] = $ok } }
    default { $results['claude-sg4'] = "failed - exit $code, see deploy-batch.log"
      if ($results.Contains('hub')) { $results['hub'] = "failed - exit $code, see deploy-batch.log" } }
  }
  if ($code -eq 0 -and -not (Test-Unheld $RoomHoldUrl)) { $results['claude-sg4'] = $heldMsg }
} elseif ($want -contains 'hub') {
  & "$PSScriptRoot\deploy-hub-only.ps1" -WhatIf:$WhatIf -NoGate
  $code = $LASTEXITCODE
  $results['hub'] = if ($code -ne 0) { "failed - exit $code, see deploy.log" } elseif ($WhatIf) { 'success (WhatIf)' } else { 'success' }
}

# 4. sg4-control last: stop it, swap its own binary in ctl-bin by two renames, start it. Its cards reopen.
if ($want -contains 'sg4-control') {
  Stop-Control
  $ctlExe = Join-Path $CtlBinDir 'atrium.exe'
  $next = Join-Path $CtlBinDir 'atrium.next.exe'
  $aside = Join-Path $CtlBinDir "atrium.old-$(Get-Date -Format yyyyMMddHHmmss).exe"
  Invoke-Step "copy $AtriumNew -> $next, rename $ctlExe -> $aside, $next -> $ctlExe" {
    Copy-Item $AtriumNew $next -Force
    Rename-Item $ctlExe $aside
    Rename-Item $next (Split-Path $ctlExe -Leaf)
    Get-ChildItem (Join-Path $CtlBinDir 'atrium.old-*.exe') | Sort-Object LastWriteTime -Descending |
      Select-Object -Skip 3 | ForEach-Object { Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue }
  }
  & "$PSScriptRoot\start-atrium-control.ps1" -WhatIf:$WhatIf
  $results['sg4-control'] = if ($WhatIf) { 'success (WhatIf)' }
    elseif (-not (Test-Up $CtlHealth)) { 'failed - not answering after start, see ctl\room.err' }
    elseif (-not (Test-Unheld 'http://127.0.0.1:7791/v1/hold')) { $heldMsg }
    else { 'success' }
}

Say "commit $($commit.Substring(0, 8))"
Report
if ($results.Values | Where-Object { $_ -like 'failed*' }) { exit 1 }
