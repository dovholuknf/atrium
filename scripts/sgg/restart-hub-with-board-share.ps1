# Restart the HUB ONLY, detached, so it serves the board over a zrok share.
#
# Goal A activation. The hub holds nothing and is safe to restart. The ROOM
# (claude-sg4) holds the store, terminals and agents and is NOT touched here.
# The board blips for a few seconds while the hub comes back, and the room
# reconnects on its own.
#
# DETACHED ON PURPOSE. Start-Process forks the new hub so it does not die with
# the shell that ran this script. Run this from a shell atrium does not
# supervise.
#
# AFTER THE CUTOVER ONLY (docs/one-atrium-cutover.md). The hub is
# `atrium.exe run --no-room`, and atrium.exe is also the file every hook runs,
# so the process is matched by path plus subcommand and the new binary goes in
# by two renames, never a copy over the live name. Before the cutover this
# refuses and names the old line.
#
# Blocked today: the zrok account returns 500 on any share create. When that is
# fixed (free reserved-name capacity, or the hosted instance recovers), this
# brings the share up. If the share still cannot be created the hub logs it and
# serves loopback anyway, so running this is never worse than the plain hub.
#
#     pwsh -File scripts\sgg\restart-hub-with-board-share.ps1 -ShareMode private

[CmdletBinding()]
param(
  [ValidateSet('private', 'public')]
  [string]$ShareMode = 'private',
  # The freshly built binary. Swapped in over the canonical one so a later
  # plain restart also has the board-share flag available.
  [string]$NewBinary = "$PSScriptRoot\..\..\build.claude\atrium.exe",
  [string]$Bin = 'C:\Users\claude\.atrium\bin\atrium.exe',
  [string]$HubDir = 'C:\Users\claude\.atrium2\hub',
  [string]$HubAddr = '127.0.0.1:7778',
  [string]$Link = '127.0.0.1:7779',
  [int]$TimeoutSeconds = 30
)
$ErrorActionPreference = 'Stop'
$board = "http://$HubAddr"

$old = Get-CimInstance Win32_Process -Filter "Name='atrium2.exe'" |
  Where-Object { $_.CommandLine -match ' hub ' } | Select-Object -First 1
if ($old) {
  Write-Host "the hub is still atrium2.exe (pid $($old.ProcessId)), so this machine is not cut over yet."
  Write-Host "see docs/one-atrium-cutover.md. until then the hub line is:"
  Write-Host ("  atrium2.exe hub --addr $HubAddr --link $Link --dir $HubDir " +
    "--board-transport zrok --board-share $ShareMode")
  exit 1
}

if ($ShareMode -eq 'public') {
  Write-Host "PUBLIC share: the board will have NO login in front of it. Anyone with the"
  Write-Host "URL can read every command and answer permission prompts. Ctrl-C now to stop."
  Start-Sleep -Seconds 4
}

# Stop the running hub (and ONLY the hub). Matched by path and the ' run '
# argument: every hook in flight is also atrium.exe, and the room is ' room '.
$hub = Get-CimInstance Win32_Process -Filter "Name='atrium.exe'" |
  Where-Object { $_.ExecutablePath -eq $Bin -and $_.CommandLine -match ' run ' } | Select-Object -First 1
if ($hub) {
  Write-Host "stopping hub pid $($hub.ProcessId)"
  Stop-Process -Id $hub.ProcessId -Force
  Start-Sleep -Seconds 2
} else {
  Write-Host "no hub running"
}

# Stage the new binary beside the live one, then swap with two renames. The room
# and every hook keep running the image they already loaded.
if (Test-Path $NewBinary) {
  $next = Join-Path (Split-Path $Bin) 'atrium.next.exe'
  $aside = Join-Path (Split-Path $Bin) "atrium.old-$(Get-Date -Format yyyyMMddHHmmss).exe"
  Copy-Item $NewBinary $next -Force
  Rename-Item $Bin $aside
  Rename-Item $next (Split-Path $Bin -Leaf)
  Write-Host "deployed $NewBinary -> $Bin (previous: $aside)"
} else {
  Write-Host "no new binary at $NewBinary, using existing $Bin"
}

Write-Host "starting hub on $board (link $Link), board share: $ShareMode"
Start-Process -FilePath $Bin `
  -ArgumentList 'run', '--no-room', '--addr', $HubAddr, '--link', $Link, '--atrium-dir', $HubDir, `
  '--board-transport', 'zrok', '--board-share', $ShareMode `
  -WindowStyle Hidden `
  -RedirectStandardOutput 'C:\Users\claude\.atrium2\hub.out' `
  -RedirectStandardError  'C:\Users\claude\.atrium2\hub.err'

$dl = (Get-Date).AddSeconds($TimeoutSeconds); $ok = $false
while ((Get-Date) -lt $dl) {
  Start-Sleep -Milliseconds 500
  try { $null = Invoke-WebRequest "$board/_hub/health" -UseBasicParsing -TimeoutSec 2; $ok = $true; break } catch {}
}
if (-not $ok) { Write-Host "hub did not answer at $board after $TimeoutSeconds s. see hub.err"; exit 1 }
Write-Host "hub up. the board share address is in hub.err:"
Get-Content 'C:\Users\claude\.atrium2\hub.err' | Select-String 'zrok share|zrok access|serving loopback only'
