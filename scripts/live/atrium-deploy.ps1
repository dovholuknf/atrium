# Deploy claude/main everywhere in one command: build, the remote rooms, the hub with claude-sg4, then sg4-control.
#
#   pwsh -NoProfile -File D:\git\github\dovholuknf\atrium\scripts\live\atrium-deploy.ps1
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms hub              # hub only, rooms untouched
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -Rooms sg3,m1mini       # remote rooms only
#   pwsh -NoProfile -File ...\atrium-deploy.ps1 -WhatIf                 # say every step, change nothing
#
# -Rooms takes any of hub, claude-sg4, sg4-control, sg3, m1mini, or `local` (hub, claude-sg4, sg4-control, the
# default) or `all`. The order is fixed whatever order they are given in: remote rooms first, because they touch no
# terminal here, then the hub and claude-sg4 (deploy-batch.ps1, or deploy-hub-only.ps1 when claude-sg4 is not named),
# then sg4-control, which holds the orchestrator's terminal.
#
# RUN FROM AN ATRIUM CARD IT DETACHES ITSELF. Stopping sg4-control ends the terminal that started it, so when
# ATRIUM_TASK_ID is set and a room here is named, it starts itself again as a hidden process and returns at once. Follow
# it in C:\Users\claude\.atrium2\atrium-deploy.log.
#
# Remote rooms are built for their platform with the same stamp, copied over with scp, stopped with `atrium stop`, and
# swapped with the old binary kept as atrium.prev. m1mini runs through `zsh -lic`, or the room cannot find claude.
param([string[]]$Rooms = @('local'), [switch]$NoBuild, [switch]$WhatIf, [switch]$Immediate, [switch]$Detached)

$ErrorActionPreference = 'Continue'
$LiveTag = 'DEPLOY'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'atrium-deploy.log'

$known = 'hub', 'claude-sg4', 'sg4-control', 'sg3', 'm1mini'
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
$want = $want | Select-Object -Unique
$local = @($want | Where-Object { $_ -in 'hub', 'claude-sg4', 'sg4-control' })

if ($env:ATRIUM_TASK_ID -and $local.Count -and -not $Detached -and -not $WhatIf) {
  $argv = @('-NoProfile', '-File', $PSCommandPath, '-Rooms', ($want -join ','), '-Detached')
  if ($NoBuild) { $argv += '-NoBuild' }
  if ($Immediate) { $argv += '-Immediate' }
  Start-Process pwsh -ArgumentList $argv -WindowStyle Hidden
  Say "detached: deploying $($want -join ', '). follow $LiveLog"
  exit 0
}
if ($Detached) {
  Clear-SessionEnv
  Remove-Item Env:ATRIUM_NEW_BUILD -ErrorAction SilentlyContinue
  # Let the card that started this end its turn before anything stops.
  Start-Sleep -Seconds 10
}
Say "deploying $($want -join ', ')"

# 1. Build, once, from a clean claude/main. build-deploy.ps1 refuses anything else.
if (-not $NoBuild) {
  & "$PSScriptRoot\build-deploy.ps1" -WhatIf:$WhatIf
  if ($LASTEXITCODE -ne 0) { Say 'FATAL: build failed, nothing deployed'; exit 1 }
}
$commit = git -C $Repo rev-parse HEAD
$ver = git -C $Repo describe --tags --exact-match 2>$null
if (-not $ver) { $ver = 'dev' }
$pkg = 'github.com/dovholuknf/atrium/internal/cli'
$ldflags = "-X $pkg.Version=$ver -X $pkg.Commit=$commit -X $pkg.Tree=clean"

$scp = 'C:\WINDOWS\System32\OpenSSH\scp.exe'
$ssh = 'C:\WINDOWS\System32\OpenSSH\ssh.exe'
$failed = @()

# 2. Remote rooms. Each: cross-build, copy, stop, swap, start detached, check it is back on the hub.
$remote = @{
  'sg3'    = @{ GOOS = 'windows'; GOARCH = 'amd64'; Out = 'windows-amd64\atrium.exe'; Dest = 'atrium.new.exe'
    Swap = 'C:/Users/claude/.atrium/bin/atrium.exe stop ; ' +
      'Move-Item -Force C:/Users/claude/.atrium/bin/atrium.exe C:/Users/claude/.atrium/bin/atrium.prev.exe ; ' +
      'Move-Item -Force C:/Users/claude/atrium.new.exe C:/Users/claude/.atrium/bin/atrium.exe ; ' +
      'C:/Users/claude/.atrium/bin/atrium.exe room --detach --dir C:/Users/claude/.atrium/room ' +
      '--db C:/Users/claude/.atrium/atrium.db' }
  'm1mini' = @{ GOOS = 'darwin'; GOARCH = 'arm64'; Out = 'darwin-arm64\atrium'; Dest = '.local/bin/atrium.new'
    Swap = 'zsh -lic "~/.local/bin/atrium stop ; mv -f ~/.local/bin/atrium ~/.local/bin/atrium.prev && ' +
      'mv -f ~/.local/bin/atrium.new ~/.local/bin/atrium && chmod +x ~/.local/bin/atrium && ' +
      '~/.local/bin/atrium room --detach --dir /Users/claude/.atrium/room --db /Users/claude/.atrium/atrium.db ' +
      '--agent 127.0.0.1:7777 --http 127.0.0.1:7781"' }
}
foreach ($name in 'sg3', 'm1mini') {
  if ($want -notcontains $name) { continue }
  $r = $remote[$name]
  $out = Join-Path $Repo "build.claude\deploy\$($r.Out)"
  Invoke-Step "build $name ($($r.GOOS)/$($r.GOARCH)) -> $out" {
    $env:CGO_ENABLED = '0'; $env:GOOS = $r.GOOS; $env:GOARCH = $r.GOARCH
    $b = & go -C $Repo build -ldflags $ldflags -o $out ./cmd/atrium 2>&1
    $code = $LASTEXITCODE
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    if ($code -ne 0) { $b | ForEach-Object { Say "  $_" } }
  }
  if (-not $WhatIf -and -not (Test-Path $out)) { Say "$name FAILED: no build"; $failed += $name; continue }
  Invoke-Step "scp $out -> ${name}:$($r.Dest)" { & $scp -q $out "${name}:$($r.Dest)" }
  if (-not $WhatIf -and $LASTEXITCODE -ne 0) { Say "$name FAILED: scp"; $failed += $name; continue }
  $start = Get-Date
  Invoke-Step "ssh ${name}: stop, swap (old kept as atrium.prev), start" {
    & $ssh $name $r.Swap 2>&1 | ForEach-Object { Say "  ${name}: $_" }
  }
  if (-not $WhatIf) {
    $saved = $RoomName
    $RoomName = $name
    $back = Wait-RoomAttached $start
    $RoomName = $saved
    if ($back) { Say "$name attached and held since $($back.since)" } else { Say "$name FAILED: not back on the hub"; $failed += $name }
  }
}

# 3. The hub, with claude-sg4 when it is named. Run as child scripts, so their own checks and reverts apply.
if ($want -contains 'claude-sg4') {
  & "$PSScriptRoot\deploy-batch.ps1" -WhatIf:$WhatIf -Immediate:$Immediate
  if ($LASTEXITCODE -ne 0) { Say "hub + claude-sg4 FAILED (exit $LASTEXITCODE), see deploy-batch.log"; $failed += 'claude-sg4' }
} elseif ($want -contains 'hub') {
  & "$PSScriptRoot\deploy-hub-only.ps1" -WhatIf:$WhatIf -NoGate
  if ($LASTEXITCODE -ne 0) { Say "hub FAILED (exit $LASTEXITCODE), see deploy.log"; $failed += 'hub' }
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
  if (-not $WhatIf -and -not (Test-Up $CtlHealth)) { Say 'sg4-control FAILED: not answering'; $failed += 'sg4-control' }
}

if ($failed.Count) { Say "DEPLOY FAILED on: $($failed -join ', '). commit $($commit.Substring(0, 8))"; exit 1 }
Say "deploy done: $($want -join ', ') on commit $($commit.Substring(0, 8))"
