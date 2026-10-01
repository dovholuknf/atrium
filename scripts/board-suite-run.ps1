# The room's half of scripts/board-suite-remote.ps1. Windows PowerShell 5.1, because that is what a room has over ssh.
# Do not run it by hand: it takes the commit the other half pushed and runs the sharded suite on it.
param(
    [Parameter(Mandatory)] [string] $Clone,
    [Parameter(Mandatory)] [string] $Id,
    [Parameter(Mandatory)] [string] $Sha,
    # `A` then the base64 of the suite's arguments, one per line.
    [Parameter(Mandatory)] [string] $ArgsB64
)

$ErrorActionPreference = 'Continue'
$suiteText = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($ArgsB64.Substring(1)))
$suiteArgs = @($suiteText -split "`n" | Where-Object { $_ })

# The wrapper room-git.ps1 leaves first: the git on an ssh session's PATH can be a Cygwin one, which does not
# understand C:/ paths. Then whatever git is on the PATH.
$git = $null
if (Test-Path "$HOME/.room-git/git.cmd") { $git = "$HOME/.room-git/git.cmd" }
if (-not $git) { $git = (Get-Command git -ErrorAction SilentlyContinue).Source }
if (-not $git) { Write-Host 'board-suite fail git is not on this room'; exit 4 }

# A run cut off by Ctrl-C may never reach its finally, so each run sweeps what an earlier one left a day ago.
$cutoff = (Get-Date).AddDays(-1)
Get-ChildItem "$Clone-worktrees" -Directory -Filter 'suite-*' -ErrorAction SilentlyContinue |
    Where-Object { $_.LastWriteTime -lt $cutoff } |
    ForEach-Object { & $git -C $Clone worktree remove --force $_.FullName 2>&1 | Out-Null }
& $git -C $Clone worktree prune 2>&1 | Out-Null
$limit = [DateTimeOffset]::UtcNow.AddDays(-1).ToUnixTimeSeconds()
& $git -C $Clone for-each-ref --format='%(refname) %(committerdate:unix)' refs/suite 2>$null | ForEach-Object {
    $ref, $when = $_ -split ' '
    if ($ref -and [long]$when -lt $limit) { & $git -C $Clone update-ref -d $ref 2>&1 | Out-Null }
}

$wt = "$Clone-worktrees/suite-$Id"
& $git -C $Clone worktree add --quiet --detach $wt $Sha 2>&1 | ForEach-Object { Write-Host $_ }
if (-not (Test-Path $wt)) { Write-Host "board-suite fail could not make the worktree $wt"; exit 4 }

$code = 4
try {
    # Any worktree beside the clone that has playwright installed lends its node_modules.
    $nm = Get-ChildItem "$Clone-worktrees" -Directory | Where-Object { Test-Path "$($_.FullName)/node_modules/playwright-core" } |
        Select-Object -First 1
    if (-not $nm) { Write-Host 'board-suite fail no worktree here has node_modules with playwright-core'; exit 4 }
    $env:NODE_PATH = "$($nm.FullName)/node_modules"
    # A room's own location settings would point the harness at the live room.
    Remove-Item Env:ATRIUM_LOCATION, Env:ATRIUM_DEBUG_INPUTLAG, Env:ATRIUM_REAL_SCROLLBACK -ErrorAction SilentlyContinue
    $env:ATRIUM_SUITE_REMOTE = '1'
    Set-Location $wt
    Write-Host "board-suite on $env:COMPUTERNAME $($env:NUMBER_OF_PROCESSORS) cores: node scripts/test-board-sharded.js --local $suiteArgs"
    & node scripts/test-board-sharded.js --local @suiteArgs 2>&1 | ForEach-Object { Write-Host $_ }
    $code = $LASTEXITCODE
} finally {
    Set-Location $Clone
    & $git worktree remove --force $wt 2>&1 | Out-Null
    & $git update-ref -d "refs/suite/$Id" 2>&1 | Out-Null
}
exit $code
