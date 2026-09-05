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
    # Where the atrium binary is.
    #
    # DEFAULTS TO AN INSTALLED COPY, not to the build in this repo. That was
    # the first of three defects in this script and it is the one that made the
    # other two hard to see.
    #
    # A path under `build.claude\` is a moving identity: it changes with the
    # checkout, every `go build` rewrites it, and on Windows it cannot be
    # written at all while the daemon is running from it, so rebuilding during a
    # working session fails with a sharing violation that reads like a virus
    # scanner. A logon task pointing there survives exactly until the repo moves.
    #
    # Left empty, this looks for `atrium` on PATH first, which is where a
    # package manager puts it, and falls back to `~\.atrium\bin\atrium.exe` for
    # a copy placed there by hand.
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
    # PATH first: a packaged atrium lands there, and resolving it here means the
    # task records the real target rather than a shim that may be rewritten.
    $onPath = Get-Command atrium -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($onPath) {
        $Exe = $onPath.Source
    } else {
        $Exe = Join-Path $env:USERPROFILE '.atrium\bin\atrium.exe'
    }
}
$Exe = [System.IO.Path]::GetFullPath($Exe)

if (-not (Test-Path -LiteralPath $Exe)) {
    throw "no atrium at $Exe. install it, or pass -Exe with the full path."
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
    throw @"
no atrium binary at $Exe

install one there first, so this task points at something that does not move:
  go build -o build.claude\atrium.exe .\cmd\atrium
  .\build.claude\atrium.exe install

or pass -Exe to point this task somewhere else on purpose.
"@
}

$dbDir = Split-Path -Parent $Db
if (-not (Test-Path $dbDir)) {
    New-Item -ItemType Directory -Path $dbDir -Force | Out-Null
}

# WHO THIS RUNS AS.
#
# The second defect. Both the trigger and the principal took `$env:USERNAME`,
# which is a bare name with no domain. On a machine joined to a domain, or one
# signed in with a Microsoft account, the identity is `DOMAIN\user` or
# `MicrosoftAccount\you@example.com`, and a bare name either fails to register
# or registers against the wrong account and never fires. It happens to work on
# a local-only account, which is why it survived.
#
# The current identity's own name is what Windows itself calls this user, in
# whatever form this machine uses.
$me = [Security.Principal.WindowsIdentity]::GetCurrent().Name

# THE CONSOLE WINDOW.
#
# The third defect was a comment claiming a behaviour the code did not
# implement: it said `-WindowStyle Hidden` and `New-ScheduledTaskAction` has no
# such parameter, so a console window appeared at every logon and stayed there
# to be closed by accident, taking the daemon and every supervised runner with
# it.
#
# A scheduled task's window is controlled by the PRINCIPAL, not the action:
# `-LogonType Interactive` gets a window, `S4U` does not but also cannot open a
# pseudo terminal. So the window is hidden by launching through `conhost.exe
# --headless`, which is the documented way to run a console program with no
# window on Windows 10 1809 and later, and falls back to the plain invocation
# where that is not available.
$conhost = Join-Path $env:SystemRoot 'System32\conhost.exe'
if (Test-Path $conhost) {
    $action = New-ScheduledTaskAction -Execute $conhost `
        -Argument "--headless `"$Exe`" daemon --db `"$Db`""
} else {
    Write-Warning "conhost.exe is not on this machine, so the daemon will have a console window."
    $action = New-ScheduledTaskAction -Execute $Exe -Argument "daemon --db `"$Db`""
}

$trigger = New-ScheduledTaskTrigger -AtLogOn -User $me

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
$principal = New-ScheduledTaskPrincipal -UserId $me -LogonType Interactive

# STOP THE OLD ONE BEFORE REPLACING IT.
#
# `-Force` overwrites the registration and leaves a running instance alone, so
# re-running this script while atrium was up left the old daemon running and
# registered a task that would start a second one at the next logon. Two daemons
# on one database is a mistake the daemon itself warns about, and the warning
# only appears in a log nobody is reading at logon.
#
# The task is stopped, not the process: killing the daemon takes every
# supervised runner with it, and `atrium stop` is the wind-down that does not.
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
    $state = (Get-ScheduledTask -TaskName $TaskName).State
    if ($state -eq 'Running') {
        Write-Host "the existing '$TaskName' task is running. stopping it first."
        Write-Host "  if a daemon is up outside this task, wind it down yourself: atrium stop"
        Stop-ScheduledTask -TaskName $TaskName
    }
}

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
    -Settings $settings -Principal $principal -Force | Out-Null

Write-Host "registered '$TaskName' to start at logon."
Write-Host "  runs:     $Exe daemon --db `"$Db`""
Write-Host "  as:       $me"
Write-Host "  database: $Db"
Write-Host ""
Write-Host "start it now without logging out:"
Write-Host "  Start-ScheduledTask -TaskName $TaskName"
Write-Host ""
Write-Host "take it away again:"
Write-Host "  .\scripts\atrium-autostart.ps1 -Remove"
