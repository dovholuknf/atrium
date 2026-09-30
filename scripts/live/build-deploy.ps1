# Build the binary the deploy scripts install, and ONLY from a clean claude/main in the main checkout.
#
# A deploy names the commit it ran, and that name is a lie when the build carried uncommitted code: the live binary
# said "commit af38dc1 (modified)" on 2026-09-29. So this refuses any other branch and any change to a tracked file,
# and stamps the tree as clean. Untracked files (notes/, BRIEF.md, handoffs) are not the build and do not count.
# Test-NewBuildStamped in live-common.ps1 refuses a build that says modified, so a hand-rolled `go build` fails there.
#
#   pwsh -NoProfile -File scripts\live\build-deploy.ps1
param([switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'BUILD'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'build-deploy.log'
$Out = Join-Path $Repo 'build.claude\atrium.exe'

$branch = git -C $Repo rev-parse --abbrev-ref HEAD 2>$null
if ($branch -ne 'claude/main') { Say "FATAL: $Repo is on '$branch'. deploys build from claude/main only"; exit 1 }
$dirty = git -C $Repo status --porcelain --untracked-files=no 2>$null
if ($dirty) {
  Say "FATAL: $Repo has uncommitted changes to tracked files. commit or drop them first:"
  $dirty | ForEach-Object { Say "  $_" }
  exit 1
}
$commit = git -C $Repo rev-parse HEAD
$ver = git -C $Repo describe --tags --exact-match 2>$null
if (-not $ver) { $ver = 'dev' }
$pkg = 'github.com/dovholuknf/atrium/internal/cli'
$ldflags = "-X $pkg.Version=$ver -X $pkg.Commit=$commit -X $pkg.Tree=clean"

Invoke-Step "go build $ldflags -o $Out ./cmd/atrium (in $Repo)" {
  Remove-Item Env:ATRIUM_NEW_BUILD -ErrorAction SilentlyContinue
  $b = & go -C $Repo build -ldflags $ldflags -o $Out ./cmd/atrium 2>&1
  if ($LASTEXITCODE -ne 0) { $b | ForEach-Object { Say "  $_" }; Say 'FATAL: go build failed'; exit 1 }
}
if ($WhatIf) { exit 0 }
if (-not (Test-NewBuildStamped $Out)) { exit 1 }
Say "built $Out from claude/main $($commit.Substring(0, 7)), tree clean"
