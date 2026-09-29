<#
.SYNOPSIS
  Create, idempotently, the worktree merges are made in, with Playwright ready for scripts/merge-check.ps1.

.DESCRIPTION
  Merging in the main checkout locks it for everybody. This makes <Root>/merge on branch claude/merge-scratch, using
  scripts/new-worktree.ps1 so every CLAUDE.md is linked, then runs `npm install` (package.json holds Playwright) and
  `npx playwright install chromium` there. merge-check.ps1 finds <Root>/merge/node_modules on its own.

  Safe to run again: an existing, registered worktree is kept as it is, and only the missing npm and chromium steps
  are done. The branch is a scratch: reset it to claude/main before each merge with
  `git reset --hard claude/main`, and merge the branch under test into it.
#>
param(
  [string]$Root = 'D:/worktrees/claude/atrium',
  [string]$Base = 'claude/main'
)

$ErrorActionPreference = 'Stop'
$branch = 'claude/merge-scratch'
$wt = Join-Path $Root 'merge'

function Fail([string]$msg) {
  [Console]::Error.WriteLine("setup-merge-worktree: $msg")
  exit 1
}

$main = Split-Path -Parent $PSScriptRoot
$first = (git -C $main worktree list --porcelain 2>$null | Select-Object -First 1)
if ($first -match '^worktree (.+)$') { $main = $Matches[1] }

$registered = (git -C $main worktree list --porcelain) -contains "worktree $($wt -replace '\\', '/')"
if (-not $registered) {
  # git prints forward slashes, so compare against the resolved form as well.
  $registered = [bool]((git -C $main worktree list --porcelain) | Where-Object {
      $_ -match '^worktree (.+)$' -and ($Matches[1] -replace '\\', '/') -ieq ($wt -replace '\\', '/') })
}

if ($registered) {
  Write-Output "worktree $wt already registered, keeping it"
} else {
  if (Test-Path -LiteralPath $wt) { Fail "$wt exists but is not a registered worktree. remove it by hand." }
  git -C $main show-ref --verify --quiet "refs/heads/$branch"
  if ($LASTEXITCODE -eq 0) { Fail "branch $branch exists with no worktree. delete it (git branch -D) or add the worktree by hand." }
  & (Join-Path $PSScriptRoot 'new-worktree.ps1') -Name merge -Base $Base -Root $Root -Main $main
  if ($LASTEXITCODE -ne 0) { Fail 'new-worktree.ps1 failed' }
  # new-worktree names the branch claude/<Name>. The merge worktree's branch is claude/merge-scratch.
  git -C $wt branch -m 'claude/merge' $branch
  if ($LASTEXITCODE -ne 0) { Fail "could not rename claude/merge to $branch" }
}

Push-Location $wt
try {
  if (-not (Test-Path -LiteralPath 'node_modules/playwright') -and -not (Test-Path -LiteralPath 'node_modules/@playwright/test')) {
    npm install --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { Fail 'npm install failed' }
  } else {
    Write-Output 'playwright already installed'
  }
  # Idempotent: a no-op when the browser is already in the shared cache.
  npx playwright install chromium
  if ($LASTEXITCODE -ne 0) { Fail 'npx playwright install chromium failed' }
} finally {
  Pop-Location
}

Write-Output "merge worktree ready at $wt (branch $branch). merge-check.ps1 will find its node_modules."
