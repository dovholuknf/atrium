# Rebuild and restart a never-run atrium in docker on agentwsl, its board on sg4 http://127.0.0.1:7920.
#
# Each run builds the linux binary from -Ref in a detached worktree, builds a fresh image and replaces the container,
# so the board is always a first launch: empty data dir, default settings, claude installed and logged out.
# Docker runs in agent-ubuntu over ssh because wslc on sg4 fails with 0x800703fa (a registry key marked for deletion)
# in the WSL service. WSL's localhost relay carries the published 127.0.0.1 port to sg4. Touches no live hub or room.
#
#   pwsh -NoProfile -File scripts\live\first-run-demo.ps1 [-Ref claude/main] [-Port 7920]
param(
  [string]$Ref = 'claude/main',
  [ValidateRange(7900, 7999)][int]$Port = 7920,
  [string]$Name = 'atrium-first-run',
  [string]$Src = 'D:/tmp/atrium-first-run/src',
  [string]$SshHost = 'agentwsl'
)

$ErrorActionPreference = 'Stop'
function Run([string]$what, [scriptblock]$cmd) {
  Write-Host "== $what"
  & $cmd
  if ($LASTEXITCODE) { throw "$what failed (exit $LASTEXITCODE)" }
}

$repo = git -C $PSScriptRoot rev-parse --show-toplevel
if (Test-Path "$Src/.git") {
  Run "checkout $Ref in $Src" { git -C $Src checkout -q --detach --force $Ref }
} else {
  New-Item -ItemType Directory -Force (Split-Path $Src) | Out-Null
  Run "worktree $Src at $Ref" { git -C $repo worktree add -q --detach $Src $Ref }
}
$commit = git -C $Src rev-parse HEAD
$ver = & "$Src/scripts/version.ps1"
$pkg = 'github.com/dovholuknf/atrium/internal/cli'
$codeId = & go -C $Src run ./tools/codeid
$ldflags = "-X github.com/dovholuknf/atrium/internal/api.CodeID=$codeId -X $pkg.Version=$ver -X $pkg.Commit=$commit -X $pkg.Tree=clean"
$ctx = "$Src/build.claude/first-run"
New-Item -ItemType Directory -Force $ctx | Out-Null
Copy-Item "$PSScriptRoot/first-run-demo.Dockerfile" "$ctx/Dockerfile" -Force

$saved = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
$env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
try {
  Run "build linux atrium $ver ($commit)" { go -C $Src build -ldflags $ldflags -o "$ctx/atrium" ./cmd/atrium }
} finally {
  $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $saved
}

# -4: localhost on sg4 resolves to ::1 first, and WSL relays the ssh port on IPv4 only.
$remote = "atrium-first-run"
Run "stage on $SshHost" { ssh -4 $SshHost "rm -rf ~/$remote && mkdir -p ~/$remote" }
Run "copy build context" { scp -4 -q "$ctx/Dockerfile" "$ctx/atrium" "${SshHost}:$remote/" }
$sh = @"
set -e
cd ~/$remote
chmod 0755 atrium
docker build -q -t ${Name}:latest .
docker rm -f $Name >/dev/null 2>&1 || true
docker run -d --name $Name --hostname first-run --restart unless-stopped -p 127.0.0.1:${Port}:7920 ${Name}:latest
"@
Run "image and container $Name" { ssh -4 $SshHost ($sh -replace "`r", '') }

$url = "http://127.0.0.1:$Port/"
$deadline = (Get-Date).AddSeconds(90)
while ($true) {
  try {
    $r = Invoke-WebRequest $url -UseBasicParsing -TimeoutSec 5
    if ($r.StatusCode -eq 200) { break }
  } catch { }
  if ((Get-Date) -gt $deadline) {
    ssh -4 $SshHost "docker logs --tail 40 $Name"
    throw "$url did not answer 200 within 90s"
  }
  Start-Sleep -Milliseconds 500
}
Write-Host "UP: $url  ($ver $commit)"
