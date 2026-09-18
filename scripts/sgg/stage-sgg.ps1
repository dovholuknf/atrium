# Copy the atrium2 binary to sgg over the existing SSH path. Idempotent.
#
# sgg is Windows amd64, same as SG4, so the binary built here runs there with no
# cross-compile. This only stages the file. It starts nothing on sgg and opens
# no ports. Bringing the room up is bringup-sgg.md, and it needs an overlay
# client on sgg first (see that file).
#
#     pwsh -File scripts\sgg\stage-sgg.ps1

[CmdletBinding()]
param(
  [string]$LocalBinary = "$PSScriptRoot\..\..\build.claude\atrium2.exe",
  [string]$SshHost = 'sgg',
  [string]$RemoteDir = 'C:\Users\localai\.atrium2\bin',
  [string]$RemoteBinary = 'C:\Users\localai\.atrium2\bin\atrium2.exe'
)
$ErrorActionPreference = 'Stop'

# Use Windows OpenSSH explicitly. On this box `scp` on PATH is the msys64 build,
# which shells out to `/usr/bin/ssh` and cannot talk to a Windows remote. ssh and
# scp have to be the same family, so both are pinned to System32\OpenSSH.
$sshExe = "$env:WINDIR\System32\OpenSSH\ssh.exe"
$scpExe = "$env:WINDIR\System32\OpenSSH\scp.exe"
if (-not (Test-Path $sshExe)) { $sshExe = 'ssh' }
if (-not (Test-Path $scpExe)) { $scpExe = 'scp' }

if (-not (Test-Path $LocalBinary)) {
  Write-Host "no binary at $LocalBinary. build it first: go build -o build.claude\atrium2.exe ./cmd/atrium2"
  exit 1
}

Write-Host "ensuring $RemoteDir on $SshHost"
& $sshExe -o BatchMode=yes -o ConnectTimeout=10 $SshHost "pwsh -NoProfile -Command `"New-Item -ItemType Directory -Force '$RemoteDir' | Out-Null`""
if ($LASTEXITCODE -ne 0) { Write-Host "could not reach $SshHost over ssh"; exit 1 }

Write-Host "copying $LocalBinary -> ${SshHost}:$RemoteBinary"
& $scpExe -o BatchMode=yes -o ConnectTimeout=10 $LocalBinary "${SshHost}:$RemoteBinary"
if ($LASTEXITCODE -ne 0) { Write-Host "scp failed"; exit 1 }

Write-Host "verifying on $SshHost"
& $sshExe -o BatchMode=yes -o ConnectTimeout=10 $SshHost "pwsh -NoProfile -Command `"& '$RemoteBinary' version`""
Write-Host "staged."
