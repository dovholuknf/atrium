# Deploy claude/main everywhere in one command: build, the remote rooms, the hub with claude-sg4, and sg4-control on request.
#
#   pwsh -NoProfile -File C:\Users\claude\.atrium2\scripts\atrium-deploy.ps1
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms hub              # hub only, rooms untouched
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms sg3,m1mini,sgg   # remote rooms only
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms all -WhatIf      # say every step, change nothing
#
# -Rooms takes any of hub, claude-sg4, sg3, m1mini, sgg, sgg-zrok, or `local` (hub, claude-sg4, the default) or `all`.
# The order is fixed whatever order they are given in: remote rooms first, because they touch no terminal here, then the
# hub and claude-sg4 (deploy-batch.ps1, or deploy-hub-only.ps1 when claude-sg4 is not named), then sg4-control.
#
# sgg-zrok is a second, --isolated room on sgg, as localai, in ~/.atrium-zrok, on 7787 and 7791, linking over zrok. It
# is reached over sgg's ssh route, never its own name.
#
# sg4-control is OPT-IN. It is in no default and not in `all`, and takes both -Control and -IKnowItKillsMyShell, because
# the orchestrator works in it. Its claude.exe runs under the pty host (atrium-pty.exe), not the room, so a stop keeps
# it (2026-10-26 22:44). One switch without the other is refused.
#
# THE RESULT is one line per room, printed last and logged:
#
#   hub: success
#   sgg: failed - offline, ssh could not connect by any route
#   sgg: failed - ssh refused as claude, the host answered and rejected the key
#
# Run from an atrium card, the same lines are said to that card when the deploy ends, so the card that asked for it
# hears the result with nothing to read. -ReportTo <card>@<room> names another card.
#
# IT REFUSES TO RUN UNDER AN ATRIUM ROOM (refuse-supervised.ps1): stopping the room ends the shell that runs it. Run it
# from a terminal that is not under atrium, such as a fresh wt.exe window. A shell that carries ATRIUM_TASK_ID with no
# room above it still detaches itself and returns at once. The steps are logged to
# C:\Users\claude\.atrium2\atrium-deploy.log.
#
# A room with pty_host on keeps its runners across the stop and the swap: `atrium stop` leaves what the pty host holds
# running and the new room reattaches it. The result then reads `success (runners kept)`, and no deploy hold is set for
# it (deploy-batch.ps1, Test-RunnersSurvive). Turning it on per room is docs/changes/blue-green-1-2.md.
#
# Remote rooms are built for their platform with the same stamp, copied over with scp, stopped with `atrium stop`, and
# swapped with the old binary kept as atrium.prev. m1mini runs through `zsh -lic`, or the room cannot find claude.
param([string[]]$Rooms = @('local'), [switch]$NoBuild, [switch]$WhatIf, [switch]$Immediate, [switch]$KillRunners, [switch]$Detached,
  [string]$ReportTo, [switch]$Control, [switch]$IKnowItKillsMyShell)

$ErrorActionPreference = 'Continue'
$LiveTag = 'DEPLOY'
. "$PSScriptRoot\refuse-supervised.ps1"
. "$PSScriptRoot\live-common.ps1"
$LiveLog = if ($env:ATRIUM_LIVE_LOG) { $env:ATRIUM_LIVE_LOG } else { Join-Path $Base 'atrium-deploy.log' }

if (-not $ReportTo -and $env:ATRIUM_TASK_ID) {
  $ReportTo = if ($env:ATRIUM_ROOM) { "$env:ATRIUM_TASK_ID@$env:ATRIUM_ROOM" } else { $env:ATRIUM_TASK_ID }
}

# Stop-Early ends a deploy that cannot begin. The detached child has no window, so what it says goes to the log and to
# the card, or the deploy ends in silence.
function Stop-Early([string]$Message) {
  Say $Message
  if ($Detached -and $ReportTo) { $null = Send-CardSay $ReportTo "atrium-deploy: $Message" }
  exit 1
}
trap { if ($Detached) { Stop-Early "failed before its first step: $($_.Exception.Message)" }; break }

$why = Test-ControlOptIn -Control:$Control -IKnowItKillsMyShell:$IKnowItKillsMyShell `
  -Named:(($Rooms -split ',' | ForEach-Object { $_.Trim() }) -contains 'sg4-control')
if ($why) { Stop-Early "refusing: $why" }

$noGo = Find-Go
if ($noGo) { Stop-Early $noGo }

$remoteNames = 'sg3', 'm1mini', 'sgg', 'sgg-zrok'
try { $want = Resolve-DeployRooms -Rooms $Rooms -Control:$Control -IKnowItKillsMyShell:$IKnowItKillsMyShell }
catch { Stop-Early "FATAL: $($_.Exception.Message)" }
$local = @($want | Where-Object { $_ -in 'hub', 'claude-sg4', 'sg4-control' })
# Only a deploy that stops something on this machine can end the shell it runs in. A deploy of only remote rooms
# stops nothing here, so it runs from any shell, a room's included.
# The hub holds no shell, so stopping it ends none: only the local ROOMS named here are checked against this shell's.
$localRooms = @($local | Where-Object { $_ -ne 'hub' })
if ($localRooms.Count) { Assert-NotSupervised -Targets $localRooms }

if ($env:ATRIUM_TASK_ID -and $local.Count -and -not $Detached -and -not $WhatIf) {
  $argv = Get-DetachedArgv $PSCommandPath $want $ReportTo $PSBoundParameters
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

# Report says the result lines, logs them, and says them to $ReportTo through the hub. The hub routes `card@room`. It is
# called once per run, and lists every room.
function Report {
  $lines = foreach ($k in $results.Keys) { "${k}: $($results[$k])" }
  $lines | ForEach-Object { Say $_ }
  if (-not $ReportTo) { return }
  # A retry that fails the way the last report said gets one line, not the list again. Never nothing: a detached
  # deploy's card waits for this.
  $lastFile = Join-Path $Base 'atrium-deploy-last.json'
  $last = @{}
  if (-not $WhatIf -and (Test-Path $lastFile)) {
    try { (Get-Content $lastFile -Raw | ConvertFrom-Json).PSObject.Properties | ForEach-Object { $last[$_.Name] = $_.Value } } catch {}
  }
  $repeat = -not $WhatIf
  foreach ($k in $results.Keys) { if ($results[$k] -notlike 'failed*' -or $last[$k] -ne $results[$k]) { $repeat = $false } }
  if (-not $WhatIf) {
    foreach ($k in $results.Keys) { $last[$k] = $results[$k] }
    $last | ConvertTo-Json | Set-Content $lastFile
  }
  $text = "atrium-deploy$(if ($WhatIf) { ' (WhatIf)' }):`n" + ($lines -join "`n")
  if ($repeat) { $text = 'atrium-deploy: every room failed again as the last report said' }
  $null = Send-CardSay $ReportTo $text
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
$codeId = (& go -C $Repo run ./tools/codeid)
$ldflags = "-X github.com/dovholuknf/atrium/internal/api.CodeID=$codeId -X $pkg.Version=$ver -X $pkg.Commit=$commit -X $pkg.Tree=clean"

$scp = 'C:\WINDOWS\System32\OpenSSH\scp.exe'
$ssh = 'C:\WINDOWS\System32\OpenSSH\ssh.exe'
# ServerAlive drops a session whose peer went quiet, and Invoke-Ssh puts a wall-clock cap on every call besides, since
# ConnectTimeout bounds only the connect. The caps are seconds: a probe is short, a copy and the swap are not.
$sshOpts = @('-o', 'ConnectTimeout=25', '-o', 'BatchMode=yes', '-o', 'ServerAliveInterval=10', '-o', 'ServerAliveCountMax=3')
$probeCap = 60; $scpCap = 300; $diagCap = 30
function Invoke-Ssh([string]$Label, [string[]]$SshArgs, [int]$Cap = $probeCap) {
  Invoke-CappedRetry $Label $ssh ($sshOpts + $SshArgs) $Cap
}
function Invoke-SshOnce([string]$Label, [string[]]$SshArgs, [int]$Cap) {
  # The swap is not idempotent: run twice, it stops the new room and finds no atrium.new. A hang is never retried.
  $r = Invoke-Capped $ssh ($sshOpts + $SshArgs) $Cap
  if ($r.TimedOut) { Say "$Label hung past ${Cap}s, killed. not retried" }
  $r
}
# A cold connect to sgg takes about 10s, so the connect allows 25s. The bare try is one connect, no retry.
$bareOpts = @('-o', 'ConnectTimeout=25', '-o', 'BatchMode=yes')
$bareCap = 35
$readOpts = @('-o', 'ConnectTimeout=25', '-o', 'BatchMode=yes')
# Over <room>.atrium the ssh session rides the room being stopped. On mac the swap runs detached (nohup) and writes to a
# file, and the hub is waited on, reading the file whenever ssh answers. On Windows it runs in the session, which survives
# the drop (sg3), and a dropped session is no verdict: the hub decides.
function Start-DetachedSwap([string]$Name, [string]$SshHost, [string]$Os, [string]$Script, [datetime]$Since, [int]$Budget) {
  $back = { Test-RoomBackOnHub $Name $Since $commit }
  if ($Os -eq 'windows') {
    $run = { Invoke-SshOnce "ssh $Name swap" @($SshHost, $Script) $Budget }
    return Invoke-WindowsSwap $run $back $Budget
  }
  $launch = Invoke-SshOnce "ssh $Name swap launch" @($SshHost, (New-DetachedCommand $Os $Script)) $diagCap
  if ($launch.TimedOut -or $launch.Code -ne 0 -or $launch.Out -notmatch 'detached') {
    return [pscustomobject]@{ Back = $false; Out = "DETACH-FAILED $($launch.Out)"; TimedOut = $launch.TimedOut }
  }
  $readCmd = Read-DetachedCommand $Os
  $read = {
    $x = Invoke-Capped $ssh ($readOpts + @($SshHost, $readCmd)) $diagCap
    if ($x.TimedOut -or $x.Code -ne 0) { throw 'read failed' }
    $x.Out
  }
  Wait-SwapDone $back $read $Budget
}
function Get-HungMsg([string]$Label, [int]$Cap) { "failed - $Label hung past ${Cap}s and was killed" }
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
# The port going quiet is not the room letting go of its store: the old room can still hold atrium.db, and the new one
# then exits "store held by another process" (sgg). So each swap also waits, up to $StoreBudget, for the store itself.
$winWaitStore = New-StoreWaitScript 'windows' '(Join-Path $HOME ''.atrium\atrium.db'')'
$sg3WaitStore = New-StoreWaitScript 'windows' '''C:/Users/claude/.atrium/atrium.db'''
$macWaitStore = New-StoreWaitScript 'mac' '~/.atrium/atrium.db'
$winEnv ='$e = Join-Path $HOME ''.atrium\toolchain\room-env.ps1'' ; if (Test-Path $e) { . $e } ; ' +
  '$b = Join-Path $HOME ''.atrium\bin\atrium.exe'' ; '
$winSwap = $winEnv + '& $b stop ; ' +
  $waitFree + $winWaitStore +
  'Move-Item -Force $b ($b -replace ''atrium.exe$'', ''atrium.prev.exe'') ; ' +
  'Move-Item -Force (Join-Path $HOME ''atrium.new.exe'') $b ; & $b room --detach'
$sg3Detach = 'C:/Users/claude/.atrium/bin/atrium.exe room --detach --dir C:/Users/claude/.atrium/room ' +
  '--db C:/Users/claude/.atrium/atrium.db'
$macWait = 'for i in {1..' + $portWait + '}; do lsof -iTCP:7781 -sTCP:LISTEN >/dev/null || break ; sleep 1 ; done ; ' +
  'if lsof -iTCP:7781 -sTCP:LISTEN >/dev/null ; then echo ' + $portTimeout + ' ; exit 3 ; fi ; '
$macDetach = '~/.local/bin/atrium room --detach --dir /Users/claude/.atrium/room --db /Users/claude/.atrium/atrium.db ' +
  '--agent 127.0.0.1:7777 --http 127.0.0.1:7781'
# sgg-zrok keeps its address beside its own --dir (--isolated), so a bare `atrium stop` would reach sgg: it names its board.
$zrokEnv = '$e = Join-Path $HOME ''.atrium\toolchain\room-env.ps1'' ; if (Test-Path $e) { . $e } ; ' +
  '$b = Join-Path $HOME ''.atrium-zrok\bin\atrium.exe'' ; '
$zrokWait = $waitFree.Replace('7781', '7791') + (New-StoreWaitScript 'windows' '(Join-Path $HOME ''.atrium-zrok\atrium.db'')')
$zrokDetach = '& $b room --detach --isolated --dir (Join-Path $HOME ''.atrium-zrok\room'') ' +
  '--db (Join-Path $HOME ''.atrium-zrok\atrium.db'') --agent 127.0.0.1:7787 --http 127.0.0.1:7791'
# Retry is run when the detach after the swap found the old room still answering. It waits for the port again, then detaches.
# Log is how to read the room's own log over ssh, for Get-RoomLink.
$remote = @{
  'sg3'    = @{ GOOS = 'windows'; GOARCH = 'amd64'; Dest = 'atrium.new.exe'
    Swap = 'C:/Users/claude/.atrium/bin/atrium.exe stop ; ' + $waitFree + $sg3WaitStore +
      'Move-Item -Force C:/Users/claude/.atrium/bin/atrium.exe C:/Users/claude/.atrium/bin/atrium.prev.exe ; ' +
      'Move-Item -Force C:/Users/claude/atrium.new.exe C:/Users/claude/.atrium/bin/atrium.exe ; ' + $sg3Detach
    Retry = $waitFree + $sg3WaitStore + $sg3Detach
    Log = 'Get-Content C:/Users/claude/.atrium/room/room.log -Tail 300' }
  'sgg'    = @{ GOOS = 'windows'; GOARCH = 'amd64'; Dest = 'atrium.new.exe'; Swap = $winSwap
    Retry = $winEnv + $waitFree + $winWaitStore + '& $b room --detach'
    Log = 'Get-Content (Join-Path $HOME ''.atrium\room\room.log'') -Tail 300' }
  'sgg-zrok' = @{ GOOS = 'windows'; GOARCH = 'amd64'; Dest = 'atrium-zrok.new.exe'; SshName = 'sgg'
    Swap = $zrokEnv + '& $b stop --url http://127.0.0.1:7791 ; ' + $zrokWait +
      'Move-Item -Force $b ($b -replace ''atrium.exe$'', ''atrium.prev.exe'') ; ' +
      'Move-Item -Force (Join-Path $HOME ''atrium-zrok.new.exe'') $b ; ' + $zrokDetach
    Retry = $zrokEnv + $zrokWait + $zrokDetach
    Log = 'Get-Content (Join-Path $HOME ''.atrium-zrok\room\room.log'') -Tail 300' }
  'm1mini' = @{ GOOS = 'darwin'; GOARCH = 'arm64'; Dest = '.local/bin/atrium.new'
    Swap = 'zsh -lic "~/.local/bin/atrium stop ; ' + $macWait + $macWaitStore +
      'mv -f ~/.local/bin/atrium ~/.local/bin/atrium.prev && ' +
      'mv -f ~/.local/bin/atrium.new ~/.local/bin/atrium && chmod +x ~/.local/bin/atrium && ' + $macDetach + '"'
    Retry = 'zsh -lic "' + $macWait + $macWaitStore + $macDetach + '"'
    Log = 'tail -n 300 ~/.atrium/room/room.log' }
}

# Get-RoomLink here shadows live-common's, which reads the local room.err. For a remote room, $RoomName is that room's
# name while Wait-RoomAttached runs, and the verdict is the hub's: attached on a connection made after the swap started,
# on the commit just built. Never an ssh call, which can hang or fail while the room is fine (2026-10-14, m1mini). The
# room's own log is read over ssh only to explain a failure, in Show-RoomLog.
$localRoomLink = ${function:Get-RoomLink}
$script:swapStart = Get-Date
$script:sshHosts = @{}
function Get-RoomLink {
  if (-not $remote.Contains($RoomName)) { return & $localRoomLink }
  if (Test-RoomBackOnHub $RoomName $script:swapStart $commit) { return 'up' }
  return 'down'
}
function Show-RoomLog([string]$Name) {
  $r = Invoke-Ssh "log tail ${Name}" @($script:sshHosts[$Name], $remote[$Name].Log) $diagCap
  if ($r.TimedOut) { Say "${Name}: log tail hung, skipped. the verdict is the hub's, not the log's"; return }
  ($r.Out -split "`n" | Select-Object -Last 15) | ForEach-Object { Say "  ${Name} log: $_" }
}
foreach ($name in $remoteNames) {
  if ($want -notcontains $name) { continue }
  $r = $remote[$name]
  $hubRoom = try { (Invoke-RestMethod $HubRooms -TimeoutSec 3).rooms | Where-Object { $_.name -eq $name } | Select-Object -First 1 } catch { $null }
  $sshName = if ($r.SshName) { $r.SshName } else { $name }
  $sshUser = Get-RoomSshUser $sshName $hubRoom
  $route = Resolve-SshRoute $sshName {
    param($h)
    if (-not $h.EndsWith('.atrium')) { return Invoke-Capped $ssh ($bareOpts + @($h, 'echo ok')) $bareCap }
    Invoke-Ssh "ssh $h probe" @($h, 'echo ok') $probeCap
  } $sshUser
  if (-not $route.Host) {
    if (@($route.Results | Where-Object { $_.TimedOut }).Count -gt 0 -and (Get-SshFailure $route $sshUser) -notmatch 'refused') {
      $results[$name] = Get-HungMsg 'ssh probe' $probeCap; Say "${name}: probe hung ($($route.Tried -join ', '))"; continue
    }
    $results[$name] = Get-SshFailure $route $sshUser
    Say "${name}: $($results[$name] -replace '^failed - ', ''), no route answered ($($route.Tried -join ', '))"
    continue
  }
  $sshHost = $route.Host
  $script:sshHosts[$name] = $sshHost
  Say "${name}: ssh route $sshHost"
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
  $script:scpRes = $null
  Invoke-Step "scp $out -> ${name}:$($r.Dest)" {
    $script:scpRes = Invoke-CappedRetry "scp $name" $scp (@('-q') + $sshOpts + @($out, "${sshHost}:$($r.Dest)")) $scpCap
  }
  if (-not $WhatIf -and $scpRes.TimedOut) { $results[$name] = Get-HungMsg 'scp' $scpCap; continue }
  if (-not $WhatIf -and $scpRes.Code -ne 0) { $results[$name] = 'failed - scp failed'; continue }
  # What a restart does to this room's runners, read before it goes. A room that keeps them gets no hold, and the
  # result says so, so a deploy that was meant to leave agents running can be told from one that did not.
  $keeps = (-not $WhatIf) -and (Test-RunnersSurvive 'http://127.0.0.1:7778/v1/preflight' @{ 'X-Atrium-Room' = $name })
  Say "${name}: runners survive the restart: $keeps"
  $start = Get-Date
  $script:swapStart = $start
  $swapOut = @()
  $swapCap = $portWait + 120
  $swapRes = $null
  $os = if ($r.GOOS -eq 'windows') { 'windows' } else { 'mac' }
  Invoke-Step "ssh ${name}: stop, wait up to ${portWait}s for the port, swap (old kept as atrium.prev), start" {
    $script:swapRes = Start-DetachedSwap $name $sshHost $os $r.Swap $start $swapCap
    $script:swapOut = @($swapRes.Out -split "`r?`n" | Where-Object { $_ -and $_ -ne $SwapDone })
    $swapOut | ForEach-Object { Say "  ${name}: $_" }
  }
  if ($WhatIf) { $results[$name] = 'success (WhatIf)'; continue }
  # A swap call that hung is not a verdict either way: the hub decides below whether the room came back.
  if ($swapRes.Out -like 'DETACH-FAILED*') { $results[$name] = 'failed - the swap could not be started on the room'; continue }
  $swapHung = $swapRes.TimedOut
  if ($swapHung) { Say "${name}: the swap output was not seen within ${swapCap}s. checking the hub for the room anyway" }
  if ($swapOut -contains $portTimeout) {
    $results[$name] = "failed - the room still held its port after ${portWait}s, so nothing was swapped or started"
    continue
  }
  $storeLine = $swapOut | Where-Object { $_ -like "$StoreTimeout*" } | Select-Object -First 1
  if ($storeLine) {
    $results[$name] = "failed - the old room still held its store after ${StoreBudget}s ($($storeLine -replace "^$StoreTimeout ", 'pid ')), so the new room was not started"
    continue
  }
  # The old room can outlive the port check by a moment and answer the detach: wait for the port again, detach once more.
  if (($swapOut -join "`n") -match 'left it running|not attached') {
    Say "${name}: detach found the old room still running. retrying once"
    $retryRes = Start-DetachedSwap $name $sshHost $os $r.Retry $start $swapCap
    $retryOut = @($retryRes.Out -split "`r?`n" | Where-Object { $_ -and $_ -ne $SwapDone })
    $retryOut | ForEach-Object { Say "  ${name}: $_" }
    if ($retryRes.Out -like 'DETACH-FAILED*') { $results[$name] = 'failed - the swap retry could not be started on the room'; continue }
    $retryStore = $retryOut | Where-Object { $_ -like "$StoreTimeout*" } | Select-Object -First 1
    if ($retryStore) {
      $results[$name] = "failed - the old room still held its store after ${StoreBudget}s more ($($retryStore -replace "^$StoreTimeout ", 'pid ')), so it was not started"
      continue
    }
    if ($retryOut -contains $portTimeout) {
      $results[$name] = "failed - the room still held its port after ${portWait}s more, so it was not started"
      continue
    }
  }
  $saved = $RoomName
  $RoomName = $name
  $back = Wait-RoomAttached $start $ReattachBudget
  $RoomName = $saved
  if (-not $back) { Show-RoomLog $name }
  $results[$name] = if (-not $back -and $swapHung) { Get-HungMsg 'ssh swap' $swapCap }
    elseif (-not $back) { "failed - restarted but not back on the hub after ${ReattachBudget}s" }
    elseif (-not (Test-Unheld 'http://127.0.0.1:7778/v1/hold' @{ 'X-Atrium-Room' = $name })) { $heldMsg }
    elseif ($keeps) { 'success (runners kept)' }
    else { 'success' }
}

# 3. The hub, with claude-sg4 when it is named. Run as child scripts, so their own checks and reverts apply.
if ($want -contains 'claude-sg4') {
  & "$PSScriptRoot\deploy-batch.ps1" -WhatIf:$WhatIf -Immediate:$Immediate -KillRunners:$KillRunners -RoomOnly:(-not ($want -contains 'hub'))
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
