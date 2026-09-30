<#
.SYNOPSIS
  Install the post-merge hook that tells atrium a merge happened, so a merged worker is culled without anybody
  remembering to.

.DESCRIPTION
  Writes ONE file, `post-merge`, into the hooks directory of the repository at -Repo (default: the current one).
  That is the COMMON hooks directory, `<repo>/.git/hooks` (or wherever `core.hooksPath` points), which every
  worktree of the repository shares, so it is installed once and runs in whichever worktree merged.

  The hook runs `atrium merged --into <the branch HEAD is on>` and nothing else. It is best effort and never fails
  a merge: no atrium on PATH, a daemon that is down, a detached HEAD and any error all exit 0. Fast-forwards fire
  post-merge too. See docs/rnd/merged-cull-design.md.

  It refuses to overwrite a `post-merge` it did not write, and says so. Re-running over its own hook is harmless.

  TO UNDO: run this script again with -Remove, or delete `<repo>/.git/hooks/post-merge` by hand. Nothing else was
  changed: no git config, no other hook, no file in the working tree.

.PARAMETER Repo
  Any directory inside the repository. Default: the current directory.

.PARAMETER Remove
  Remove the hook this script installed, and only that one.

.PARAMETER DryRun
  Say what would be written and where, and write nothing.

.EXAMPLE
  pwsh scripts/install-git-hooks.ps1 -DryRun

.EXAMPLE
  pwsh scripts/install-git-hooks.ps1 -Repo D:/dev/atrium -Remove
#>
param(
  [string]$Repo = '.',
  [switch]$Remove,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

function Fail([string]$msg) {
  [Console]::Error.WriteLine("install-git-hooks: $msg")
  exit 1
}

# The line that says this file is ours. Anything without it is somebody else's and is left alone.
$marker = '# atrium-post-merge: installed by scripts/install-git-hooks.ps1'

$hook = @"
#!/bin/sh
$marker
# Tell atrium a merge happened. Best effort: this must never fail a merge.
branch=`$(git symbolic-ref --short -q HEAD 2>/dev/null) || exit 0
[ -n "`$branch" ] || exit 0
command -v atrium >/dev/null 2>&1 || exit 0
atrium merged --into "`$branch" >/dev/null 2>&1
exit 0
"@ -replace "`r`n", "`n"

if (-not (Test-Path -LiteralPath $Repo)) { Fail "-Repo '$Repo' does not exist" }
$Repo = (Resolve-Path -LiteralPath $Repo).Path

# `--git-path hooks` is the hooks directory git itself would use from here: the common one, and core.hooksPath when set.
$hooksDir = (git -C $Repo rev-parse --path-format=absolute --git-path hooks 2>$null)
if ($LASTEXITCODE -ne 0 -or -not $hooksDir) { Fail "'$Repo' is not inside a git repository" }
$target = Join-Path $hooksDir 'post-merge'

$existing = $null
if (Test-Path -LiteralPath $target) { $existing = [IO.File]::ReadAllText($target) }
$ours = $existing -and $existing.Contains($marker)

if ($Remove) {
  if (-not $existing) { Write-Host "install-git-hooks: no post-merge hook at $target. nothing to remove."; exit 0 }
  if (-not $ours) { Fail "$target is not the atrium hook, so it was left alone" }
  if ($DryRun) { Write-Host "install-git-hooks: would remove $target"; exit 0 }
  Remove-Item -LiteralPath $target
  Write-Host "install-git-hooks: removed $target"
  exit 0
}

if ($existing -and -not $ours) {
  Fail "$target already exists and is not the atrium hook. Merge them by hand: add 'atrium merged --into <branch>' to it."
}
if ($DryRun) {
  Write-Host "install-git-hooks: would write $target"
  Write-Host $hook
  exit 0
}

New-Item -ItemType Directory -Force -Path $hooksDir | Out-Null
[IO.File]::WriteAllText($target, $hook + "`n", (New-Object Text.UTF8Encoding($false)))
Write-Host "install-git-hooks: wrote $target"
Write-Host "  undo: pwsh scripts/install-git-hooks.ps1 -Repo '$Repo' -Remove"
