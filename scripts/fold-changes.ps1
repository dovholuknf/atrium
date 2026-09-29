<#
.SYNOPSIS
  Fold every docs/changes/<item>.md into docs/test-plan.md, then git rm it.

.DESCRIPTION
  Each change file has one required section, "## Test plan". Its body has @LETTER@ placeholders, which are replaced
  with the next free test-plan letter, and is appended to docs/test-plan.md. See docs/changes/README.md.

  CHANGELOG.md is frozen and this script never writes it. An older file may still carry a "## Changelog" section.
  That section is moved, as it stands, into changelog/<dept>/<yyyy-mm-dd>-<item>.md. The dept comes from the item's
  prefix (u ui, t terminal, r runtime, f fabric, m merge), or from -Dept when the item has none of those.

  The next letter is the one after the highest heading letter in docs/test-plan.md. Letters run A..Z, AA..AZ, BA and
  so on. A heading whose letter is more than 26 steps past the running highest one in file order is treated as an
  outlier (the file has a stray "UA") and ignored, with a warning.

.PARAMETER DryRun
  Print what would happen and change nothing.

.PARAMETER Item
  Fold only docs/changes/<Item>.md.

.PARAMETER Dept
  The dept for a leftover changelog section whose item id has no department prefix: the branch's director.

.PARAMETER Root
  Repository root. Defaults to the parent of this script's directory.
#>
[CmdletBinding()]
param(
    [switch]$DryRun,
    [string]$Item,
    [ValidateSet('ui', 'terminal', 'runtime', 'fabric', 'review', 'rnd', 'merge')]
    [string]$Dept,
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

function Get-Trimmed([string[]]$lines) {
    $l = @($lines)
    while ($l.Count -gt 0 -and $l[0].Trim() -eq '') { $l = @($l | Select-Object -Skip 1) }
    while ($l.Count -gt 0 -and $l[-1].Trim() -eq '') { $l = @($l | Select-Object -SkipLast 1) }
    , $l
}

$prefixDept = @{ u = 'ui'; t = 'terminal'; r = 'runtime'; f = 'fabric'; m = 'merge' }

$changesDir = Join-Path $Root 'docs/changes'
$testplan = Join-Path $Root 'docs/test-plan.md'
$today = Get-Date -Format 'yyyy-MM-dd'

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
    if ($ti -lt 0) { Fail "$($f.Name): no '## Test plan' line" }
    $cl = @()
    if ($ci -ge 0) {
        # A leftover section runs from its heading to the other one, or to the end of the file.
        $end = if ($ti -gt $ci) { $ti - 1 } else { $lines.Count - 1 }
        if ($end -gt $ci) { $cl = Get-Trimmed $lines[($ci + 1)..$end] }
        if ($cl.Count -eq 0) { Fail "$($f.Name): the Changelog section is empty, delete the heading" }
        if ($cl -join "`n" -match '@LETTER@') { Fail "$($f.Name): @LETTER@ appears in the changelog section" }
    }
    $tpEnd = if ($ci -gt $ti) { $ci - 1 } else { $lines.Count - 1 }
    $tp = @()
    if ($tpEnd -gt $ti) { $tp = Get-Trimmed $lines[($ti + 1)..$tpEnd] }
    if ($tp.Count -eq 0) { Fail "$($f.Name): the Test plan section is empty" }
    if ($tp[0] -notmatch '^## @LETTER@\.? \S') { Fail "$($f.Name): the test plan must start with '## @LETTER@ Title'" }
    $joined = $tp -join "`n"
    if ($joined -match '(?m)^#{2,3} (?!@LETTER@)[A-Z]{1,3}\d*\. ') { Fail "$($f.Name): a heading carries a hard-coded letter" }

    $entryPath = $null
    if ($cl.Count -gt 0) {
        $d = $null
        if ($f.BaseName -match '^([a-z])-\d') { $d = $prefixDept[$Matches[1]] }
        if (-not $d) { $d = $Dept }
        if (-not $d) { Fail "$($f.Name): has a Changelog section and no dept prefix, pass -Dept <the branch's director>" }
        $entryPath = "changelog/$d/$today-$($f.BaseName).md"
        if (Test-Path (Join-Path $Root $entryPath)) { Fail "$($f.Name): $entryPath already exists" }
    }
    $parsed += [pscustomobject]@{ File = $f; Changelog = $cl; TestPlan = $tp; EntryPath = $entryPath }
}

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
foreach ($p in $parsed) {
    $next++
    $letter = ConvertTo-Letters $next
    $tp = @($p.TestPlan | ForEach-Object { $_ -replace '@LETTER@', $letter })
    $tpLines.Add('')
    $tpLines.AddRange([string[]]$tp)
    Write-Host "$($p.File.Name): test plan as section $letter"
    if ($p.EntryPath) {
        Write-Host "$($p.File.Name): leftover changelog section to $($p.EntryPath)"
        if ($p.Changelog.Count -gt 5) { Write-Warning "$($p.EntryPath) is $($p.Changelog.Count) lines, the rule is 1 to 5" }
    }
}

if ($DryRun) { Write-Host 'dry run: nothing written, nothing removed'; exit 0 }

$enc = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($testplan, (($tpLines -join "`n") + "`n"), $enc)
foreach ($p in $parsed) {
    if ($p.EntryPath) {
        $full = Join-Path $Root $p.EntryPath
        New-Item -ItemType Directory -Force (Split-Path -Parent $full) | Out-Null
        [System.IO.File]::WriteAllText($full, (($p.Changelog -join "`n") + "`n"), $enc)
        git -C $Root add -- $p.EntryPath
        if ($LASTEXITCODE -ne 0) { Fail "git add failed for $($p.EntryPath)" }
        Write-Host "wrote $($p.EntryPath)"
    }
    git -C $Root rm -q -f -- "docs/changes/$($p.File.Name)"
    if ($LASTEXITCODE -ne 0) { Fail "git rm failed for $($p.File.Name)" }
    Write-Host "removed docs/changes/$($p.File.Name)"
}
Write-Host "folded $($parsed.Count) item(s)"
