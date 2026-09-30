<#
.SYNOPSIS
  Print the backlog: one line per item under docs/backlog/<dept>/<id>.md, with its title and its Status line.

.DESCRIPTION
  Printed and never written, because an index file would be a shared file that every branch appends to, which is
  what splitting the backlog removed. See docs/backlog/README.md.

.PARAMETER Dept
  One folder only: ui, terminal, runtime, fabric, release, review or rnd.

.PARAMETER Open
  Leave out items whose Status line says DONE.
#>
param(
  [string]$Root = (Split-Path -Parent $PSScriptRoot),
  [string]$Dept = '',
  [switch]$Open
)
$ErrorActionPreference = 'Stop'
$base = Join-Path $Root 'docs/backlog'
$dirs = Get-ChildItem -Path $base -Directory | Where-Object { -not $Dept -or $_.Name -eq $Dept }
$rows = foreach ($d in $dirs) {
  foreach ($f in (Get-ChildItem -Path $d.FullName -Filter '*.md')) {
    $lines = Get-Content -LiteralPath $f.FullName -TotalCount 12 -Encoding utf8
    $title = ($lines | Where-Object { $_ -match '^# ' } | Select-Object -First 1) -replace '^# [^.]+\.\s*', ''
    $status = ($lines | Where-Object { $_ -match '^Status:' } | Select-Object -First 1) -replace '^Status:\s*', ''
    if ($Open -and $status -match '\bDONE\b') { continue }
    $id = $f.BaseName
    # numbers sort as numbers, lettered ids after them
    $key = if ($id -match '^(\d+)([a-z]?)$') { '0{0:D5}{1}' -f [int]$Matches[1], $Matches[2] } else { "1$id" }
    [pscustomobject]@{ Key = $key; Id = $id; Dept = $d.Name; Title = $title; Status = $status }
  }
}
$rows | Sort-Object Dept, Key | ForEach-Object {
  $s = if ($_.Status.Length -gt 70) { $_.Status.Substring(0, 67) + '...' } else { $_.Status }
  "{0,-6} {1,-9} {2}`n                 {3}" -f $_.Id, $_.Dept, $_.Title, $s
}
