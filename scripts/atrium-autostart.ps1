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

# NO [CmdletBinding()], AND THAT IS NOT AN OVERSIGHT. It was here, and it meant
# this script could not run at all:
#
#   The parameter 'Db' cannot be specified because it conflicts with the
#   parameter alias of the same name for parameter 'Debug'.
#
# CmdletBinding adds the common parameters, and -Debug carries the alias `db`.
# A parameter named $Db collides with it and PowerShell refuses to bind ANY
# invocation, including `-Remove`, with a message about an alias nobody wrote.
# The script was written, reviewed and documented and had never been executed.
#
# The alternative is renaming the parameter to $Database, which changes the
# thing every doc and every muscle memory says. Dropping CmdletBinding costs
# -Verbose and -WhatIf, neither of which this uses.
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

    # The two listen addresses, and where this daemon records itself.
    #
    # ALL THREE DEFAULT TO EMPTY, which means the flag is not passed at all and
    # the daemon uses its own defaults. That is deliberate: writing `--addr
    # :7777` into the task would freeze today's default into a registration
    # that outlives it.
    #
    # They exist because a SECOND registration has to be possible without
    # damaging the first. Testing this script otherwise means registering a task
    # that starts a daemon on the ports the real one is already using, against
    # the same database, and stealing the location file every hook reads to find
    # a port. `scripts/atrium-service.ps1 selftest` uses all three.
    [string] $Addr,
    [string] $Http,
    [string] $LocationFile,

    # What the task runs. `daemon` is a machine's own atrium with no hub. `room`
    # is the same daemon attached to the hub this machine already joined, which
    # is what scripts/provision-room.ps1 installs. For a room, -Addr is its
    # agent address and -Http its own board, and -LocationFile does not apply.
    [ValidateSet('daemon', 'room')]
    [string] $Verb = 'daemon',

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

# schtasks.exe, NOT THE ScheduledTask CMDLETS. Those go through CIM, and a session
# that arrived over ssh is denied CIM ("Cannot connect to CIM server. Access
# denied", seen on sg3). schtasks.exe talks to the task scheduler directly, works
# in every session, and registers a logon task for your own account without
# admin. Registration is by XML because the flags cannot say everything the
# settings below do.
#
# Continue, because Windows PowerShell with Stop turns the first line a native
# command writes to stderr, which is how schtasks says "no such task", into a
# terminating error.
function Invoke-Schtasks {
    $ErrorActionPreference = 'Continue'
    $o = & schtasks.exe @args 2>&1
    $script:schtasksExit = $LASTEXITCODE
    $o
}
function Test-TaskExists {
    $null = Invoke-Schtasks /Query /TN $TaskName
    $script:schtasksExit -eq 0
}
function Get-TaskStatus {
    $o = Invoke-Schtasks /Query /TN $TaskName /FO LIST
    ($o | Where-Object { $_ -match '^Status:\s*(.+)$' } | ForEach-Object { $Matches[1].Trim() } | Select-Object -First 1)
}

if ($Remove) {
    if (Test-TaskExists) {
        $null = Invoke-Schtasks /End /TN $TaskName
        $o = Invoke-Schtasks /Delete /TN $TaskName /F
        if ($script:schtasksExit -ne 0) { throw "could not delete the '$TaskName' task: $o" }
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
if ($Verb -eq 'room') {
    $daemonArgs = "room --db `"$Db`""
    if ($Addr)         { $daemonArgs += " --agent `"$Addr`"" }
    if ($Http)         { $daemonArgs += " --http `"$Http`"" }
    if ($LocationFile) { Write-Warning "-LocationFile does not apply to a room and was left out." }
} else {
    $daemonArgs = "daemon --db `"$Db`""
    if ($Addr)         { $daemonArgs += " --addr `"$Addr`"" }
    if ($Http)         { $daemonArgs += " --http `"$Http`"" }
    if ($LocationFile) { $daemonArgs += " --location-file `"$LocationFile`"" }
}

$conhost = Join-Path $env:SystemRoot 'System32\conhost.exe'
if ($Verb -eq 'room') {
    # A ROOM'S TASK STARTS THE DETACHED ROOM, so a room started by hand, by provision and by a logon is one path:
    # `room --detach` returns once the room answers. The toolchain's room-env.ps1 is dot-sourced first when
    # room-toolchain.ps1 wrote one, for the PATH its runners need. The command stays readable text, because
    # provision-room.ps1 reads the task back to see whether it is already right.
    # Windows PowerShell 5.1 runs this over ssh, so no -replace with a scriptblock.
    $detached = ($daemonArgs -replace '^room ', 'room --detach ') -replace '"', "'"
    $ps = "`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }; & '$($Exe -replace "'", "''")' $detached"
    $shell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $shellArgs = "-NoProfile -ExecutionPolicy Bypass -Command `"$($ps -replace '"', '\"')`""
    if (Test-Path $conhost) { $command = $conhost; $argument = "--headless `"$shell`" $shellArgs" }
    else { $command = $shell; $argument = "-WindowStyle Hidden $shellArgs" }
} elseif (Test-Path $conhost) {
    $command = $conhost
    $argument = "--headless `"$Exe`" $daemonArgs"
} else {
    Write-Warning "conhost.exe is not on this machine, so the daemon will have a console window."
    $command = $Exe
    $argument = $daemonArgs
}

# The task, as XML. What each setting is for:
#   LogonTrigger for this user   starts when you log in.
#   ExecutionTimeLimit PT0S      no time limit: this is meant to run all day. The
#                                default is three days, after which the task host
#                                stops it and the board vanishes for no visible reason.
#   RestartOnFailure             three tries a minute apart.
#   InteractiveToken, Least...   Interactive, so the daemon runs as you in your own
#                                session and can open a pseudo terminal. A task that
#                                runs whether or not you are logged on cannot. No
#                                elevation.
function Esc { param([string] $s) [Security.SecurityElement]::Escape($s) }
$taskXml = @"
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>$(Esc $me)</UserId></LogonTrigger></Triggers>
  <Principals><Principal id="Author"><UserId>$(Esc $me)</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author"><Exec><Command>$(Esc $command)</Command><Arguments>$(Esc $argument)</Arguments></Exec></Actions>
</Task>
"@

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
if (Test-TaskExists) {
    if ((Get-TaskStatus) -eq 'Running') {
        Write-Host "the existing '$TaskName' task is running. stopping it first."
        Write-Host "  if a daemon is up outside this task, wind it down yourself: atrium stop"
        $null = Invoke-Schtasks /End /TN $TaskName
    }
}

$xmlFile = Join-Path ([IO.Path]::GetTempPath()) "atrium-task-$([guid]::NewGuid().ToString('N')).xml"
try {
    # UTF-16 with a BOM, which is what the XML above declares and what schtasks reads.
    [IO.File]::WriteAllText($xmlFile, $taskXml, [Text.Encoding]::Unicode)
    $o = Invoke-Schtasks /Create /TN $TaskName /XML $xmlFile /F
    if ($script:schtasksExit -ne 0) { throw "schtasks could not register '$TaskName': $o" }
} finally {
    Remove-Item -LiteralPath $xmlFile -Force -ErrorAction SilentlyContinue
}

Write-Host "registered '$TaskName' to start at logon."
Write-Host "  runs:     $Exe $daemonArgs"
Write-Host "  as:       $me"
Write-Host "  database: $Db"
Write-Host ""
Write-Host "start it now without logging out:"
Write-Host "  schtasks /Run /TN $TaskName"
Write-Host ""
Write-Host "take it away again:"
Write-Host "  .\scripts\atrium-autostart.ps1 -Remove"
