# A throwaway but LIVE hub and room on this machine, built from any branch, with a seeded board to click around.
#
#     pwsh -File scripts/mock-live.ps1 -Branch claude/main
#     pwsh -File scripts/mock-live.ps1 -Branch claude/pr-1102-batch -Port 7960
#     pwsh -File scripts/mock-live.ps1 -Port 7960 -Stop
#
# It touches nothing of the running atrium, the same way scripts/walkthrough/hubroom.spec.js does: its own ports
# (board -Port, link +1, room board +2, room agents +3), its own data dir under .uncommitted/mock/<port>, and the
# room is --isolated with its own ATRIUM_LOCATION, so no hook on this machine is aimed at it. The branch is checked out
# as a worktree under .uncommitted/mock-src/ and built there with `make build` into its build.claude/.
#
# The seed cards run pwsh scripts under the mock's own `mock` harness, never claude, so they cost no tokens. They say
# needs-input, needs-permission and done through the room's agent port, as a real session's hooks would.
#
# -Stop asks the room to shut down (a kill is not a stop), ends the hub by pid, and deletes the data dir.
param(
  [string]$Branch = 'claude/main',
  [int]$Port = 7950,
  [switch]$Stop
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'D:/git/github/dovholuknf/atrium'
$Data = "$Repo/.uncommitted/mock/$Port"
$Board = "127.0.0.1:$Port"
$Link = "127.0.0.1:$($Port + 1)"
$RoomHttp = "127.0.0.1:$($Port + 2)"
$RoomAgent = "127.0.0.1:$($Port + 3)"
$Pwsh = (Get-Process -Id $PID).Path

function Say($m) { Write-Host "[mock $Port] $m" }

function Wait-Until([scriptblock]$what, [int]$seconds = 30, [string]$label = 'it') {
  $deadline = (Get-Date).AddSeconds($seconds)
  while ((Get-Date) -lt $deadline) {
    try { if (& $what) { return } } catch {}
    Start-Sleep -Milliseconds 250
  }
  throw "timed out after ${seconds}s waiting for $label"
}

function Api([string]$method, [string]$path, $body = $null) {
  $call = @{ Method = $method; Uri = "http://$Board$path"; TimeoutSec = 30 }
  if ($null -ne $body) { $call.Body = ($body | ConvertTo-Json -Depth 8); $call.ContentType = 'application/json' }
  Invoke-RestMethod @call
}

function Stop-Mock {
  $state = "$Data/pids.json"
  if (Test-Path $state) {
    $pids = Get-Content $state -Raw | ConvertFrom-Json
    try { Invoke-RestMethod -Method Post "http://$RoomHttp/v1/shutdown" -TimeoutSec 10 | Out-Null } catch {}
    $roomProc = Get-Process -Id $pids.room -ErrorAction SilentlyContinue
    if ($roomProc) { try { $roomProc.WaitForExit(30000) | Out-Null } catch {} }
    # By pid, never by image name: every hook on this machine is an atrium.exe.
    foreach ($id in @($pids.room, $pids.hub)) {
      $p = Get-Process -Id $id -ErrorAction SilentlyContinue
      if ($p -and $p.Path -like "$($pids.exe_dir)*") { Stop-Process -Id $id -Force -ErrorAction SilentlyContinue }
    }
    foreach ($id in @($pids.room, $pids.hub)) {
      $p = Get-Process -Id $id -ErrorAction SilentlyContinue
      if ($p) { try { $p.WaitForExit(10000) | Out-Null } catch {} }
    }
  }
  if (Test-Path $Data) {
    Wait-Until { Remove-Item -Recurse -Force $Data; -not (Test-Path $Data) } 30 "the data dir to delete"
  }
  Say "stopped, and $Data is gone"
}

if ($Stop) { Stop-Mock; exit 0 }

foreach ($a in @($Board, $Link, $RoomHttp, $RoomAgent)) {
  $hp = $a.Split(':')
  $busy = Get-NetTCPConnection -LocalPort ([int]$hp[1]) -State Listen -ErrorAction SilentlyContinue
  if ($busy) { throw "port $($hp[1]) is in use. stop that mock with -Port $Port -Stop, or pick another -Port" }
}

# ── the build ────────────────────────────────────────────
$slug = ($Branch -replace '[^A-Za-z0-9._-]', '-')
$Src = "$Repo/.uncommitted/mock-src/$slug"
Push-Location $Repo
try {
  git fetch --quiet hub "+refs/heads/${Branch}:refs/remotes/hub/$Branch" 2>$null
  $ref = "hub/$Branch"
  git rev-parse --verify --quiet "$ref^{commit}" *> $null
  if ($LASTEXITCODE -ne 0) { $ref = $Branch }
  $sha = (git rev-parse --verify "$ref^{commit}").Trim()
  if (-not (Test-Path $Src)) {
    git worktree add --quiet --detach $Src $sha
    if ($LASTEXITCODE -ne 0) { throw "git worktree add $Src failed" }
  }
} finally { Pop-Location }
Push-Location $Src
try {
  git checkout --quiet --detach $sha
  if ($LASTEXITCODE -ne 0) { throw "checkout $sha in $Src failed" }
  $make = (Get-Command make -ErrorAction SilentlyContinue).Source
  if (-not $make) { $make = (Get-Command mingw32-make -ErrorAction SilentlyContinue).Source }
  if (-not $make) {
    $make = Get-ChildItem 'C:/Program Files/JetBrains' -Recurse -Filter mingw32-make.exe -ErrorAction SilentlyContinue |
      Select-Object -First 1 -ExpandProperty FullName
  }
  if (-not $make) { throw "no make or mingw32-make on this machine" }
  # Make's recipe lines are POSIX: Git's sh first, or the WSL bash.exe in System32 answers instead.
  $env:PATH = "C:/Program Files/Git/usr/bin;C:/Program Files/Git/bin;" + $env:PATH
  Say "building $Branch at $($sha.Substring(0, 9)) in $Src"
  & $make build 2>&1 | ForEach-Object { "  $_" } | Write-Host
  if ($LASTEXITCODE -ne 0) { throw "make build failed in $Src" }
} finally { Pop-Location }

# The binary runs from a copy in the data dir, so a rebuild of the branch never meets a running exe.
New-Item -ItemType Directory -Force "$Data/room", "$Data/bin", "$Data/cards" | Out-Null
Copy-Item "$Src/build.claude/atrium.exe" "$Data/bin/atrium.exe"
$Exe = (Resolve-Path "$Data/bin/atrium.exe").Path

# ── hub and room ─────────────────────────────────────────
# Nothing of the session running this script reaches the mock: its own address file, no shared one.
Get-ChildItem env: | Where-Object Name -like 'ATRIUM_*' | ForEach-Object { Remove-Item "env:$($_.Name)" }
$env:ATRIUM_LOCATION = "$Data/room/daemon.json"
$env:ATRIUM_SHARED_LOCATION = '-'

# Through a hidden cmd by ShellExecute, never Start-Process -Redirect*: that hands the caller's stdout to atrium, and
# a caller reading this script through a pipe then waits on the mock for as long as it runs.
function Start-Atrium([string[]]$argv, [string]$name) {
  $line = '/d /s /c ""' + $Exe + '" ' + ($argv -join ' ') + " > `"$Data/$name.log`" 2>&1`""
  $cmd = Start-Process "$env:SystemRoot/System32/cmd.exe" -ArgumentList $line -WindowStyle Hidden -PassThru
  Wait-Until {
    $script:child = Get-CimInstance Win32_Process -Filter "ParentProcessId=$($cmd.Id) AND Name='atrium.exe'"
    $null -ne $script:child
  } 15 "atrium $name to start"
  Get-Process -Id $script:child.ProcessId
}

# The hub gets an empty home: with the real one it finds this machine's zrok env and reserves a share on the account.
$realProfile, $realHome = $env:USERPROFILE, $env:HOME
New-Item -ItemType Directory -Force "$Data/hub-home" | Out-Null
$env:USERPROFILE = $env:HOME = (Resolve-Path "$Data/hub-home").Path
$hub = Start-Atrium @('run', '--no-room', '--addr', $Board, '--link', $Link, '--atrium-dir', "$Data/hub") 'hub'
$env:USERPROFILE, $env:HOME = $realProfile, $realHome
@{ hub = $hub.Id; room = 0; exe_dir = (Split-Path $Exe) } | ConvertTo-Json | Set-Content "$Data/pids.json"
Wait-Until { (Invoke-WebRequest "http://$Board/_hub/health" -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200 } 30 'the hub'
function Assert-NoShare {
  if (-not (Test-Path "$Data/hub/zrok-share")) { return }
  Stop-Process -Id $hub.Id -Force -ErrorAction SilentlyContinue
  & $Exe rooms retire-share --atrium-dir "$Data/hub" | Out-Null
  Stop-Mock; throw "the mock hub reserved a zrok share. retired it and stopped"
}
Assert-NoShare

$printed = & $Exe rooms add mock --atrium-dir "$Data/hub" --link $Link 2>&1 | Out-String
$token = [regex]::Match($printed, 'atrium room join (\S+)').Groups[1].Value
if (-not $token) { throw "atrium rooms add printed no join string:`n$printed" }

$room = Start-Atrium @('room', 'join', $token, '--isolated', '--dir', "$Data/room", '--db', "$Data/room/atrium.db",
  '--http', $RoomHttp, '--agent', $RoomAgent) 'room'
@{ hub = $hub.Id; room = $room.Id; exe_dir = (Split-Path $Exe) } | ConvertTo-Json | Set-Content "$Data/pids.json"
Wait-Until { @((Api GET '/_hub/rooms').rooms).Count -ge 1 } 60 'the room to join the hub'
Assert-NoShare

# ── the seed ─────────────────────────────────────────────
$cardScript = "$Data/bin/card.ps1"
@'
param([string]$Kind = 'running', [string]$Agent, [string]$Title = 'work')
$ProgressPreference = 'SilentlyContinue'
$task = $env:ATRIUM_TASK_ID
$name = $env:ATRIUM_AGENT_NAME
function Post($path, $body) {
  Invoke-RestMethod -Method Post "http://$Agent$path" -ContentType 'application/json' -TimeoutSec 86400 `
    -Body ($body | ConvertTo-Json -Depth 5)
}
$e = [char]27
function Line($color, $text) { Write-Host "$e[${color}m$text$e[0m" }
Line 36 "== $Title =="
Line 90 "card $task, kind $Kind"
$steps = @('resolving modules', 'go vet ./...', 'go build ./cmd/atrium', 'running unit tests',
  'internal/store ok 0.41s', 'internal/daemon ok 3.82s', 'internal/api ok 1.12s', 'board units 212/212')
foreach ($s in $steps) { Line 32 "  ok  $s"; Start-Sleep -Milliseconds 120 }
switch ($Kind) {
  'running' {
    $n = 0
    while ($true) {
      $n++
      Line 33 ("[{0:HH:mm:ss}] tick {1}: still working on {2}" -f (Get-Date), $n, $Title)
      Start-Sleep -Seconds 4
    }
  }
  'input' {
    Line 35 "I need a decision before I go on."
    Post '/help' @{ agent = $name; task_id = $task; blocked = $true;
      ask = 'Should the retry budget be 3 or 5 attempts? Both pass the tests.' } | Out-Null
  }
  'permission' {
    Line 31 "about to run: git push origin claude/$Title"
    while ($true) {
      $r = Post '/permission' @{ agent = $name; task_id = $task; runner = 'mock'; tool = 'Bash'; pid = $PID; cwd = (Get-Location).Path;
        command = "git push origin claude/$Title" }
      Line 36 "decision: $($r.decision) $($r.reason)"
      if ($r.decision -eq 'approve') { Line 32 "pushed."; break }
    }
  }
  'shell' {
    Line 36 "a plain shell. type away."
  }
  'done' {
    Line 32 "all green. reporting done."
    Post '/finish' @{ agent = $name; task_id = $task; status = 'done'; sha = (git rev-parse HEAD).Trim();
      recap = "Finished $Title, tests pass." } | Out-Null
  }
}
'@ | Set-Content $cardScript

# launch_mode pty, or the room opens a real terminal window on the desktop for every card. -NoExit leaves a prompt.
Api PUT '/v1/harnesses/mock' @{ id = 'mock'; label = 'mock script'; enabled = $true; cmd = $Pwsh; launch_mode = 'pty';
  args = @('-NoLogo', '-NoProfile', '-NoExit', '-File', $cardScript) } | Out-Null
$h = @((Api GET '/v1/harnesses').harnesses) | Where-Object id -eq 'mock'
if (-not $h -or $h.launch_mode -ne 'pty') { Stop-Mock; throw "harness mock is not launch_mode pty, refusing to seed" }
# A new hub and a new room each approve everything on their own, and then no card ever waits on a permission.
foreach ($at in @($Board, $RoomHttp)) {
  Invoke-RestMethod -Method Post "http://$at/v1/settings" -ContentType 'application/json' `
    -Body '{"global_auto":false}' | Out-Null
}

$seed = @(
  @{ kind = 'running';    title = 'retry-budget';     project = 'acme/payments-api'; tags = @('origin:agent', 'p1') },
  @{ kind = 'running';    title = 'flaky-shard-fix';  project = 'acme/payments-api'; tags = @('ci') },
  @{ kind = 'input';      title = 'webhook-backoff';  project = 'acme/payments-api'; tags = @('origin:agent') },
  @{ kind = 'permission'; title = 'release-notes';    project = 'acme/web-board';    tags = @('release') },
  @{ kind = 'permission'; title = 'schema-v42';       project = 'acme/ledger';       tags = @('db', 'p1') },
  @{ kind = 'done';       title = 'dark-mode-polish'; project = 'acme/web-board';    tags = @('ui') },
  @{ kind = 'done';       title = 'audit-log-export'; project = 'acme/ledger';       tags = @('origin:agent') },
  @{ kind = 'running';    title = 'perf-sweep';       project = 'acme/ledger';       tags = @() },
  @{ kind = 'shell';      title = 'scratch-shell';    project = 'acme/web-board';    tags = @() }
)
foreach ($c in $seed) {
  $cwd = "$Data/cards/github/$($c.project)/$($c.title)"
  New-Item -ItemType Directory -Force $cwd | Out-Null
  # Its own repo, so the card reads its own branch rather than the atrium checkout this dir sits in.
  Push-Location $cwd
  git init --quiet -b "claude/$($c.title)"
  "# $($c.title)" | Set-Content README.md
  git add README.md
  git -c user.name=mock -c user.email=mock@example.invalid commit --quiet -m "Start $($c.title)"
  Pop-Location
  $req = @{ cwd = $cwd; title = $c.title; tags = $c.tags; why = "mock card: $($c.kind)"; harness = 'mock'
    args = @('-Kind', $c.kind, '-Agent', $RoomAgent, '-Title', $c.title) }
  $t = Api POST '/v1/launch' $req
  Say "card $($c.title) ($($c.kind)) -> $($t.id)"
}

try { Wait-Until {
  $ts = @((Api GET '/v1/tasks').tasks)
  (@($ts | Where-Object status -eq 'needs-permission').Count -ge 2) -and
  (@($ts | Where-Object status -eq 'needs-input').Count -ge 1) -and
  (@($ts | Where-Object status -eq 'done').Count -ge 2)
} 60 'the seed cards to reach their states' } catch { Say "warning: $_" }

Assert-NoShare
Say "board: http://$Board/"
Say "stop:  pwsh -File scripts/mock-live.ps1 -Port $Port -Stop"
