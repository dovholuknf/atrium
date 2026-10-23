# List the local claude/* branches whose every commit has a patch-equivalent on claude/main, and with -Apply delete them.
#
#   pwsh scripts/branch-sweep.ps1                     dry run: one line per branch, then totals
#   pwsh scripts/branch-sweep.ps1 -Apply              delete the "delete" ones with git branch -D
#   pwsh scripts/branch-sweep.ps1 -Worktrees          dry run over worktrees: one line per merged claude/* worktree
#   pwsh scripts/branch-sweep.ps1 -Worktrees -Apply   git worktree remove, then git branch -D, for each "remove"
#   pwsh scripts/branch-sweep.ps1 -Repo D:\some\repo  sweep another checkout
#
# A branch is deleted when `git cherry claude/main <branch>` shows no `+` line. A branch checked out in any worktree,
# and the long-lived claude/* branches, are kept. Local only: nothing here fetches, pushes or names a remote.
#
# -Worktrees looks at each worktree on a claude/* branch that cherry finds fully on claude/main. It is kept when it has
# uncommitted changes, untracked files outside build.claude/ and node_modules/, is the main checkout, or is one of
# $keepWorktrees (matched on the directory name, with or without an atrium- prefix, or on the branch claude/<name>).
param(
    [string]$Repo = 'D:\git\github\dovholuknf\atrium',
    [string]$Base = 'claude/main',
    [switch]$Worktrees,
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'

$protected = @('claude/main', 'claude/orch', 'claude/qa', 'claude/rnd', 'claude/orch-premerge')
$keepWorktrees = @('orch-land', 'branch-sweep', 'atrium-root-design')

function Invoke-Git {
    $out = & git -C $Repo @args 2>&1
    if ($LASTEXITCODE -ne 0) { throw "git $($args -join ' ') failed: $out" }
    $out
}

Invoke-Git rev-parse --verify --quiet "refs/heads/$Base" | Out-Null

$worktreeList = @()
$wt = $null
foreach ($line in (Invoke-Git worktree list --porcelain)) {
    if ($line -like 'worktree *') {
        $wt = [pscustomobject]@{ Path = $line.Substring(9); Branch = $null; Prunable = $false; Main = ($worktreeList.Count -eq 0) }
        $worktreeList += $wt
    }
    elseif ($line -like 'branch refs/heads/*') { $wt.Branch = $line.Substring(18) }
    elseif ($line -like 'prunable*') { $wt.Prunable = $true }
}

function Get-UnmergedCount([string]$branch) {
    @(Invoke-Git cherry $Base $branch | Where-Object { $_ -like '+*' }).Count
}

if ($Worktrees) {
    $toRemove = @()
    $kept = 0
    $unmerged = 0
    foreach ($w in $worktreeList) {
        if (-not $w.Branch -or $w.Branch -notlike 'claude/*') { continue }
        if ((Get-UnmergedCount $w.Branch) -gt 0) { $unmerged++; continue }
        $leaf = Split-Path -Leaf $w.Path
        $short = $w.Branch.Substring(7)
        $reason = $null
        if ($w.Main) { $reason = 'main checkout' }
        elseif ($keepWorktrees | Where-Object { $leaf -eq $_ -or $leaf -eq "atrium-$_" -or $short -eq $_ }) { $reason = 'named keep' }
        elseif ($w.Prunable -or -not (Test-Path -LiteralPath $w.Path)) { $reason = 'path missing' }
        else {
            $status = & git -C $w.Path status --porcelain --untracked-files=normal 2>&1
            if ($LASTEXITCODE -ne 0) { $reason = "git status failed: $status" }
            else {
                $changed = @($status | Where-Object { $_ -notlike '`?`? *' }).Count
                $untracked = @($status | Where-Object { $_ -like '`?`? *' } | ForEach-Object { $_.Substring(3).Trim('"') } |
                    Where-Object { $_ -notlike 'build.claude/*' -and $_ -notmatch '(^|/)node_modules/' })
                if ($changed -gt 0) { $reason = "$changed uncommitted change(s)" }
                elseif ($untracked.Count -gt 0) { $reason = "$($untracked.Count) untracked file(s): $(($untracked | Select-Object -First 3) -join ', ')" }
            }
        }
        if ($reason) {
            "$($w.Path) [$($w.Branch)] keep: $reason"
            $kept++
        } else {
            "$($w.Path) [$($w.Branch)] remove"
            $toRemove += $w
        }
    }

    ''
    "remove: $($toRemove.Count)  keep: $kept  merged: $($toRemove.Count + $kept)  unmerged (not listed): $unmerged"

    if (-not $Apply) {
        'dry run, nothing removed. Pass -Apply to remove.'
        return
    }

    $failed = 0
    foreach ($w in $toRemove) {
        & git -C $Repo worktree remove $w.Path
        if ($LASTEXITCODE -ne 0) { $failed++; continue }
        & git -C $Repo branch -D $w.Branch
        if ($LASTEXITCODE -ne 0) { $failed++ }
    }
    "removed: $($toRemove.Count - $failed)  failed: $failed"
    if ($failed -gt 0) { exit 1 }
    return
}

$checkedOut = @{}
foreach ($w in $worktreeList) { if ($w.Branch) { $checkedOut[$w.Branch] = $w.Path } }

$branches = Invoke-Git for-each-ref --format='%(refname)' 'refs/heads/claude/' | ForEach-Object { $_.Substring(11) } | Sort-Object

$toDelete = @()
$kept = 0
foreach ($b in $branches) {
    $reason = $null
    if ($protected -contains $b) { $reason = 'protected' }
    elseif ($checkedOut.ContainsKey($b)) { $reason = "checked out in $($checkedOut[$b])" }
    else {
        $plus = Get-UnmergedCount $b
        if ($plus -gt 0) { $reason = "$plus commit(s) not on $Base" }
    }
    if ($reason) {
        "$b keep: $reason"
        $kept++
    } else {
        "$b delete"
        $toDelete += $b
    }
}

''
"delete: $($toDelete.Count)  keep: $kept  total: $($branches.Count)"

if (-not $Apply) {
    'dry run, nothing deleted. Pass -Apply to delete.'
    return
}

$failed = 0
foreach ($b in $toDelete) {
    & git -C $Repo branch -D $b
    if ($LASTEXITCODE -ne 0) { $failed++ }
}
"deleted: $($toDelete.Count - $failed)  failed: $failed"
if ($failed -gt 0) { exit 1 }
