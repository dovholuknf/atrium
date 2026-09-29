<#
.SYNOPSIS
  Fold every docs/changes/<item>.md into CHANGELOG.md and docs/test-plan.md, then git rm it.

.DESCRIPTION
  Each change file has two sections, "## Changelog" and "## Test plan", in that order. The changelog body goes to
  the top of "## Unreleased" in CHANGELOG.md (newest first). The test-plan body has @LETTER@ placeholders, which are
  replaced with the next free test-plan letter, and is appended to docs/test-plan.md. See docs/changes/README.md.

  The next letter is the one after the highest heading letter in docs/test-plan.md. Letters run A..Z, AA..AZ, BA and
  so on. A heading whose letter is more than 26 steps past the running highest one in file order is treated as an
  outlier (the file has a stray "UA") and ignored, with a warning.

.PARAMETER DryRun
  Print what would happen and change nothing.

.PARAMETER Item
  Fold only docs/changes/<Item>.md.

.PARAMETER Root
  Repository root. Defaults to the parent of this script's directory.
#>
[CmdletBinding()]
param(
    [switch]$DryRun,
    [string]$Item,
    [string]$Root = (Split-Path -Parent $PSScriptRoot)
)
$ErrorActionPreference = 'Stop'

function Fail($msg) { Write-Error "fold-changes: $msg"; exit 1 }

function ConvertTo-Number([string]$letters) {
    $n = 0
    foreach ($c in $letters.ToCharArray()) { $n = $n * 26 + ([int]$c - [int][char]'A' + 1) }
    $n
}

function ConvertTo-Letters([int]$n) {
    $s = ''
    while ($n -gt 0) {
        $n--
        $s = [string][char]([int][char]'A' + ($n % 26)) + $s
        $n = [math]::Floor($n / 26)
    }
    $s
}

$changesDir = Join-Path $Root 'docs/changes'
$changelog = Join-Path $Root 'CHANGELOG.md'
$testplan = Join-Path $Root 'docs/test-plan.md'

if (-not (Test-Path $changesDir)) { Write-Host 'nothing pending (no docs/changes)'; exit 0 }
$files = @(Get-ChildItem $changesDir -Filter '*.md' | Where-Object { $_.Name -ne 'README.md' } | Sort-Object Name)
if ($Item) {
    $files = @($files | Where-Object { $_.BaseName -eq $Item })
    if ($files.Count -eq 0) { Fail "no docs/changes/$Item.md" }
}
if ($files.Count -eq 0) { Write-Host 'nothing pending'; exit 0 }

# Parse and validate every file before touching anything.
$parsed = @()
foreach ($f in $files) {
    $lines = @(Get-Content $f.FullName)
    $ci = [array]::IndexOf($lines, '## Changelog')
    $ti = [array]::IndexOf($lines, '## Test plan')
    if ($ci -lt 0) { Fail "$($f.Name): no '## Changelog' line" }
    if ($ti -lt 0) { Fail "$($f.Name): no '## Test plan' line" }
    if ($ti -lt $ci) { Fail "$($f.Name): '## Test plan' comes before '## Changelog'" }
    $cl = @($lines[($ci + 1)..($ti - 1)])
    $tp = @($lines[($ti + 1)..($lines.Count - 1)])
    while ($cl.Count -gt 0 -and $cl[0].Trim() -eq '') { $cl = @($cl | Select-Object -Skip 1) }
    while ($cl.Count -gt 0 -and $cl[-1].Trim() -eq '') { $cl = @($cl | Select-Object -SkipLast 1) }
    while ($tp.Count -gt 0 -and $tp[0].Trim() -eq '') { $tp = @($tp | Select-Object -Skip 1) }
    while ($tp.Count -gt 0 -and $tp[-1].Trim() -eq '') { $tp = @($tp | Select-Object -SkipLast 1) }
    if ($cl.Count -eq 0) { Fail "$($f.Name): the Changelog section is empty" }
    if ($tp.Count -eq 0) { Fail "$($f.Name): the Test plan section is empty" }
    if ($cl[0] -notmatch '^- \*\*') { Fail "$($f.Name): the changelog entry must start with '- **Title.**'" }
    if ($tp[0] -notmatch '^## @LETTER@\.? \S') { Fail "$($f.Name): the test plan must start with '## @LETTER@ Title'" }
    $joined = $tp -join "`n"
    if ($joined -match '(?m)^#{2,3} (?!@LETTER@)[A-Z]{1,3}\d*\. ') { Fail "$($f.Name): a heading carries a hard-coded letter" }
    if ($cl -join "`n" -match '@LETTER@') { Fail "$($f.Name): @LETTER@ appears in the changelog section" }
    $parsed += [pscustomobject]@{ File = $f; Changelog = $cl; TestPlan = $tp }
}

# The changelog anchor.
$clLines = [System.Collections.Generic.List[string]]@(Get-Content $changelog)
$ui = $clLines.IndexOf('## Unreleased')
if ($ui -lt 0) { Fail "CHANGELOG.md has no '## Unreleased' line" }

# The next free letter.
$tpLines = [System.Collections.Generic.List[string]]@(Get-Content $testplan)
$max = 0
foreach ($l in $tpLines) {
    if ($l -match '^## ([A-Z]{1,3})\. ') {
        $v = ConvertTo-Number $Matches[1]
        if ($v -gt $max + 26 -and $max -gt 0) { Write-Warning "ignoring outlier test-plan letter $($Matches[1])"; continue }
        if ($v -gt $max) { $max = $v }
    }
}
if ($max -eq 0) { Fail 'found no lettered sections in docs/test-plan.md' }
Write-Host "highest letter in use: $(ConvertTo-Letters $max)"

$next = $max
# Newest first: fold in file order, each one going above the last, so a later item ends up on top.
$insertAt = $ui + 1
while ($insertAt -lt $clLines.Count -and $clLines[$insertAt].Trim() -eq '') { $insertAt++ }
foreach ($p in $parsed) {
    $next++
    $letter = ConvertTo-Letters $next
    $tp = @($p.TestPlan | ForEach-Object { $_ -replace '@LETTER@', $letter })
    $entry = @($p.Changelog) + ''
    $clLines.InsertRange($insertAt, [string[]]$entry)
    $tpLines.Add('')
    $tpLines.AddRange([string[]]$tp)
    Write-Host "$($p.File.Name): changelog entry to the top of Unreleased, test plan as section $letter"
}

if ($DryRun) { Write-Host 'dry run: nothing written, nothing removed'; exit 0 }

$enc = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($changelog, (($clLines -join "`n") + "`n"), $enc)
[System.IO.File]::WriteAllText($testplan, (($tpLines -join "`n") + "`n"), $enc)
foreach ($p in $parsed) {
    git -C $Root rm -q -f -- "docs/changes/$($p.File.Name)"
    if ($LASTEXITCODE -ne 0) { Fail "git rm failed for $($p.File.Name)" }
    Write-Host "removed docs/changes/$($p.File.Name)"
}
Write-Host "folded $($parsed.Count) item(s)"
