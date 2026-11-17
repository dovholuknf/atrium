# Tests that scripts/mock-live.ps1 leaves no process of its mock behind: not on -Stop, and not when the start fails.
# Needs no build, no hub and no room:
#
#   pwsh -NoProfile -File scripts/test-mock-live.ps1
#
# A fake pty host (a copy of cmd.exe named atrium-pty.exe in the mock's bin dir) holds a fake card (pwsh, sleeping),
# the way the room's real pty host holds the card terminals. Both must be gone after -Stop, and after a start that
# throws on a branch that does not exist.
$ErrorActionPreference = 'Stop'

$script:failed = 0
$script:ran = 0
function Check {
    param([string] $name, $got, $want)
    $script:ran++
    $g = (@($got) | ForEach-Object { "$_" }) -join '|'
    $w = (@($want) | ForEach-Object { "$_" }) -join '|'
    if ($g -ceq $w) { Write-Host "ok   $name" }
    else { $script:failed++; Write-Host "FAIL $name`n     want: $w`n     got:  $g" }
}

$Pwsh = (Get-Process -Id $PID).Path
$Mock = Join-Path $PSScriptRoot 'mock-live.ps1'
$Root = (Join-Path $PSScriptRoot "../build.claude/test-mock-live-$PID") -replace '\\', '/'
$Port = 7980
$Data = "$Root/.uncommitted/mock/$Port"

function Wait-For([scriptblock] $what, [int] $seconds = 15) {
    $deadline = (Get-Date).AddSeconds($seconds)
    while ((Get-Date) -lt $deadline) {
        $got = & $what
        if ($got) { return $got }
        Start-Sleep -Milliseconds 100 # wait: polling a condition under a deadline
    }
    $null
}

# Returns the pids of the fake host and its fake card.
function Start-FakeMock {
    New-Item -ItemType Directory -Force "$Data/bin" | Out-Null
    Copy-Item "$env:SystemRoot/System32/cmd.exe" "$Data/bin/atrium-pty.exe"
    $line = '/d /s /c ""' + $Pwsh + '" -NoLogo -NoProfile -Command Start-Sleep 300"'
    $hostProc = Start-Process "$Data/bin/atrium-pty.exe" -ArgumentList $line -WindowStyle Hidden -PassThru
    $card = Wait-For { Get-Process -Name pwsh | Where-Object { $_.Parent -and $_.Parent.Id -eq $hostProc.Id } }
    if (-not $card) { throw "the fake card did not start under the fake pty host" }
    @($hostProc.Id, $card.Id)
}

function Alive([int[]] $ids) { @($ids | Where-Object { Get-Process -Id $_ -ErrorAction SilentlyContinue }).Count }

$started = @()
try {
    $started = Start-FakeMock
    Check 'setup: fake host and card are running' (Alive $started) 2
    & $Pwsh -NoLogo -NoProfile -File $Mock -Port $Port -Repo $Root -Stop *> $null
    Check '-Stop: exits 0' $LASTEXITCODE 0
    Check '-Stop: no fake host or card left' (Alive $started) 0
    Check '-Stop: the data dir is gone' (Test-Path $Data) $false

    $started = Start-FakeMock
    & $Pwsh -NoLogo -NoProfile -File $Mock -Port $Port -Repo $Root -Branch 'no-such-branch/test-mock-live' *> $null
    Check 'failed start: exits non-zero' ($LASTEXITCODE -ne 0) $true
    Check 'failed start: no fake host or card left' (Alive $started) 0
    Check 'failed start: the data dir is gone' (Test-Path $Data) $false
} finally {
    foreach ($id in $started) { Stop-Process -Id $id -Force -ErrorAction SilentlyContinue }
    $null = Wait-For { (Alive $started) -eq 0 }
    Remove-Item -Recurse -Force $Root -ErrorAction SilentlyContinue
}

Write-Host "$($script:ran - $script:failed)/$($script:ran) passed"
if ($script:failed -gt 0) { exit 1 }
