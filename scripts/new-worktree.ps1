<#
.SYNOPSIS
  Create a worktree and branch, then link every CLAUDE.md the main checkout holds.

.DESCRIPTION
  `git worktree add` gives a worker none of the CLAUDE.md files: they are untracked symlinks into dotagents, present
  only in the main checkout. This makes the worktree at <Root>/<Name> on a new branch claude/<Name> off <Base>, then
  makes a symlink at the same relative path for each CLAUDE.md in the main checkout that is itself a symlink,
  pointing at the same target. Links stay links, never copies. Exits non-zero, with a message, if the worktree or
  branch already exists, if any link cannot be made, or if CLAUDE.md is absent in the result.

.EXAMPLE
  pwsh scripts/new-worktree.ps1 -Name sa80
#>
param(
  [Parameter(Mandatory)][string]$Name,
  [string]$Base = 'claude/main',
  # The main checkout. Defaults to the checkout this script lives in, or the main one when run from a worktree.
  [string]$Main,
  [string]$Root = 'D:/worktrees/claude/atrium'
)

$ErrorActionPreference = 'Stop'

function Fail([string]$msg) {
  [Console]::Error.WriteLine("new-worktree: $msg")
  exit 1
}

if ($Name -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { Fail "bad -Name '$Name'" }

if (-not $Main) {
  $Main = Split-Path -Parent $PSScriptRoot
  # Run from inside a worktree, the first entry of the list is still the main checkout.
  $first = (git -C $Main worktree list --porcelain 2>$null | Select-Object -First 1)
  if ($first -match '^worktree (.+)$') { $Main = $Matches[1] }
}
if (-not (Test-Path -LiteralPath (Join-Path $Main '.git'))) { Fail "-Main '$Main' is not a git checkout" }
$Main = (Resolve-Path -LiteralPath $Main).Path

$branch = "claude/$Name"
$wt = Join-Path $Root $Name

if (Test-Path -LiteralPath $wt) { Fail "worktree path already exists: $wt" }
git -C $Main show-ref --verify --quiet "refs/heads/$branch"
if ($LASTEXITCODE -eq 0) { Fail "branch already exists: $branch" }
git -C $Main show-ref --verify --quiet "refs/heads/$Base"
if ($LASTEXITCODE -ne 0) { Fail "base branch not found: $Base" }

New-Item -ItemType Directory -Force -Path $Root | Out-Null
git -C $Main worktree add -b $branch $wt $Base
if ($LASTEXITCODE -ne 0) { Fail "git worktree add failed for $wt" }
$wt = (Resolve-Path -LiteralPath $wt).Path
Write-Output "worktree $wt (branch $branch, base $Base)"

$skip = '[\\/](\.git|node_modules|\.mercurius)[\\/]'
$found = Get-ChildItem -LiteralPath $Main -Recurse -Force -Filter CLAUDE.md -ErrorAction SilentlyContinue |
  Where-Object { $_.LinkType -eq 'SymbolicLink' -and $_.FullName -notmatch $skip }

$count = 0
foreach ($l in $found) {
  $rel = $l.FullName.Substring($Main.Length).TrimStart('\', '/')
  $dest = Join-Path $wt $rel
  $target = @($l.Target)[0]
  try {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $dest) | Out-Null
    # A tracked CLAUDE.md would already be there from the checkout. Replace it only if it is not our link.
    if (Test-Path -LiteralPath $dest) { Remove-Item -LiteralPath $dest -Force }
    New-Item -ItemType SymbolicLink -Path $dest -Target $target | Out-Null
    $made = Get-Item -LiteralPath $dest -Force
    if ($made.LinkType -ne 'SymbolicLink') { throw "result is not a symlink" }
  } catch {
    Fail "could not link $rel -> ${target}: $($_.Exception.Message) (worktree left at $wt)"
  }
  $count++
  Write-Output "linked $rel -> $target"
}

if (-not (Test-Path -LiteralPath (Join-Path $wt 'CLAUDE.md'))) {
  Fail "CLAUDE.md is absent in $wt (worktree left in place)"
}

# The links are untracked, so `git add -A` in a worktree commits them. r-031's worker committed seven. info/exclude
# lives in the common dir and so covers every worktree at once.
$common = (git -C $wt rev-parse --path-format=absolute --git-common-dir).Trim()
$exclude = Join-Path $common 'info/exclude'
$have = @(if (Test-Path -LiteralPath $exclude) { Get-Content -LiteralPath $exclude })
if ($have -notcontains 'CLAUDE.md') {
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $exclude) | Out-Null
  Add-Content -LiteralPath $exclude -Value 'CLAUDE.md'
  Write-Output "added CLAUDE.md to $exclude"
}
git -C $wt check-ignore -q --no-index CLAUDE.md
if ($LASTEXITCODE -ne 0) { Fail "CLAUDE.md is not ignored in $wt after writing $exclude" }

Write-Output "linked $count CLAUDE.md file(s) into $wt"
