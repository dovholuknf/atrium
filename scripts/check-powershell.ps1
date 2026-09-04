# Parse-check every PowerShell file in the repo, without running any of them.
#
# These ship as things the operator runs, and nothing has ever checked they
# parse. A syntax error in one is found by somebody at a prompt, which is the
# worst place to find it.
$ErrorActionPreference = 'Stop'

$root = $PSScriptRoot
$bad = 0

Get-ChildItem -Path $root -Filter *.ps1 -Recurse | ForEach-Object {
    $errors = $null
    [System.Management.Automation.Language.Parser]::ParseFile(
        $_.FullName, [ref]$null, [ref]$errors) | Out-Null
    if ($errors -and $errors.Count -gt 0) {
        $bad++
        Write-Host ("FAIL " + $_.Name)
        $errors | ForEach-Object { Write-Host ("     " + $_.Message) }
    } else {
        Write-Host ("ok   " + $_.Name)
    }
}

if ($bad -gt 0) { exit 1 }
Write-Host "`nall powershell parses."
