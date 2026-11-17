# Registers the scheduled task a Windows room's deploy swap runs through, once per room account, or removes it.
#
# The swap used to run inside the deploy's ssh session, so the new room was a child of that sshd session and outlived
# its client, spinning a core (sg3). Now the deploy writes ~/.atrium/swap-task.ps1 over ssh, runs this task and returns.
# The task runs that file as the room account, owned by the task scheduler, not by sshd.
#
# Manual trigger only: no boot or logon trigger. S4U, so it runs with nobody logged in at the machine and stores no
# password. Registering an S4U task needs an administrator, so run this elevated, naming the room account:
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\room-swap-task.ps1 -User sg3\claude
param(
    [string] $User = [Security.Principal.WindowsIdentity]::GetCurrent().Name,
    [string] $TaskName = 'atrium-swap',
    [switch] $Remove
)
$ErrorActionPreference = 'Stop'

# schtasks.exe, not the ScheduledTask cmdlets, which go through CIM and are denied over ssh (sg3).
function Invoke-Schtasks {
    $ErrorActionPreference = 'Continue'
    $o = & schtasks.exe @args 2>&1
    $script:schtasksExit = $LASTEXITCODE
    $o
}

if ($Remove) {
    $o = Invoke-Schtasks /Delete /TN $TaskName /F
    if ($script:schtasksExit -ne 0) { throw "could not delete '$TaskName': $o" }
    Write-Host "removed '$TaskName'."
    return
}

$sid = ([Security.Principal.NTAccount]$User).Translate([Security.Principal.SecurityIdentifier]).Value
function Esc { param([string] $s) [Security.SecurityElement]::Escape($s) }
$shell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$argument = "-NoProfile -ExecutionPolicy Bypass -Command `"& (Join-Path `$HOME '.atrium\swap-task.ps1')`""
# The descriptor lets the room account run the task an administrator registered. Priority 5 is normal, not the
# scheduler's default 7, which the room and every runner would inherit.
$taskXml = @"
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>atrium deploy swap for $(Esc $User): runs ~\.atrium\swap-task.ps1. Manual start only.</Description>
    <SecurityDescriptor>D:(A;;FA;;;BA)(A;;FA;;;SY)(A;;GRGX;;;$sid)</SecurityDescriptor>
  </RegistrationInfo>
  <Triggers />
  <Principals><Principal id="Author"><UserId>$sid</UserId><LogonType>S4U</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
    <Priority>5</Priority>
  </Settings>
  <Actions Context="Author"><Exec><Command>$(Esc $shell)</Command><Arguments>$(Esc $argument)</Arguments></Exec></Actions>
</Task>
"@

$xmlFile = Join-Path ([IO.Path]::GetTempPath()) "atrium-swap-task-$([guid]::NewGuid().ToString('N')).xml"
try {
    [IO.File]::WriteAllText($xmlFile, $taskXml, [Text.Encoding]::Unicode)
    $o = Invoke-Schtasks /Create /TN $TaskName /XML $xmlFile /F
    if ($script:schtasksExit -ne 0) { throw "schtasks could not register '$TaskName' for ${User}: $o. run this elevated" }
} finally {
    Remove-Item -LiteralPath $xmlFile -Force -ErrorAction SilentlyContinue
}
Write-Host "registered '$TaskName': runs ~\.atrium\swap-task.ps1 as $User, manual start only."
