# Install, remove, start, stop and inspect a room as a Windows service. A thin face over `atrium service`, which
# registers it, and `net`, which starts and stops it.
#
#   .\scripts\atrium-service.ps1 install -Name sg4-claude -User .\claude -Exe C:\Users\claude\.atrium\bin\atrium.exe
#   .\scripts\atrium-service.ps1 status  -Name sg4-claude
#   .\scripts\atrium-service.ps1 stop    -Name sg4-claude
#   .\scripts\atrium-service.ps1 start   -Name sg4-claude
#   .\scripts\atrium-service.ps1 restart -Name sg4-claude
#   .\scripts\atrium-service.ps1 uninstall -Name sg4-claude
#
# Install and uninstall need an administrator. Stopping the service is the room's graceful stop, so the runners the pty
# host holds stay up. `-Start auto` (at boot, the default when not asked) or `-Start manual`. The room's own flags go in
# -RoomArgs. The password comes from $env:ATRIUM_SERVICE_PASSWORD, else the prompt. NO [CmdletBinding()] and NO
# [Parameter()], see scripts/atrium-autostart.ps1.
param(
    [ValidateSet('install', 'uninstall', 'start', 'stop', 'restart', 'status')]
    [string] $Action = 'status',
    [string] $Name,
    [string] $Exe = 'atrium',
    [string] $User,
    [string] $Start,
    [string[]] $RoomArgs = @()
)

$ErrorActionPreference = 'Stop'
if (-not $Name) { throw 'pass -Name, the service name' }

switch ($Action) {
    'install' {
        $a = @('service', 'install', '--name', $Name)
        if ($User) { $a += @('--user', $User) }
        if ($Start) { $a += @('--start', $Start) }
        & $Exe @a -- @RoomArgs
    }
    'uninstall' { & $Exe service uninstall --name $Name }
    'status'    { & $Exe service status --name $Name }
    'start'     { net.exe start $Name }
    'stop'      { net.exe stop $Name }
    'restart'   { net.exe stop $Name; net.exe start $Name }
}
exit $LASTEXITCODE
