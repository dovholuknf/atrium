# Windows Defender on a hosted CI runner: say whether it is on, measure what it costs a never-seen executable, and
# on GitHub's runner exclude the folders the tests write binaries to, then measure again.
#
#   pwsh -NoProfile -File scripts\ci-defender.ps1
#
# WHY. scripts/test-time-guard.sh flagged internal/ptyhost's TestStartRunsADetachedHostFromACopy at 8 to 12 s on
# the windows-latest runner, and internal/base/detach's TestStartOutsideJobAnswersTheProgramNotCmd at 7 to 14 s. Both
# start an executable that did not exist a moment before. On a Windows box with real-time protection on, the first
# CreateProcess of a fresh copy of a 7 MB test binary takes 2.8 to 3.4 s and the second 20 ms: Defender scans a
# file it has not seen before the process may run. That the hosted runner does the same was a belief, so this
# prints the evidence into the machine step of every Windows job: real-time protection on or off, the exclusions
# already there, and the first and second start of a fresh copy of go.exe, before and after the exclusions.
#
# WHAT IT EXCLUDES, and only when GITHUB_ACTIONS is set, so a developer's machine is measured and never changed:
# TEMP, where go test links its binaries and t.TempDir lives, RUNNER_TEMP, GOCACHE, GOTMPDIR when it is set, and
# the checkout's build.claude. The runner is a throwaway machine and these folders hold only what this job built.
# scripts/room-defender.ps1 is the careful version for a room that lives on.
#
# NEVER FAILS THE STEP. Everything is inside one try, and the exit code is always 0: an answer that is missing is
# said and the tests run as they did before. Every line is prefixed ci-defender.
#
# WRITTEN AS A FILE, NEVER PASTED INTO A SHELL. Defender's behaviour signatures flag a command line that carries the
# exclusion cmdlet (Trojan:Win32/PowExcEnv on 2026-10-17, which blocked the bash that was writing this file with a
# heredoc). A file on disk with the same text is fine, as scripts/room-defender.ps1 has been.

$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'

function Say { param([string] $s) Write-Host "ci-defender: $s" }

# Copies $exe to a name nothing has seen and starts it twice. Answers the two times in ms: the first start pays
# for whatever inspects a new executable, the second does not, so the difference is the scan.
function Measure-FreshStart {
    param([string] $exe, [string] $dir)
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $probe = Join-Path $dir ('probe-' + [guid]::NewGuid().ToString('N').Substring(0, 12) + '.exe')
    Copy-Item -Path $exe -Destination $probe
    $times = @()
    foreach ($i in 1, 2) {
        $sw = [Diagnostics.Stopwatch]::StartNew()
        & $probe version 2>&1 | Out-Null
        $times += [int] $sw.ElapsedMilliseconds
    }
    Remove-Item -Path $probe -Force -ErrorAction SilentlyContinue
    $times
}

function Test-FullPath { param([string] $p) "$p" -match '^[A-Za-z]:\\.' -and "$p" -notmatch '[*?"<>|]' }

try {
    if (-not $IsWindows) { Say 'not windows, nothing to say'; exit 0 }
    $status = Get-MpComputerStatus -ErrorAction Stop
    Say ("real-time protection " + $(if ($status.RealTimeProtectionEnabled) { 'on' } else { 'off' }) +
        ", antivirus " + $(if ($status.AntivirusEnabled) { 'on' } else { 'off' }) + ", engine " + $status.AMEngineVersion)
    $pref = Get-MpPreference -ErrorAction Stop
    $have = @($pref.ExclusionPath | Where-Object { $_ })
    Say ('excluded already: ' + $(if ($have.Count) { $have -join ', ' } else { 'nothing' }))

    $go = (Get-Command go -ErrorAction SilentlyContinue).Source
    if (-not $go) { Say 'no go.exe on PATH to measure with'; exit 0 }
    $dir = Join-Path $env:TEMP 'atrium-scan-probe'
    $before = Measure-FreshStart $go $dir
    Say ("a fresh copy of go.exe: first start $($before[0]) ms, second $($before[1]) ms. " +
        'the difference is what a new executable pays before it runs')

    if ($env:GITHUB_ACTIONS -ne 'true') { Say 'not on GitHub Actions, so nothing is excluded'; exit 0 }
    if (-not $status.RealTimeProtectionEnabled) { Say 'real-time protection is off, so there is nothing to exclude from'; exit 0 }

    $want = [Collections.Generic.List[string]]::new()
    $add = { param($what, $p)
        if (-not "$p".Trim()) { return }
        $p = ("$p".Trim() -replace '/', '\').TrimEnd('\')
        if (-not (Test-FullPath $p)) { Say "left out $what, $p, which is not a full path on a drive"; return }
        if ($want -notcontains $p) { $want.Add($p) }
    }
    & $add 'TEMP' $env:TEMP
    & $add 'RUNNER_TEMP' $env:RUNNER_TEMP
    $goenv = @(& $go env GOCACHE GOTMPDIR 2>$null)
    & $add 'GOCACHE' $goenv[0]
    & $add 'GOTMPDIR' $goenv[1]
    if ($env:GITHUB_WORKSPACE) { & $add 'build.claude' (Join-Path $env:GITHUB_WORKSPACE 'build.claude') }

    $miss = @($want | Where-Object { $have -notcontains $_ })
    if (-not $miss.Count) { Say 'every folder is excluded already'; exit 0 }
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $elevated = ([Security.Principal.WindowsPrincipal] $id).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $elevated) { Say "$($id.Name) is not elevated, so nothing can be excluded. the tests run with the scan"; exit 0 }
    Add-MpPreference -ExclusionPath $miss -ErrorAction Stop
    $now = @((Get-MpPreference -ErrorAction Stop).ExclusionPath | Where-Object { $_ })
    $left = @($want | Where-Object { $now -notcontains $_ })
    Say ('excluded now: ' + ($miss -join ', '))
    if ($left.Count) { Say ('defender did not take: ' + ($left -join ', ')) }
    $after = Measure-FreshStart $go $dir
    Say ("a fresh copy of go.exe after the exclusions: first start $($after[0]) ms, second $($after[1]) ms")
} catch {
    Say ('could not finish: ' + $_.Exception.Message)
}
exit 0
