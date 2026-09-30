<#
.SYNOPSIS
  Every check a merge needs, in one call. Prints failures, and one line per check with a count.

.DESCRIPTION
  1. go test -p 4 ./...  (ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared: room sessions inherit both and they
     fail tests)
  2. scripts/check-board.sh, including the headless run, with NODE_PATH pointed at a node_modules that has
     Playwright. A headless run that skipped itself is a FAILURE here, unless -SkipHeadless.
  3. scripts/check-skins.sh, when the board changed (git diff --name-only <Base>...HEAD touches internal/api/web/).
  4. go build -o build.claude/atrium.exe ./cmd/atrium

  A check that did not run prints no count, so a skipped one is a missing entry rather than silence.

  Known load noise, rerun ALONE once and reported as flaky-pass when they then pass: the link restart-gate tests
  (TestTheGate*). Anything else failing is real. Exit is non-zero on any real failure.

  Playwright is found, in order: -NodePath, $env:ATRIUM_NODE_PATH, $env:NODE_PATH, the merge worktree, this
  checkout, any sibling worktree, D:/tmp. See scripts/setup-merge-worktree.ps1.

.EXAMPLE
  pwsh scripts/merge-check.ps1                 # after a merge commit: Base is HEAD^1
  pwsh scripts/merge-check.ps1 -SkipGo -NoBoard
  pwsh scripts/merge-check.ps1 -NoUI           # the merger's run: go and build only, no board, no headless, no skins
#>
param(
  [string]$NodePath = $env:ATRIUM_NODE_PATH,
  [string]$Base = 'HEAD^1',
  [switch]$Board,
  [switch]$NoBoard,
  [switch]$SkipGo,
  [switch]$SkipHeadless,
  [switch]$SkipBuild,
  [switch]$NoUI,
  [string]$WorktreeRoot = 'D:/worktrees/claude/atrium',
  # Tests known to fail under `go test ./...` load and pass alone. Regexes on the top-level test name.
  [string[]]$Flaky = @()
)

$ErrorActionPreference = 'Continue'
$repo = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Set-Location -LiteralPath $repo

# The link restart-gate tests are timing tests: every Test func in internal/link/restartgate*_test.go is known noise.
$gateTests = @(Get-ChildItem -Path (Join-Path $repo 'internal/link') -Filter 'restartgate*_test.go' -ErrorAction SilentlyContinue |
    ForEach-Object { Select-String -LiteralPath $_.FullName -Pattern '^func (Test\w+)\(' } |
    ForEach-Object { $_.Matches[0].Groups[1].Value })
$Flaky = @($Flaky) + @($gateTests | ForEach-Object { "^$_`$" })

$summary = [System.Collections.Generic.List[string]]::new()
$failed = $false

function Fail([string]$title, [string]$detail) {
  $script:failed = $true
  [Console]::Error.WriteLine("--- FAIL: $title")
  if ($detail) { [Console]::Error.WriteLine($detail.TrimEnd()) }
}

function Test-PlaywrightDir([string]$dir) {
  if (-not $dir) { return $false }
  (Test-Path -LiteralPath (Join-Path $dir 'playwright')) -or (Test-Path -LiteralPath (Join-Path $dir '@playwright/test'))
}

function Find-NodePath {
  $cands = @($NodePath, $env:ATRIUM_NODE_PATH, $env:NODE_PATH)
  $cands += (Join-Path $WorktreeRoot 'merge/node_modules')
  $cands += (Join-Path $repo 'node_modules')
  $cands += @(Get-ChildItem -Directory -LiteralPath $WorktreeRoot -ErrorAction SilentlyContinue |
      ForEach-Object { Join-Path $_.FullName 'node_modules' })
  $cands += 'D:/tmp/node_modules', 'D:/tmp'
  foreach ($c in $cands) {
    foreach ($one in ($c -split [IO.Path]::PathSeparator)) {
      if ($one -and (Test-PlaywrightDir $one)) { return (Resolve-Path -LiteralPath $one).Path }
    }
  }
  return $null
}

# ---- 1. go test
if ($SkipGo) {
  $summary.Add('go skipped')
} else {
  $env:ATRIUM_LOCATION = $null
  $env:ATRIUM_DEBUG_INPUTLAG = $null
  $pass = 0
  $fails = @{}   # "pkg|Test" -> $true
  $pkgOut = @{}  # pkg -> output lines
  $testOut = @{} # "pkg|Test" -> output lines
  $pkgFail = @{} # pkg -> $true, a package that failed with no failing test (build failure, panic, timeout)
  go test -p 4 -json ./... 2>&1 | ForEach-Object {
    $ev = $null
    try { $ev = $_ | ConvertFrom-Json -ErrorAction Stop } catch { }
    if (-not $ev -or -not $ev.Action) { [Console]::Error.WriteLine("$_"); return }
    if ($ev.Action -eq 'output' -and $ev.Package) {
      if (-not $pkgOut.ContainsKey($ev.Package)) { $pkgOut[$ev.Package] = [System.Collections.Generic.List[string]]::new() }
      $pkgOut[$ev.Package].Add($ev.Output)
    }
    if ($ev.Action -eq 'output' -and $ev.Test) {
      $tk = "$($ev.Package)|$($ev.Test)"
      if (-not $testOut.ContainsKey($tk)) { $testOut[$tk] = [System.Collections.Generic.List[string]]::new() }
      $testOut[$tk].Add($ev.Output)
    }
    if ($ev.Test -and $ev.Action -eq 'pass' -and $ev.Test -notmatch '/') { $pass++ }
    if ($ev.Test -and $ev.Action -eq 'fail' -and $ev.Test -notmatch '/') { $fails["$($ev.Package)|$($ev.Test)"] = $true }
    if (-not $ev.Test -and $ev.Package -and $ev.Action -eq 'fail') { $pkgFail[$ev.Package] = $true }
  }
  $flakyPass = 0
  $real = 0
  foreach ($k in ($fails.Keys | Sort-Object)) {
    $pkg, $name = $k -split '\|', 2
    $isFlaky = $false
    foreach ($rx in $Flaky) { if ($name -match $rx) { $isFlaky = $true } }
    if ($isFlaky) {
      $out = go test -count=1 -run "^$([regex]::Escape($name))$" $pkg 2>&1 | Out-String
      if ($LASTEXITCODE -eq 0) { $flakyPass++; [Console]::Error.WriteLine("flaky-pass: $name ($pkg) failed under load, passed alone"); continue }
      Fail "$name ($pkg) failed under load AND alone" (($out -split "`n" | Select-Object -Last 30) -join "`n")
    } else {
      # Subtests print under their own key, so pull them in with the parent.
      $lines = ($testOut.Keys | Where-Object { $_ -eq $k -or $_ -like "$k/*" } | Sort-Object |
          ForEach-Object { $testOut[$_] } | Where-Object { $_ }) -join ''
      Fail "$name ($pkg)" (($lines -split "`n" | Select-Object -Last 60) -join "`n")
    }
    $real++
  }
  foreach ($pkg in ($pkgFail.Keys | Sort-Object)) {
    # Only a package that failed without a failing test of its own is new information here.
    if ($fails.Keys | Where-Object { $_ -like "$pkg|*" }) { continue }
    Fail "package $pkg" (($pkgOut[$pkg] | Where-Object { $_ }) -join '')
    $real++
  }
  $s = "go $pass pass"
  if ($flakyPass) { $s += ", $flakyPass flaky-pass" }
  if ($real) { $s += ", $real FAIL" }
  $summary.Add($s)
}

# ---- 2. board
# -NoUI SKIPS BOTH, check-board.sh included and not only its headless run. The merger does not run the UI tests: @ui
# runs them on claude/main on its own schedule, and a merge or a hub deploy does not wait for them.
if (-not $NoUI) {
  $np = $null
  if ($SkipHeadless) {
    $summary.Add('board headless skipped')
  } else {
    $np = Find-NodePath
    if (-not $np) {
      Fail 'playwright not found' ("no node_modules with playwright anywhere. pass -NodePath or set ATRIUM_NODE_PATH, run scripts/setup-merge-worktree.ps1, or pass -SkipHeadless to say the headless run is meant to be skipped.")
    }
  }
  if ($SkipHeadless -or $np) {
    if ($np) { $env:NODE_PATH = $np }
    $out = & bash scripts/check-board.sh 2>&1 | Out-String
    $rc = $LASTEXITCODE
    $headless = ($SkipHeadless -or $out -notmatch 'so the headless board\s+check is skipped|headless board check is skipped')
    if ($rc -ne 0) {
      Fail 'check-board.sh' $out
    } elseif (-not $headless) {
      Fail 'check-board.sh passed but the headless run skipped itself (playwright or chromium missing)' $out
    } else {
      $summary.Add($(if ($SkipHeadless) { 'board ok (no headless)' } else { "board ok (headless ran, NODE_PATH=$np)" }))
    }
  }

  # ---- 3. skins
  $runSkins = $false
  if ($Board) { $runSkins = $true }
  elseif ($NoBoard) { $runSkins = $false }
  else {
    $names = git diff --name-only "$Base...HEAD" 2>$null
    if ($LASTEXITCODE -ne 0) { $runSkins = $true; [Console]::Error.WriteLine("merge-check: could not diff against $Base, running skins") }
    elseif ($names -match '^internal/api/web/') { $runSkins = $true }
  }
  if ($runSkins) {
    $out = & bash scripts/check-skins.sh 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { Fail 'check-skins.sh' $out }
    else { $summary.Add(($out.Trim() -split "`n" | Select-Object -Last 1).Trim()) }
  } else {
    $summary.Add('skins skipped (board unchanged)')
  }
} else {
  $summary.Add('board and skins not run (-NoUI, @ui runs them on claude/main)')
}

# ---- 4. build
if ($SkipBuild) {
  $summary.Add('build skipped')
} else {
  $out = go build -o build.claude/atrium.exe ./cmd/atrium 2>&1 | Out-String
  if ($LASTEXITCODE -ne 0) { Fail 'go build' $out } else { $summary.Add('build ok') }
}

$line = 'merge-check: ' + ($summary -join ' | ')
if ($failed) { Write-Output "$line | FAILED"; exit 1 }
Write-Output "$line | PASSED"
exit 0
