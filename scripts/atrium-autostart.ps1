# Register atrium to start when you log in, or take that registration away.
#
# Why this exists: the daemon on this machine was started by hand once, months
# ago, and stayed up. Nothing brought it back, so `atrium stop` looked like
# losing everything, and starting it again from a different shell opened a
# DIFFERENT DATABASE, because which one you get depends on WORKTREE_ROOT in the
# environment you happened to be in.
#
# Both halves of that are fixed by pinning it down once: one command line, one
# database, started the same way every time.
#
# A scheduled task rather than a Windows service. A service runs as SYSTEM in
# session 0, which cannot open a pseudo terminal a person can attach to, and
# supervision is most of what the daemon is for. A logon task runs as you, in
# your session, with your PATH, which is what a runner needs.

[CmdletBinding()]
param(
    # Where the atrium binary is. Defaults to the build in this repo.
    [string] $Exe,

    # Which database. **Passed explicitly on purpose.** Leaving it out means
    # the task inherits whatever WORKTREE_ROOT happens to be at logon, which is
    # the thing that caused the confusion this script exists to end.
    [string] $Db = (Join-Path $env:USERPROFILE '.atrium\atrium.db'),

    [string] $TaskName = 'atrium',

    # Remove the task instead of creating it.
    [switch] $Remove
)

$ErrorActionPreference = 'Stop'

if (-not $Exe) {
    $repo = Split-Path -Parent $PSScriptRoot
    $Exe = Join-Path $repo 'build.claude\atrium.exe'
}

if ($Remove) {
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
        Write-Host "removed the '$TaskName' task. nothing starts atrium at logon now."
    } else {
        Write-Host "there is no '$TaskName' task to remove."
    }
    return
}

if (-not (Test-Path $Exe)) {
    throw "no atrium binary at $Exe. build it first: go build -o build.claude\atrium.exe .\cmd\atrium"
}

$dbDir = Split-Path -Parent $Db
if (-not (Test-Path $dbDir)) {
    New-Item -ItemType Directory -Path $dbDir -Force | Out-Null
}

# -WindowStyle Hidden because a console window that has to stay open is a
# console window somebody eventually closes.
$action = New-ScheduledTaskAction -Execute $Exe -Argument "daemon --db `"$Db`""
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME

# No time limit: this is meant to run all day. The default is three days, after
# which the task host stops it and the board vanishes for no visible reason.
$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -RestartCount 3 `
    -RestartInterval (New-TimeSpan -Minutes 1) `
    -StartWhenAvailable

# Interactive, so the daemon runs as you in your own session and can open a
# pseudo terminal. A task that runs whether or not you are logged on cannot.
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
    -Settings $settings -Principal $principal -Force | Out-Null

Write-Host "registered '$TaskName' to start at logon."
Write-Host "  runs:     $Exe daemon --db `"$Db`""
Write-Host "  database: $Db"
Write-Host ""
Write-Host "start it now without logging out:"
Write-Host "  Start-ScheduledTask -TaskName $TaskName"
Write-Host ""
Write-Host "take it away again:"
Write-Host "  .\scripts\atrium-autostart.ps1 -Remove"
