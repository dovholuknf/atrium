# What provision-room.ps1 runs ON A WINDOWS REMOTE to install the logon task and bring the room up, dot-sourced by it and
# by scripts/test-room-start.ps1. It holds functions and runs nothing when it is dot-sourced.
#
# THE LOGON TASK IS INTERACTIVE, so `schtasks /Run` does nothing while nobody is logged in at the machine (last result
# 267011, 0x41303, "has not yet run"), and a machine reached only over ssh is exactly that. Then the room is started with
# `room --detach`, which survives the ssh session, and the line is `start=warn <why>`: the task starts the room at the next
# logon. The run goes on to the git step, auth and the smoke. Only when `room --detach` fails too is it `start=fail`.

# Get-WindowsAutostartScript is the remote script: install the task when it is not there, and start the room when it is
# not answering. Its keys: autostart=ok|done, start=ok|done|warn|fail <detail>, and started=1 when this run started the room
# with `room --detach`, so the attach wait wants a connection made after now.
function Get-WindowsAutostartScript {
@'
$svc = Get-AT
if ($svc -and $svc -like "*$Bin*room --*") { "autostart=ok" }
else {
    $o = & (Join-Path $P 'scripts\atrium-service.ps1') install -Verb room -Exe $Bin *>&1
    if (-not (Get-AT)) { $o; exit 1 }
    "autostart=done"
}
if (-not $StartWait) { $StartWait = 30 }
function Test-RoomUp { try { $null = Invoke-RestMethod http://127.0.0.1:7781/v1/health -TimeoutSec 2; $true } catch { $false } }
if (Test-RoomUp) { "start=ok" } else {
    $null = Sch /Run /TN atrium
    $deadline = (Get-Date).AddSeconds($StartWait)
    $up = $false
    while (-not $up -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 1; $up = Test-RoomUp }
    if ($up) { "start=done" } else {
        $lr = (Sch /Query /TN atrium /V /FO LIST | Where-Object { "$_" -match '^Last Result:\s*(.+)$' } | ForEach-Object { $Matches[1].Trim() } | Select-Object -First 1)
        $why = if ("$lr" -match '^(267011|0x0*41303)$') { "the logon task cannot run now, it is Interactive and nobody is logged in at the machine (last result $lr)" } else { "the logon task did not bring the room up (last result $lr)" }
        # THE FALLBACK: room --detach, the path a started-by-hand room takes, after the toolchain's room-env.ps1 when there is one.
        $ErrorActionPreference = 'Continue'
        $e = Join-Path $HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path $e) { . $e }
        $null = & $Bin room --detach 2>&1
        $deadline = (Get-Date).AddSeconds($StartWait)
        while (-not $up -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 1; $up = Test-RoomUp }
        if ($up) { "start=warn $why. started with room --detach instead, which runs until the machine restarts or the user logs out. the task starts it at the next logon"; "started=1" }
        else { "start=fail $why, and room --detach did not bring it up either" }
    }
}
'@
}
