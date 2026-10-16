# Tests for the Docs line warning in scripts/fold-changes.ps1. Needs no hub and no room:
#
#   pwsh -NoProfile -File scripts/test-fold-changes.ps1
#
# Each case builds a throwaway root with one docs/changes file and a docs/test-plan.md, then runs a dry run of the real
# script in a child pwsh. What this cannot run: a real fold (git rm needs a repository), which the warning never touches.
$ErrorActionPreference = 'Stop'

$script:failed = 0
$script:ran = 0
function Check {
    param([string] $name, $got, $want)
    $script:ran++
    if ("$got" -ceq "$want") { Write-Host "ok   $name" }
    else { $script:failed++; Write-Host "FAIL $name`n     want: $want`n     got:  $got" }
}

$fold = Join-Path $PSScriptRoot 'fold-changes.ps1'
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("fold-changes-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force (Join-Path $tmp 'docs/changes') | Out-Null
try {
    Set-Content -LiteralPath (Join-Path $tmp 'docs/test-plan.md') -Value "# Test plan`n`n## A. First`n`nbody"

    function Invoke-Fold {
        param([string] $docsLine)
        $file = Join-Path $tmp 'docs/changes/x-001.md'
        $body = "## Test plan`n`n## @LETTER@. Thing`n`n1. Do it.`n"
        if ($docsLine) { $body = "$docsLine`n`n$body" }
        Set-Content -LiteralPath $file -NoNewline -Value $body
        $out = & (Get-Command pwsh).Source -NoProfile -File $fold -DryRun -Root $tmp 3>&1 2>&1
        [pscustomobject]@{ Out = (@($out | ForEach-Object { "$_" }) -join "`n"); Code = $LASTEXITCODE }
    }

    $r = Invoke-Fold ''
    Check 'missing: warns' ($r.Out -match 'WARNING: x-001\.md: no ''Docs:') $true
    Check 'missing: names the file' ($r.Out -match 'x-001\.md') $true
    Check 'missing: exit code is 0' $r.Code 0
    Check 'missing: the fold output is there' ($r.Out -match 'x-001\.md: test plan as section B') $true

    $r = Invoke-Fold 'Docs: none, a fix that matches the docs.'
    Check 'none: quiet' ($r.Out -match 'WARNING') $false
    Check 'none: exit code is 0' $r.Code 0
    Check 'none: the fold output is there' ($r.Out -match 'x-001\.md: test plan as section B') $true

    $r = Invoke-Fold 'Docs: d-12'
    Check 'd-12: quiet' ($r.Out -match 'WARNING') $false
    Check 'd-12: exit code is 0' $r.Code 0
    Check 'd-12: the fold output is there' ($r.Out -match 'x-001\.md: test plan as section B') $true

    $r = Invoke-Fold 'Docs: none'
    Check 'none with no reason: warns' ($r.Out -match 'WARNING: x-001\.md') $true
    Check 'none with no reason: exit code is 0' $r.Code 0
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ""
if ($script:failed) { Write-Host "$script:failed of $script:ran checks failed."; exit 1 }
Write-Host "all $script:ran checks pass."
