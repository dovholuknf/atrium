# Sign every commit a branch has over the bucket below it with your key, then push it to origin as dev-<bucket> so
# GitHub CI runs on it.
# Each landed group is a bucket. A bucket stacks on the newest dev-* on origin that the branch already holds, else on
# main, so each bucket branch holds every earlier one. Open its PR against that base and merge bottom first.
# Run as clint. The claude checkout is never touched: the signing happens in a throwaway worktree, on a throwaway
# branch, and both are removed after.
# The messages are made the way scripts/land-claude-main.ps1 makes them, by the same scripts/land-history.go: one
# line, claude scrubbed out, you as author and committer, signed. The repo's pre-push hook refuses any other.
#
#   pwsh -File D:\git\github\dovholuknf\atrium\.uncommitted\push-signed.ps1 claude/main -Bucket leftover-fixes
#   pwsh -File D:\git\github\dovholuknf\atrium\.uncommitted\push-signed.ps1 claude/main -Bucket leftover-fixes -NoPush   # rewrite and check only
#   pwsh -File D:\git\github\dovholuknf\atrium\.uncommitted\push-signed.ps1 claude/main -Bucket leftover-fixes -Force    # replace an existing bucket
param(
    [Parameter(Mandatory)][string]$Branch,
    [Parameter(Mandatory)][string]$Bucket,
    [string]$Repo = 'D:\git\github\dovholuknf\atrium',
    [string]$Onto = 'origin/main',
    [switch]$Force,
    [switch]$NoPush
)
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

function g { git --no-pager -c safe.directory=* @args }

if ($Bucket -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { throw "bad bucket name: $Bucket" }
$Target = "dev-$Bucket"

$wt = Join-Path 'D:\tmp' ('push-signed-' + ($Branch -replace '[\\/]', '-'))
# land-history.go rewrites a local branch, so the copy gets one of its own.
$tmp = 'push-signed/' + ($Branch -replace '[\\/]', '-')
g -C $Repo fetch --prune origin main '+refs/heads/dev-*:refs/remotes/origin/dev-*'
$exists = [bool](g -C $Repo for-each-ref --format='%(refname)' "refs/remotes/origin/$Target")
if ($exists -and -not $Force) { throw "$Target is already on origin: pass -Force to replace it" }

# The newest bucket the branch already holds. A signed copy has new shas, so holding it means git cherry finds every
# commit of the bucket in the branch by patch.
$Base = $Onto
$baseCount = 0
foreach ($ref in g -C $Repo for-each-ref --format='%(refname:short)' 'refs/remotes/origin/dev-*') {
    if ($ref -eq "origin/$Target") { continue }
    $count = [int](g -C $Repo rev-list --count "$Onto..$ref")
    if ($count -le $baseCount) { continue }
    $missing = g -C $Repo cherry $Branch $ref | Where-Object { $_ -notmatch '^- ' }
    if ($missing) { continue }
    $Base = $ref
    $baseCount = $count
}
$baseName = $Base -replace '^origin/', ''

g -C $Repo worktree add -B $tmp $wt $Branch
try {
    # Plain rebase: land-history.go signs every commit it writes. Rebasing onto a bucket drops the commits it holds.
    g -C $wt -c advice.skippedCherryPicks=false rebase $Base 2>&1 | ForEach-Object { "$_" } |
        Where-Object { $_ -notmatch '^warning: skipped previously applied commit' }
    Push-Location $wt
    # land-history.go prints a push to main, but this pushes to dev-<bucket>.
    try { go run scripts/land-history.go -base $Base -branch $tmp | Where-Object { $_ -notmatch '^push: ' } }
    finally { Pop-Location }
    g -C $wt merge-base --is-ancestor $Base $tmp
    $bad = g -C $wt log --format='%G? %h %s' "$Base..$tmp" | Where-Object { $_ -notmatch '^G ' }
    if ($bad) { throw "not signed:`n$($bad -join "`n")" }
    # A rebase drops merges, and with them what a resolution deleted, such as a rename's old path.
    $differ = g -C $wt diff --name-only --no-renames $Branch $tmp
    if ($differ) { throw "the signed branch's tree is not ${Branch}'s:`n$($differ -join "`n")" }
    # The repo's pre-push hook refuses these on GitHub.
    $named = g -C $wt log --format='%h %s%n%b' "$Base..$tmp" | Select-String -Pattern 'claude|anthropic|co.authored.by'
    if ($named) { throw "a message names claude:`n$($named -join "`n")" }
    # A summary, not the whole log: the log grows with every landing.
    $subjects = @(g -C $wt log --reverse --format='%s' "$Base..$tmp")
    $tip = g -C $wt rev-parse $tmp
    Write-Host "$($subjects.Count) commits over $Base"
    if ($subjects.Count) {
        Write-Host "  first: $($subjects[0])"
        Write-Host "  last:  $($subjects[-1])"
    }
    if (-not $subjects.Count) { throw "nothing over $Base to put in $Target" }
    Write-Host "branch: $Target"
    Write-Host "sha:    $tip"
    Write-Host "base:   $baseName"
    if ($NoPush) {
        Write-Host "not pushed: the signed tip is $tip"
    } else {
        if ($exists) {
            # The signed copy gets new shas each run, so a replaced bucket needs the force.
            g -C $wt push --force origin "refs/heads/${tmp}:refs/heads/$Target"
        } else {
            g -C $wt push origin "refs/heads/${tmp}:refs/heads/$Target"
        }
        Write-Host "pushed $tip to $Target"
    }
    Write-Host "gh pr create --base $baseName --head $Target"
}
finally {
    g -C $Repo worktree remove --force $wt
    # The throwaway branch and the backup land-history.go kept of it.
    g -C $Repo for-each-ref --format='%(refname:short)' "refs/heads/$tmp" "refs/heads/$tmp-before-land-*" |
        ForEach-Object { g -C $Repo branch -D $_ }
}
