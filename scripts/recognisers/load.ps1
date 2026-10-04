<#
.SYNOPSIS
  Load a file of recogniser rows into atrium.

.DESCRIPTION
  Reads a JSON array of recognisers and PUTs each one. Nothing here is clever: it is a loop over
  `PUT /v1/recognisers/{id}`, written so that trying the examples is one command rather than eight
  trips through a dialog.

  A row with an id that already exists is REPLACED. Read the file before running it against a table
  you have edited by hand.

  Every example points its `cwd` at this operator's worktree layout, which is almost certainly not
  yours. Open each row afterwards and change the directory template, or pass -Root to rewrite the
  leading `D:/worktrees` and `D:/git` as it loads.

.EXAMPLE
  ./load.ps1 -Path ./github.json

.EXAMPLE
  ./load.ps1 -Path ./github.json -Root /home/me/worktrees
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string] $Path,

  # Where your worktrees live. Rewrites the examples' `D:/worktrees` and `D:/git` prefixes.
  [string] $Root,

  # The board. Matches what every other script here defaults to.
  [string] $BoardUrl = $(if ($env:ATRIUM_BOARD_URL) { $env:ATRIUM_BOARD_URL } else { 'http://localhost:7778' })
)

$ErrorActionPreference = 'Stop'

$rows = Get-Content -Raw -LiteralPath $Path | ConvertFrom-Json
if ($rows -isnot [System.Array]) { $rows = @($rows) }

foreach ($row in $rows) {
  if ($Root) {
    $row.cwd = $row.cwd -replace '^D:/worktrees', $Root.TrimEnd('/')
    $row.cwd = $row.cwd -replace '^D:/git', $Root.TrimEnd('/')
  }
  $body = $row | ConvertTo-Json -Depth 6 -Compress
  $url = "$($BoardUrl.TrimEnd('/'))/v1/recognisers/$([uri]::EscapeDataString($row.id))"
  try {
    Invoke-RestMethod -Method Put -Uri $url -ContentType 'application/json' -Body $body | Out-Null
    Write-Host "loaded $($row.id)  ->  $($row.cwd)"
  } catch {
    # A pattern that does not compile comes back as a 400 naming the position in the expression.
    # That is the whole reason it is checked on save rather than when somebody pastes a url.
    Write-Error "atrium refused $($row.id): $($_.Exception.Message)"
  }
}

Write-Host ''
Write-Host 'try one with:  atrium open https://github.com/openziti/ziti/pull/4211'
