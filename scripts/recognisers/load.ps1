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

  The hub owns the recogniser table and seeds it from these files, so loading them is only for rows of your own.
  Against a hub a PUT writes the hub's table and -Room is not needed. Against a single room with no hub it writes
  that room's.

.EXAMPLE
  ./load.ps1 -Path ./github.json

.EXAMPLE
  ./load.ps1 -Path ./github.json -Room sg4

.EXAMPLE
  ./load.ps1 -Path ./github.json -Root /home/me/worktrees
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string] $Path,

  # Where your worktrees live. Rewrites the examples' `D:/worktrees` and `D:/git` prefixes.
  [string] $Root,

  # The room to write to, sent as X-Atrium-Room. A hub writes its own table whatever this says.
  [string] $Room = $env:ATRIUM_ROOM,

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
  $headers = @{}
  if ($Room) { $headers['X-Atrium-Room'] = $Room }
  try {
    Invoke-RestMethod -Method Put -Uri $url -ContentType 'application/json' -Headers $headers -Body $body | Out-Null
    Write-Host "loaded $($row.id)  ->  $($row.cwd)"
  } catch {
    # A 409 is the hub asking for a room, and a 400 is a pattern that does not compile, with the position in
    # the expression. Both carry their own sentence in the body, which is better than the generic status text.
    $status = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
    $detail = $null
    if ($_.ErrorDetails -and $_.ErrorDetails.Message) {
      try { $detail = $_.ErrorDetails.Message | ConvertFrom-Json } catch { $detail = $null }
    }
    if ($detail -and $detail.error) {
      $msg = "$($detail.error)"
      if ($status -eq 409 -and $detail.rooms) {
        $msg += "`
rooms: $($detail.rooms -join ', ')`
retry with -Room <name> or set ATRIUM_ROOM"
      }
      Write-Error "atrium refused $($row.id) ($status): $msg"
    } else {
      Write-Error "atrium refused $($row.id): $($_.Exception.Message)"
    }
  }
}

Write-Host ''
Write-Host 'try one with:  atrium open https://github.com/openziti/ziti/pull/4211'
