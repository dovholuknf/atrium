#!/usr/bin/env pwsh
# One command: fetch the inputs for a window, then build the CSVs.
param(
    [Parameter(Mandatory)][string]$Start,
    [Parameter(Mandatory)][string]$End,
    [Parameter(Mandatory)][string]$InDir,
    [Parameter(Mandatory)][string]$OutDir,
    [string]$HubUrl = 'http://127.0.0.1:7778',
    [string]$Label = '',
    [string]$LandedFile = ''
)
$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'fetch.ps1') -Start $Start -OutDir $InDir -HubUrl $HubUrl
& (Join-Path $PSScriptRoot 'build.ps1') -Start $Start -End $End -InDir $InDir -OutDir $OutDir -HubUrl $HubUrl `
    -Label $Label -LandedFile $LandedFile
