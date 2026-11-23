# A throwaway hub and N rooms on this machine, for trying `atrium move` between rooms. docs/rnd/room-move-spike.md.
#
#     pwsh -File scripts/move-rig.ps1 -Start                  # hub + rooms ra, rb, rc
#     pwsh -File scripts/move-rig.ps1 -Seed ra -Card w1       # a card on ra: a branch, a commit, a dirty file, a brief
#     pwsh -File scripts/move-rig.ps1 -Status
#     pwsh -File scripts/move-rig.ps1 -KillHub                # the hub process only. rooms keep running
#     pwsh -File scripts/move-rig.ps1 -StartHub               # the hub again, on the state it had
#     pwsh -File scripts/move-rig.ps1 -WipeHub                # the hub killed and its store deleted. CA and certs stay
#     pwsh -File scripts/move-rig.ps1 -StartHub -Readopt      # the hub again, writing down each room that proves itself
#     pwsh -File scripts/move-rig.ps1 -Cli 'move w1 --from ra --to rb --url http://127.0.0.1:7960'   # the driver, its args in one string
#     pwsh -File scripts/move-rig.ps1 -Stop                   # everything, and the data dir deleted
#
# Nothing of the live atrium is touched: its own ports (hub board -Port, link +1, room i board +10i+2, agents +10i+3),
# its own data dir (-Data), every room --isolated with its own ATRIUM_LOCATION and its own HOME, so each room has its
# own ~/.claude and its own git identity, the way two machines would. The binary is build.claude/atrium.exe of this
# checkout, copied into the data dir. Build it first: `go build -o build.claude/atrium.exe ./cmd/atrium`.
#
# The runner is `fake`, a pwsh script that behaves like claude where a move can see it: it writes a transcript under
# ~/.claude/projects/<encoded cwd>/<session>.jsonl, reports SessionStart to its room, takes -Resume <session> and refuses
# one it cannot find (as `claude --resume` does), takes a prompt, and exits on ctrl+c. No credentials, no tokens.
param(
  [switch]$Start, [switch]$Stop, [switch]$Status, [switch]$KillHub, [switch]$StartHub, [switch]$WipeHub, [switch]$Readopt,
  [string]$Seed = '', [string]$Card = '',
  [string[]]$Rooms = @('ra', 'rb', 'rc'),
  [int]$Port = 7960,
  [string]$Data = '',
  [string]$Cli = ''
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$Repo = (Resolve-Path "$PSScriptRoot/..").Path
if (-not $Data) { $Data = Join-Path (Split-Path $Repo) 'move-rig' }
$Data = [IO.Path]::GetFullPath($Data).Replace('\', '/')
$Board = "127.0.0.1:$Port"
$Link = "127.0.0.1:$($Port + 1)"
$Pwsh = (Get-Process -Id $PID).Path
$Exe = "$Data/bin/atrium.exe"

function Say($m) { Write-Host "[move-rig $Port] $m" }
function RoomIndex([string]$r) { [array]::IndexOf($Rooms, $r) + 1 }
function RoomHttp([string]$r) { "127.0.0.1:$($Port + 10 * (RoomIndex $r) + 2)" }
function RoomAgent([string]$r) { "127.0.0.1:$($Port + 10 * (RoomIndex $r) + 3)" }

function Wait-Until([scriptblock]$what, [int]$seconds = 30, [string]$label = 'it') {
  $deadline = (Get-Date).AddSeconds($seconds)
  while ((Get-Date) -lt $deadline) {
    try { if (& $what) { return } } catch {}
    Start-Sleep -Milliseconds 250
  }
  throw "timed out after ${seconds}s waiting for $label"
}

function Api([string]$at, [string]$method, [string]$path, $body = $null) {
  $call = @{ Method = $method; Uri = "http://$at$path"; TimeoutSec = 30 }
  if ($null -ne $body) { $call.Body = ($body | ConvertTo-Json -Depth 8); $call.ContentType = 'application/json' }
  Invoke-RestMethod @call
}

function Pids { if (Test-Path "$Data/pids.json") { Get-Content "$Data/pids.json" -Raw | ConvertFrom-Json -AsHashtable } else { @{} } }
function Save-Pids($p) { $p | ConvertTo-Json | Set-Content "$Data/pids.json" }

# Through a hidden cmd, never Start-Process -Redirect*, so a caller reading this script through a pipe does not wait on
# the rig for as long as it runs. See scripts/mock-live.ps1.
function Start-Atrium([string[]]$argv, [string]$name) {
  $line = '/d /s /c ""' + $Exe + '" ' + ($argv -join ' ') + " >> `"$Data/$name.log`" 2>&1`""
  $cmd = Start-Process "$env:SystemRoot/System32/cmd.exe" -ArgumentList $line -WindowStyle Hidden -PassThru
  Wait-Until {
    $known = @((Pids).Values)
    $script:child = Get-Process -Name atrium -ErrorAction SilentlyContinue |
      Where-Object { $_.Path -and $_.Path.Replace([char]92, '/') -eq $Exe -and $known -notcontains $_.Id } | Select-Object -First 1
    $null -ne $script:child
  } 15 "atrium $name to start"
  $script:child.Id
}

function Clear-AtriumEnv { Get-ChildItem env: | Where-Object Name -like 'ATRIUM_*' | ForEach-Object { Remove-Item "env:$($_.Name)" } }

function Run-Hub {
  Clear-AtriumEnv
  $env:ATRIUM_LOCATION = "$Data/hub/daemon.json"
  if ($Readopt) { $env:ATRIUM_HUB_READOPT = '1' }
  $env:ATRIUM_SHARED_LOCATION = '-'
  $real = $env:USERPROFILE, $env:HOME
  New-Item -ItemType Directory -Force "$Data/hub-home" | Out-Null
  $env:USERPROFILE = $env:HOME = "$Data/hub-home"
  $id = Start-Atrium @('run', '--no-room', '--addr', $Board, '--link', $Link, '--atrium-dir', "$Data/hub") 'hub'
  $env:USERPROFILE, $env:HOME = $real
  $p = Pids; $p.hub = $id; Save-Pids $p
  Wait-Until { (Invoke-WebRequest "http://$Board/_hub/health" -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200 } 30 'the hub'
  if (Test-Path "$Data/hub/zrok-share") { throw "the rig hub reserved a zrok share. run -Stop, then 'atrium rooms retire-share --atrium-dir $Data/hub'" }
  Say "hub up at $Board (pid $id)"
}

function Stop-ByPid([int]$id) {
  $p = Get-Process -Id $id -ErrorAction SilentlyContinue
  # By pid and path, never by image name: every hook on this machine is an atrium.exe.
  if ($p -and $p.Path -and $p.Path.Replace('\', '/') -like "$Data/bin/*") {
    Stop-Process -Id $id -Force -ErrorAction SilentlyContinue
    try { $p.WaitForExit(10000) | Out-Null } catch {}
  }
}

function Fake-Runner {
  @'
param([string]$Resume = '', [string]$Prompt = '', [string]$Agent = '')
$ProgressPreference = 'SilentlyContinue'
$cwd = (Get-Location).Path
$enc = -join ($cwd.ToCharArray() | ForEach-Object { if ($_ -match '[A-Za-z0-9]') { $_ } else { '-' } })
$proj = Join-Path $HOME ".claude/projects/$enc"
New-Item -ItemType Directory -Force $proj | Out-Null
if ($Resume) {
  $session = $Resume
  if (-not (Test-Path "$proj/$session.jsonl")) { Write-Host "No conversation found with session ID: $session"; exit 1 }
  Write-Host "== resumed $session in $cwd. history:"
  Get-Content "$proj/$session.jsonl" | ForEach-Object { Write-Host "   $_" }
} else {
  $session = [guid]::NewGuid().ToString()
  Write-Host "== new conversation $session in $cwd"
}
$line = @{ type = 'user'; cwd = $cwd; session = $session; at = (Get-Date).ToString('o'); text = $(if ($Prompt) { $Prompt } else { 'start' }) }
($line | ConvertTo-Json -Compress) | Add-Content "$proj/$session.jsonl"
if ($Prompt) { Write-Host "prompt: $Prompt" }
$body = @{ agent = $env:ATRIUM_AGENT_NAME; event = 'start'; runner = 'fake'; cwd = $cwd.Replace('\', '/'); pid = $PID;
  task_id = $env:ATRIUM_TASK_ID; resume = $session; source = $(if ($Resume) { 'resume' } else { 'startup' }); resumable = $true }
try { Invoke-RestMethod -Method Post "http://$Agent/session" -ContentType 'application/json' -Body ($body | ConvertTo-Json) -TimeoutSec 5 | Out-Null }
catch { Write-Host "session start not reported: $_" }
while ($true) { Start-Sleep -Seconds 5 }
'@ | Set-Content "$Data/bin/fake-claude.ps1"
}

function Run-Room([string]$r) {
  $rd = "$Data/$r"
  New-Item -ItemType Directory -Force "$rd/state", "$rd/home", "$rd/src", "$rd/wt" | Out-Null
  "[user]`n  name = $r`n  email = $r@rig.invalid`n[safe]`n  directory = *" | Set-Content "$rd/home/.gitconfig"
  $printed = & $Exe rooms add $r --atrium-dir "$Data/hub" --link $Link 2>&1 | Out-String
  $token = [regex]::Match($printed, 'atrium room join (\S+)').Groups[1].Value
  if (-not $token) { throw "atrium rooms add $r printed no join string:`n$printed" }
  Clear-AtriumEnv
  $env:ATRIUM_LOCATION = "$rd/state/daemon.json"
  $env:ATRIUM_SHARED_LOCATION = '-'
  $real = $env:USERPROFILE, $env:HOME
  $env:USERPROFILE = $env:HOME = "$rd/home"
  $id = Start-Atrium @('room', 'join', $token, '--isolated', '--dir', "$rd/state", '--db', "$rd/state/atrium.db",
    '--http', (RoomHttp $r), '--agent', (RoomAgent $r)) $r
  $env:USERPROFILE, $env:HOME = $real
  $p = Pids; $p[$r] = $id; Save-Pids $p
  Wait-Until { (Invoke-WebRequest "http://$(RoomHttp $r)/v1/harnesses" -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200 } 60 "room $r"
  $at = RoomHttp $r
  Api $at PUT '/v1/harnesses/fake' @{ id = 'fake'; label = 'fake claude'; enabled = $true; cmd = $Pwsh; launch_mode = 'pty'
    args = @('-NoLogo', '-NoProfile', '-File', "$Data/bin/fake-claude.ps1", '-Agent', (RoomAgent $r))
    resume_args = @('-NoLogo', '-NoProfile', '-File', "$Data/bin/fake-claude.ps1", '-Agent', (RoomAgent $r), '-Resume', '{resume}')
    prompt_args = @('-Prompt', '{prompt}') } | Out-Null
  Invoke-RestMethod -Method Post "http://$at/v1/settings" -ContentType 'application/json' -Body '{"global_auto":false}' | Out-Null
  # Each room has its own clone of the one origin, under its own card root, as two machines would.
  $clone = "$rd/src/acme/widgets"
  if (-not (Test-Path $clone)) {
    git clone --quiet "$Data/origin/widgets.git" $clone
    if ($LASTEXITCODE -ne 0) { throw "clone for $r failed" }
  }
  Api $at PUT '/v1/providers/github' @{ name = 'github'; kind = 'git'; host = 'github.com'; enabled = $true
    root = "$rd/src"; worktrees = $true; worktree_root = "$rd/wt" } | Out-Null
  Say "room $r up: board $at, agents $(RoomAgent $r), cards under $rd/wt (pid $id)"
}

if ($Stop) {
  $p = Pids
  foreach ($r in $Rooms) {
    try { Invoke-RestMethod -Method Post "http://$(RoomHttp $r)/v1/shutdown" -TimeoutSec 10 | Out-Null } catch {}
  }
  foreach ($k in @($p.Keys)) { if ($k -ne 'hub') { Stop-ByPid $p[$k] } }
  if ($p.hub) { Stop-ByPid $p.hub }
  if (Test-Path $Data) { Wait-Until { Remove-Item -Recurse -Force $Data; -not (Test-Path $Data) } 30 'the data dir to delete' }
  Say "stopped, and $Data is gone"
  exit 0
}

if ($Cli) {
  Clear-AtriumEnv
  New-Item -ItemType Directory -Force "$Data/cli-home" | Out-Null
  $env:USERPROFILE = $env:HOME = "$Data/cli-home"
  & "$Data/bin/atrium-cli.exe" @($Cli -split '\s+' | Where-Object { $_ })
  exit $LASTEXITCODE
}

if ($KillHub) { $p = Pids; Stop-ByPid $p.hub; Say "hub pid $($p.hub) killed. rooms left running"; exit 0 }
if ($StartHub) { Run-Hub; exit 0 }
if ($WipeHub) {
  $p = Pids; if ($p.hub) { Stop-ByPid $p.hub }
  Get-ChildItem "$Data/hub" -Filter 'hub.db*' | Remove-Item -Force
  if (Test-Path "$Data/hub/backups") { Remove-Item -Recurse -Force "$Data/hub/backups" }
  Say "hub killed and its store deleted. kept: $((Get-ChildItem "$Data/hub").Name -join ' ')"
  exit 0
}

if ($Status) {
  try { $rs = (Api $Board GET '/_hub/rooms').rooms; Say "hub: $(@($rs).Count) rooms: $((@($rs) | ForEach-Object { "$($_.name)=$(if ($_.last_beat) { 'attached' } else { 'gone' })" }) -join ' ')" }
  catch { Say "hub: not answering" }
  foreach ($r in $Rooms) {
    try {
      $ts = (Api (RoomHttp $r) GET '/v1/tasks').tasks
      Say "room ${r}:"
      foreach ($t in @($ts)) {
        Write-Host ("   {0} {1,-10} {2,-8} pid={3} alias={4} moved_to={5} moved_from={6} resume={7} cwd={8}" -f $t.id, $t.title, $t.status, $t.pid, $t.alias, $t.moved_to, $t.moved_from, $t.resume_id, $t.worktree)
      }
    } catch { Say "room ${r}: not answering" }
  }
  exit 0
}

if ($Seed) {
  if (-not $Card) { throw "-Seed <room> needs -Card <name>" }
  $rd = "$Data/$Seed"
  $clone = "$rd/src/acme/widgets"
  $wt = "$rd/wt/acme/widgets/claude-$Card"
  git -C $clone worktree add --quiet -b "claude/$Card" $wt origin/main
  if ($LASTEXITCODE -ne 0) { throw "worktree for $Card failed" }
  "work by $Card" | Set-Content "$wt/$Card.txt"
  git -C $wt add "$Card.txt"
  git -c user.name=$Seed -c user.email=x@rig.invalid -C $wt commit --quiet -m "${Card}: first commit"
  "not committed yet" | Set-Content "$wt/dirty.txt"
  "# brief for $Card`nmove me" | Set-Content "$wt/BRIEF.md"
  $real = $env:USERPROFILE, $env:HOME
  $t = Api (RoomHttp $Seed) POST '/v1/launch' @{ harness = 'fake'; cwd = $wt; title = $Card; why = 'move rig card'
    prompt = "hello from $Seed"; tags = @('rig'); repo = 'widgets'; org = 'acme'; host = 'github.com' }
  $env:USERPROFILE, $env:HOME = $real
  Api (RoomHttp $Seed) PATCH "/v1/tasks/$($t.id)" @{ alias = $Card } | Out-Null
  Say "card $Card on $Seed -> $($t.id) in $wt"
  exit 0
}

if ($Start) {
  foreach ($a in @($Board, $Link) + ($Rooms | ForEach-Object { RoomHttp $_; RoomAgent $_ })) {
    $hp = $a.Split(':')
    $busy = $false
    try { $c = [Net.Sockets.TcpClient]::new(); $c.Connect($hp[0], [int]$hp[1]); $c.Close(); $busy = $true } catch {}
    if ($busy) {
      throw "port $($hp[1]) is in use. -Stop the rig on -Port $Port, or pick another -Port"
    }
  }
  New-Item -ItemType Directory -Force "$Data/bin", "$Data/origin" | Out-Null
  Copy-Item "$Repo/build.claude/atrium.exe" $Exe -Force
  Copy-Item "$Repo/build.claude/atrium.exe" "$Data/bin/atrium-cli.exe" -Force
  Fake-Runner
  $o = "$Data/origin/widgets.git"
  if (-not (Test-Path $o)) {
    git init --quiet --bare -b main $o
    $seedDir = "$Data/origin/seed"
    git init --quiet -b main $seedDir
    "# widgets" | Set-Content "$seedDir/README.md"
    git -C $seedDir add README.md
    git -c user.name=rig -c user.email=rig@rig.invalid -C $seedDir commit --quiet -m 'widgets: start'
    git -C $seedDir push --quiet $o main
  }
  Save-Pids @{}
  Run-Hub
  foreach ($r in $Rooms) { Run-Room $r }
  Wait-Until { @((Api $Board GET '/_hub/rooms').rooms | Where-Object { $_.last_beat }).Count -ge $Rooms.Count } 60 'every room to attach'
  Say "up. data in $Data"
  exit 0
}

Write-Host "say -Start, -Seed <room> -Card <name>, -Status, -KillHub, -StartHub or -Stop"
