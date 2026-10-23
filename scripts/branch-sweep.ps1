# List the local claude/* branches whose every commit has a patch-equivalent on claude/main, and with -Apply delete them.
#
#   pwsh scripts/branch-sweep.ps1                     dry run: one line per branch, then totals
#   pwsh scripts/branch-sweep.ps1 -Apply              delete the "delete" ones with git branch -D
#   pwsh scripts/branch-sweep.ps1 -Repo D:\some\repo  sweep another checkout
#
# A branch is deleted when `git cherry claude/main <branch>` shows no `+` line. A branch checked out in any worktree,
# and the long-lived claude/* branches, are kept. Local only: nothing here fetches, pushes or names a remote.
param(
    [string]$Repo = 'D:\git\github\dovholuknf\atrium',
    [string]$Base = 'claude/main',
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'

$protected = @('claude/main', 'claude/orch', 'claude/qa', 'claude/rnd', 'claude/orch-premerge')

function Invoke-Git {
    $out = & git -C $Repo @args 2>&1
    if ($LASTEXITCODE -ne 0) { throw "git $($args -join ' ') failed: $out" }
    $out
}

Invoke-Git rev-parse --verify --quiet "refs/heads/$Base" | Out-Null

$checkedOut = @{}
$wtPath = $null
foreach ($line in (Invoke-Git worktree list --porcelain)) {
    if ($line -like 'worktree *') { $wtPath = $line.Substring(9) }
    elseif ($line -like 'branch refs/heads/*') { $checkedOut[$line.Substring(18)] = $wtPath }
}

$branches = Invoke-Git for-each-ref --format='%(refname)' 'refs/heads/claude/' | ForEach-Object { $_.Substring(11) } | Sort-Object

$toDelete = @()
$kept = 0
foreach ($b in $branches) {
    $reason = $null
    if ($protected -contains $b) { $reason = 'protected' }
    elseif ($checkedOut.ContainsKey($b)) { $reason = "checked out in $($checkedOut[$b])" }
    else {
        $plus = @(Invoke-Git cherry $Base $b | Where-Object { $_ -like '+*' }).Count
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
