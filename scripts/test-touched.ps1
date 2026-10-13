# Test what this branch touched, and nothing else: scripts/test-touched.sh, from PowerShell.
#
#   pwsh -NoProfile -File scripts/test-touched.ps1
#   pwsh -NoProfile -File scripts/test-touched.ps1 --base origin/main --plan
#
# Every argument is passed through as given. Needs Git Bash, which every machine here has with git.
# Not a test of its own, so ci.sh's ps-tests step leaves it out as it does test-groups.ps1.
$ErrorActionPreference = 'Stop'

$bash = Get-Command bash -ErrorAction SilentlyContinue
if (-not $bash) {
    $git = Get-Command git -ErrorAction SilentlyContinue
    if ($git) {
        $cand = Join-Path (Split-Path (Split-Path $git.Source)) 'bin/bash.exe'
        if (Test-Path -LiteralPath $cand) { $bash = Get-Item -LiteralPath $cand }
    }
}
if (-not $bash) { [Console]::Error.WriteLine('test-touched: no bash found. install Git for Windows.'); exit 2 }
$exe = if ($bash.Source) { $bash.Source } else { $bash.FullName }

& $exe (Join-Path $PSScriptRoot 'test-touched.sh') @args
exit $LASTEXITCODE
