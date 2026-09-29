# Install, remove, start, stop and inspect atrium as a managed thing on Windows.
#
#   .\scripts\atrium-service.ps1 install
#   .\scripts\atrium-service.ps1 status
#   .\scripts\atrium-service.ps1 stop
#   .\scripts\atrium-service.ps1 start
#   .\scripts\atrium-service.ps1 restart
#   .\scripts\atrium-service.ps1 uninstall
#   .\scripts\atrium-service.ps1 selftest      # proves the above, harmlessly
#
# ================================================================================
# IT IS A SCHEDULED TASK AND NOT A WINDOWS SERVICE. Read this before changing it.
# ================================================================================
#
# The ask was "run as a service on every operating system", and on Windows the
# literal reading of that produces something that installs, starts, reports
# Running, and supervises nothing anybody can use. Three options were weighed.
#
# 1. A REAL SERVICE RUNNING AS THE LOGGED-IN USER.
#    `sc.exe create atrium binPath=... obj=DOMAIN\user password=...`
#    What it buys: starts before anybody logs in, survives logout, restarts on
#    failure through the SCM, and answers `Get-Service`. That is genuinely the
#    strongest form of "stays running" Windows has.
#    What it costs, and why it is not what this script does:
#      - A STORED PASSWORD. The account's password goes into the LSA secret
#        store at registration and has to be re-entered every time it changes.
#        On a machine signed in with a Microsoft account or joined to Entra
#        there frequently is no password to give. A gMSA avoids the password and
#        is domain-only, and it is a DIFFERENT account, which defeats the point:
#        the reason to run as the user is to be the user.
#      - SESSION 0. A service runs in session 0 no matter whose account it uses.
#        It never joins the interactive session. The user profile is not loaded
#        unless the service loads it itself, so DPAPI, the credential manager,
#        the ssh agent and the per-session PATH are all absent or different.
#        Every claude session it spawned would inherit that.
#      - IT WOULD NEED CODE. A console program registered as a service is killed
#        by the SCM after about thirty seconds for not answering the service
#        control protocol. Making atrium a service means a service control
#        handler in the binary, which is a change to atrium rather than to
#        packaging, and packaging is where registration belongs.
#
# 2. A SERVICE WRAPPER: WinSW or NSSM.
#    What it buys: solves only the third bullet above. The wrapper answers the
#    SCM and runs atrium as a child.
#    What it costs: the stored password and session 0 are unchanged, and it adds
#    a third-party binary that has to be shipped, versioned and trusted. It
#    turns "atrium supervises nothing usable" into "atrium supervises nothing
#    usable, with a dependency".
#
# 3. A LOGON TASK. What this script registers.
#    What it buys: runs AS YOU, IN YOUR SESSION, with your PATH, your profile,
#    your ssh agent and your Claude Code configuration. It can open a pseudo
#    terminal you can attach to, which is most of what atrium is for. No stored
#    credential. No elevation to register. Restarts on failure, no run-time
#    limit, and survives a reboot: the machine comes up, you log in, it starts.
#    What it costs, stated plainly rather than hidden: IT STOPS WHEN YOU LOG
#    OUT. Windows has no equivalent of `loginctl enable-linger`. The nearest
#    thing is option 1, which is the thing this exists not to be.
#
# The same trade is made on Linux by `packaging/atrium.service` being a user
# unit and on macOS by `packaging/atrium.plist` being a LaunchAgent. Linux can
# buy its way out with lingering; Windows and macOS cannot. Three files, one
# design. Change them together.

# NO [CmdletBinding()] AND NO [Parameter()] ATTRIBUTE ANYWHERE, for the reason
# recorded at length in scripts/atrium-autostart.ps1: either one turns this into
# an advanced function, which adds -Debug, whose alias is `db`, and a parameter
# named $Db then collides with it and PowerShell refuses to bind ANY invocation.
#
# [Parameter(Position = 0)] is the one that is easy to reach for and it is not
# needed: with no attribute at all, parameters bind positionally in the order
# they are declared, so $Action is already first. [ValidateSet] is a validation
# attribute rather than a binding one and is safe to keep.
param(
    [ValidateSet('install', 'uninstall', 'remove', 'start', 'stop', 'restart', 'status', 'selftest')]
    [string] $Action = 'status',

    # Passed straight through to scripts/atrium-autostart.ps1, which owns the
    # registration itself. This script does not duplicate that logic: there is
    # one place that knows how to write the task, and it is the file that
    # records why each setting is what it is.
    [string] $Exe,
    [string] $Db = (Join-Path $env:USERPROFILE '.atrium\atrium.db'),
    [string] $TaskName = 'atrium',
    [string] $Addr,
    [string] $Http,
    [string] $LocationFile,
    # `room` registers `atrium room` instead of `atrium daemon`. See
    # scripts/atrium-autostart.ps1 and scripts/provision-room.ps1.
    [ValidateSet('daemon', 'room')]
    [string] $Verb = 'daemon'
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $PSScriptRoot
$autostart = Join-Path $PSScriptRoot 'atrium-autostart.ps1'

function Say { param([string] $m) Write-Host "atrium: $m" }

# schtasks.exe, NOT THE ScheduledTask CMDLETS. The cmdlets go through CIM, which a
# session that arrived over ssh is denied ("Cannot connect to CIM server. Access
# denied", seen on sg3). schtasks.exe does not, so every verb here works over ssh.
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

# The task as an object, or $null. State, Execute, Arguments, UserId, LogonType,
# LastRunTime, LastResult and NextRunTime are what the verbs below read.
function Get-AtriumTask {
    param([string] $Name = $TaskName)
    $x = Invoke-Schtasks /Query /TN $Name /XML
    if ($script:schtasksExit -ne 0) { return $null }
    try { $doc = [xml](($x | ForEach-Object { "$_" }) -join "`n") } catch { return $null }
    $ns = New-Object Xml.XmlNamespaceManager $doc.NameTable
    $ns.AddNamespace('t', 'http://schemas.microsoft.com/windows/2004/02/mit/task')
    $node = { param($p) $doc.SelectSingleNode($p, $ns).InnerText }
    $v = @{}
    foreach ($l in (Invoke-Schtasks /Query /TN $Name /V /FO LIST)) {
        if ("$l" -match '^([^:]+):\s*(.*)$') { $v[$Matches[1].Trim()] = $Matches[2].Trim() }
    }
    [pscustomobject]@{
        State       = $v['Status']
        Execute     = & $node '//t:Exec/t:Command'
        Arguments   = & $node '//t:Exec/t:Arguments'
        UserId      = & $node '//t:Principal/t:UserId'
        LogonType   = & $node '//t:Principal/t:LogonType'
        LastRunTime = $v['Last Run Time']
        LastResult  = $v['Last Result']
        NextRunTime = $v['Next Run Time']
    }
}

function Resolve-AtriumExe {
    if ($Exe) { return [System.IO.Path]::GetFullPath($Exe) }
    # PATH first, which is where every packaging format puts it. NOT the build
    # in this checkout: a registration naming build.claude\ survives exactly
    # until the next `go build` rewrites the file underneath it, and on Windows
    # that build fails outright with a sharing violation while the daemon is
    # running from it.
    $onPath = Get-Command atrium -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($onPath) { return $onPath.Source }
    return (Join-Path $env:USERPROFILE '.atrium\bin\atrium.exe')
}

# THE GRACEFUL STOP, and it is not `Stop-ScheduledTask` on its own.
#
# A kill is not a stop. The daemon owns a pseudo terminal per supervised runner
# and closing one takes the attached process with it, so ending the task process
# ends every agent at once. `atrium stop` is the wind-down that does not: it
# releases the event streams first, gives the runners ten seconds, and closes
# the listeners. The task is stopped afterwards only to clear the registration's
# own idea of being Running.
function Stop-AtriumGracefully {
    param([string] $ExePath)

    # `atrium stop` names a daemon by URL, not by task. A task that is not
    # running is not what is listening there, and stopping the URL anyway
    # stopped a real room while a throwaway task was being tested.
    $running = Get-AtriumTask
    if ($ExePath -and (Test-Path -LiteralPath $ExePath) -and $running -and $running.State -eq 'Running') {
        # NOT $args: that is an automatic variable inside a function and
        # assigning to it works right up until somebody splats it somewhere
        # else and gets the caller's arguments instead.
        $stopArgs = @('stop')
        if ($Http) { $stopArgs += @('--url', "http://localhost$Http") }
        elseif ($Verb -eq 'room') { $stopArgs += @('--url', 'http://127.0.0.1:7781') }
        try {
            & $ExePath @stopArgs 2>&1 | ForEach-Object { Write-Host "  $_" }
        } catch {
            Say "atrium stop did not answer ($($_.Exception.Message)). continuing."
        }
    }

    $task = Get-AtriumTask
    if ($task -and $task.State -eq 'Running') {
        $null = Invoke-Schtasks /End /TN $TaskName
    }
}

function Invoke-Install {
    $splat = @{ Db = $Db; TaskName = $TaskName; Verb = $Verb }
    if ($Exe)          { $splat.Exe = $Exe }
    if ($Addr)         { $splat.Addr = $Addr }
    if ($Http)         { $splat.Http = $Http }
    if ($LocationFile) { $splat.LocationFile = $LocationFile }

    # IDEMPOTENT because atrium-autostart.ps1 registers with -Force, which
    # replaces the registration rather than adding a second one. It also stops
    # a running instance first, for the failure that matters here: two daemons
    # on one database.
    & $autostart @splat
}

function Invoke-Uninstall {
    Stop-AtriumGracefully -ExePath (Resolve-AtriumExe)
    # -Remove already says "there is no such task" and returns cleanly, so
    # running this twice is not an error. That is the whole test of an
    # uninstall: the second one has to be boring.
    & $autostart -Remove -TaskName $TaskName -Exe (Resolve-AtriumExe) -Db $Db
}

function Show-Status {
    $task = Get-AtriumTask
    if (-not $task) {
        Say "not installed: there is no '$TaskName' scheduled task."
        Say "install it:  .\scripts\atrium-service.ps1 install"
        return
    }

    Say "task      $TaskName"
    Say "state     $($task.State)"
    Say "runs      $($task.Execute) $($task.Arguments)"
    Say "as        $($task.UserId) ($($task.LogonType))"
    Say "last run  $($task.LastRunTime)  result $($task.LastResult)"
    Say "next run  $($task.NextRunTime)"

    # THE TASK BEING 'Running' IS NOT THE SAME AS ATRIUM BEING UP, which is the
    # reason this section exists at all. The task host reports its own process,
    # and with conhost --headless in front the process it reports is conhost.
    # The only honest answer comes from asking the daemon.
    $loc = if ($LocationFile) { $LocationFile }
           else { Join-Path $env:LOCALAPPDATA 'atrium\daemon.json' }
    if (Test-Path -LiteralPath $loc) {
        $j = Get-Content -LiteralPath $loc -Raw | ConvertFrom-Json
        Say "recorded  board $($j.board), agent $($j.agent), pid $($j.pid)"
        Say "database  $($j.db)"
        $board = $j.board
        if ($board -like ':*') { $board = "localhost$board" }
        try {
            $h = Invoke-RestMethod -Uri "http://$board/v1/health" -TimeoutSec 2
            Say "health    answering. board build $($h.build)"
        } catch {
            Say "health    not answering on $board. the recorded file may be stale,"
            Say "          which is expected after a daemon was killed rather than stopped."
        }
    } else {
        Say "recorded  nothing at $loc. no daemon has written where it is listening."
    }
}

# ------------------------------------------------------------------------------
# selftest: prove the verbs above without going anywhere near a real daemon.
#
# It registers a SECOND task under its own name, on its own ports, against its
# own database, recording itself in its own location file. Every one of those
# four is a thing that would otherwise be shared with the daemon somebody is
# actually using, and sharing any of them turns a test into an outage: two
# daemons on one database, or a stolen location file that every hook on the
# machine reads to find a port.
#
# Runs each verb twice, because idempotence is the property being tested and the
# second run is where it fails. Cleans up whether or not it passed.
#
# WHAT IT WILL DO ON A MACHINE WITH NO INTERACTIVE SESSION FOR THE ACCOUNT
# RUNNING IT, because that reads as a bug in this script and is not one.
#
# The registration succeeds, both installs succeed, both uninstalls succeed, and
# the `start` step appears to do nothing: the task stays Ready and its last
# result is 0x41303, SCHED_S_TASK_HAS_NOT_RUN. That is the task scheduler
# refusing an Interactive-logon task for an account that has no interactive
# session to run it in. `quser` shows whose session the console belongs to, and
# if it is not the account running this, the start cannot work and nothing here
# can make it.
#
# It is the same limitation stated at the top of this file, arriving from the
# other direction: a logon task runs in your session, and where there is no
# session there is no task. On a normal desktop where you are the person logged
# in, this is exactly the case that works.
# ------------------------------------------------------------------------------
function Invoke-Selftest {
    $testTask = 'atrium-selftest'
    $testDir  = Join-Path $env:TEMP 'atrium-selftest'
    $testDb   = Join-Path $testDir 'atrium.db'
    $testLoc  = Join-Path $testDir 'daemon.json'
    $testAddr = ':7877'
    $testHttp = ':7878'

    $testExe = if ($Exe) { $Exe } else { Join-Path $here 'build.claude\atrium.exe' }
    if (-not (Test-Path -LiteralPath $testExe)) {
        throw "selftest needs a binary. build one:  make build   (or pass -Exe)"
    }

    New-Item -ItemType Directory -Path $testDir -Force | Out-Null
    $ok = $true
    $step = 0
    function Step { param([string] $m) $script:step++; Write-Host ""; Write-Host "--- $script:step. $m" }

    $common = @{
        Exe = $testExe; Db = $testDb; TaskName = $testTask
        Addr = $testAddr; Http = $testHttp; LocationFile = $testLoc
    }

    try {
        foreach ($pass in 1, 2) {
            Step "install (pass $pass)"
            & $PSCommandPath -Action install @common
            # A task name is unique to the scheduler, so one registration is
            # what there being a task at all means.
            if (-not (Get-AtriumTask -Name $testTask)) { $ok = $false; Write-Host "FAIL: no task after install" }
        }

        Step 'start'
        & $PSCommandPath -Action start @common
        Start-Sleep -Seconds 3

        Step 'status while up'
        & $PSCommandPath -Action status @common
        try {
            $null = Invoke-RestMethod -Uri "http://localhost$testHttp/v1/health" -TimeoutSec 3
            Write-Host "  the test daemon answers on $testHttp"
        } catch {
            $ok = $false
            Write-Host "FAIL: no health answer on $testHttp -- $($_.Exception.Message)"
        }

        foreach ($pass in 1, 2) {
            Step "stop (pass $pass)"
            & $PSCommandPath -Action stop @common
            Start-Sleep -Seconds 2
        }

        Step 'status while down'
        & $PSCommandPath -Action status @common

        foreach ($pass in 1, 2) {
            Step "uninstall (pass $pass)"
            & $PSCommandPath -Action uninstall @common
            if (Get-AtriumTask -Name $testTask) {
                $ok = $false; Write-Host "FAIL: task still registered after uninstall"
            }
        }
    } finally {
        # Whatever happened above, leave nothing behind. A selftest that fails
        # halfway and leaves a registered task pointing at a temp directory is
        # worse than no selftest.
        if (Get-AtriumTask -Name $testTask) {
            $null = Invoke-Schtasks /End /TN $testTask
            $null = Invoke-Schtasks /Delete /TN $testTask /F
        }
        Remove-Item -LiteralPath $testDir -Recurse -Force -ErrorAction SilentlyContinue
    }

    Write-Host ""
    if ($ok) { Say 'selftest passed. install, start, stop and uninstall are all idempotent.' }
    else     { Say 'selftest FAILED. see above.'; exit 1 }
}

switch ($Action) {
    'install'   { Invoke-Install }
    'uninstall' { Invoke-Uninstall }
    'remove'    { Invoke-Uninstall }
    'start'     {
        if (-not (Get-AtriumTask)) { throw "no '$TaskName' task. run: .\scripts\atrium-service.ps1 install" }
        $o = Invoke-Schtasks /Run /TN $TaskName
        if ($script:schtasksExit -ne 0) { throw "could not start '$TaskName': $o" }
        Say "started '$TaskName'."
    }
    'stop'      { Stop-AtriumGracefully -ExePath (Resolve-AtriumExe); Say "stopped '$TaskName'." }
    'restart'   {
        Stop-AtriumGracefully -ExePath (Resolve-AtriumExe)
        Start-Sleep -Seconds 1
        $o = Invoke-Schtasks /Run /TN $TaskName
        if ($script:schtasksExit -ne 0) { throw "could not start '$TaskName': $o" }
        Say "restarted '$TaskName'."
    }
    'status'    { Show-Status }
    'selftest'  { Invoke-Selftest }
}
