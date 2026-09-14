# Bring the atrium daemon back up after it has died.
#
# FOR A DAEMON THAT IS GONE, not for one you want to replace. `atrium stop`
# winds one down and `restart_atrium` swaps a staged binary in, and both of
# those need a daemon that is answering. This is the other case: the process
# has vanished, the board is refusing connections, and every supervised session
# went with it, including the one you would normally ask for a restart from.
#
# Ways that happens: the machine went to sleep, something took the console the
# daemon was launched from, or a crash. A killed daemon never runs its
# wind-down, so the scrollback and the list of open terminals on disk are from
# the last CLEAN stop rather than from a moment ago. This says so rather than
# letting you work it out from a short scrollback.
#
# Run it from anywhere:
#
#     pwsh -File D:\git\github\dovholuknf\atrium\scripts\start-atrium.ps1
#
# It refuses to start a second daemon. Two on one database is the failure this
# is most likely to cause, since the second cannot bind the port, exits, and
# leaves you looking at the first one wondering why nothing changed.

[CmdletBinding()]
param(
    # Install a binary staged by a build that never got its restart. Off by
    # default: starting a daemon and changing which daemon you start are two
    # decisions, and this script exists for the moment when you want the
    # smaller one.
    [switch]$InstallStaged,
    # Where atrium is installed, when it is not under the account running this.
    # `C:\Users\someone\.atrium`, the folder holding `bin` and `atrium.db`.
    [string]$AtriumHome,
    # Start it anyway when the install belongs to another account. See the
    # refusal below for what that costs.
    [switch]$Force,
    # How long to wait for the board to answer before giving up on it.
    [int]$TimeoutSeconds = 30
)

$ErrorActionPreference = 'Stop'

# Find the install using -AtriumHome, ATRIUM_HOME, the current account,
# then other profiles. The daemon may use a dedicated account, so the
# caller's profile is not always the right location.
function Find-AtriumHome {
    param([string]$Explicit)

    if ($Explicit) {
        if (Test-Path (Join-Path $Explicit 'bin\atrium.exe')) { return $Explicit }
        throw "there is no atrium under $Explicit"
    }
    if ($env:ATRIUM_HOME -and (Test-Path (Join-Path $env:ATRIUM_HOME 'bin\atrium.exe'))) {
        return $env:ATRIUM_HOME
    }
    $mine = Join-Path $env:USERPROFILE '.atrium'
    if (Test-Path (Join-Path $mine 'bin\atrium.exe')) { return $mine }

    # Other accounts. A profile this user cannot read throws rather than
    # returning nothing, so each one is tried on its own.
    $found = @()
    foreach ($p in (Get-ChildItem 'C:\Users' -Directory -ErrorAction SilentlyContinue)) {
        try {
            $candidate = Join-Path $p.FullName '.atrium'
            if (Test-Path (Join-Path $candidate 'bin\atrium.exe')) { $found += $candidate }
        } catch { }
    }
    if ($found.Count -eq 1) { return $found[0] }
    if ($found.Count -gt 1) {
        Write-Host "atrium is installed under more than one account:"
        $found | ForEach-Object { Write-Host ("  " + $_) }
        throw "say which with -AtriumHome"
    }
    throw "no atrium install found under any account on this machine"
}

$home_ = Find-AtriumHome -Explicit $AtriumHome
$bin = Join-Path $home_ 'bin'
$exe = Join-Path $bin 'atrium.exe'
$staged = Join-Path $bin 'atrium.next.exe'
$aside = Join-Path $bin 'atrium.old.exe'

# Read the address file from the install owner's local app data. The caller's
# profile could select the wrong database and show an empty board.
$owner = Split-Path (Split-Path $home_ -Parent) -Leaf
$addressFile = Join-Path (Split-Path $home_ -Parent) 'AppData\Local\atrium\daemon.json'

# Starting here runs the daemon and its agents as the current user, even
# when another account owns the install. Require -Force for that case.
if ($owner -ne $env:USERNAME -and -not $Force) {
    Write-Host ("atrium is installed under " + $owner + " and you are " + $env:USERNAME + ".")
    Write-Host "starting it from here would run the daemon, and every agent it spawns, as you."
    Write-Host "start it as that account instead:"
    Write-Host ("  runas /user:" + $owner + " `"pwsh -File " + $PSCommandPath + "`"")
    Write-Host "or pass -Force if running it as yourself is what you meant."
    exit 1
}

function Test-Board {
    param([string]$Board)
    try {
        $null = Invoke-WebRequest -Uri ($Board + '/v1/health') -UseBasicParsing -TimeoutSec 2
        return $true
    } catch {
        return $false
    }
}

# WHERE THE LAST DAEMON WAS, which is also where its database is.
#
# Read rather than assumed. A daemon started with `--db` against another file
# records it here, and starting this one on the default would open a different
# database and show an empty board, which reads as data loss.
$board = 'http://localhost:7778'
$db = Join-Path $home_ 'atrium.db'
if (Test-Path $addressFile) {
    try {
        $where = Get-Content $addressFile -Raw | ConvertFrom-Json
        if ($where.board) { $board = $where.board }
        if ($where.db) { $db = $where.db }
        Write-Host ("last daemon: pid " + $where.pid + " since " + $where.since)
    } catch {
        Write-Host "the address file is unreadable, using the defaults"
    }
}

if (Test-Board -Board $board) {
    Write-Host ("atrium is already up at " + $board + ". nothing to do.")
    Write-Host "to replace it, use restart_atrium or atrium stop."
    exit 0
}

# The port answering nothing is a different problem from the port being free,
# and starting into it produces a daemon that exits immediately.
$held = Get-NetTCPConnection -State Listen -LocalPort 7778 -ErrorAction SilentlyContinue
if ($held) {
    $owner = ($held | Select-Object -First 1).OwningProcess
    $name = (Get-Process -Id $owner -ErrorAction SilentlyContinue).ProcessName
    Write-Host ("port 7778 is held by pid " + $owner + " (" + $name + ") and is not answering.")
    Write-Host "that process has to go before a daemon can start. NOT killing it from here."
    exit 1
}

if ($InstallStaged -and (Test-Path $staged)) {
    # The one moment a running binary can be replaced is when none is running,
    # which is exactly now.
    Remove-Item $aside -Force -ErrorAction SilentlyContinue
    if (Test-Path $exe) { Move-Item $exe $aside -Force }
    Move-Item $staged $exe -Force
    Write-Host "installed the staged binary"
} elseif (Test-Path $staged) {
    Write-Host "a staged binary is waiting. pass -InstallStaged to put it in place."
}

if (-not (Test-Path $exe)) {
    Write-Host ("there is no atrium at " + $exe)
    exit 1
}

# WHAT COMES BACK, said before it happens rather than left to be noticed.
#
# A daemon that was killed never wrote either of these, so both describe the
# last clean stop. Somebody restarting after a crash and finding an hour
# missing deserves to know that here rather than reading it as the scrollback
# being broken again.
$reopen = Join-Path (Split-Path $db -Parent) 'reopen.json'
if (Test-Path $reopen) {
    try {
        $rec = Get-Content $reopen -Raw | ConvertFrom-Json
        Write-Host ("will reopen " + $rec.cards.Count + " terminal(s), recorded at " + $rec.saved_at)
    } catch { }
}

Write-Host ("starting " + $exe + " on " + $db)
Start-Process -FilePath $exe -ArgumentList @('daemon', '--db', $db) -WindowStyle Hidden

$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
while ((Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 500
    if (Test-Board -Board $board) {
        $now = Get-Content $addressFile -Raw | ConvertFrom-Json
        Write-Host ("atrium is up at " + $board + " as pid " + $now.pid)
        Write-Host "fixtures come up first, then whatever else was open. give it a few seconds."
        exit 0
    }
}

Write-Host ("no answer from " + $board + " after " + $TimeoutSeconds + " seconds.")
Write-Host "it may have failed to open the database, which is the one thing it refuses to start without."
exit 1
