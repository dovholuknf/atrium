#!/usr/bin/env pwsh
# Collect the inputs build.ps1 reads: each online room's usage rows and card list, through the hub.
# These are the same two room endpoints the atrium_export MCP tool calls (internal/link/files_mcp.go, exportHandler),
# reached through the hub proxy with X-Atrium-Room. Nothing is put in the hub's file drop.
param(
    [Parameter(Mandatory)][string]$Start,
    [Parameter(Mandatory)][string]$OutDir,
    [string]$HubUrl = 'http://127.0.0.1:7778',
    [string[]]$Rooms = @()
)
$ErrorActionPreference = 'Stop'
$HubUrl = $HubUrl.TrimEnd('/')
$since = [datetime]::Parse($Start).ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss'Z'")
New-Item -ItemType Directory -Force $OutDir | Out-Null
if (-not $Rooms.Count) {
    $Rooms = @((Invoke-RestMethod "$HubUrl/_hub/rooms").rooms | Where-Object { $null -eq $_.online -or $_.online } | ForEach-Object name)
}
$stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmm')
foreach ($r in $Rooms) {
    $h = @{ 'X-Atrium-Room' = $r }
    try {
        Invoke-WebRequest "$HubUrl/v1/tasks" -Headers $h -OutFile (Join-Path $OutDir "tasks-$r-$stamp.json")
        Invoke-WebRequest "$HubUrl/v1/usage/export?since=$([uri]::EscapeDataString($since))" -Headers $h `
            -OutFile (Join-Path $OutDir "usage-$r-$stamp.jsonl")
        "fetched $r"
    } catch {
        Write-Warning "skipped $r : $($_.Exception.Message)"
    }
}
