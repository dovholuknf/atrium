<#
.SYNOPSIS
  Keeps your copy of push-to-github.ps1 current, then runs it. Copy this one file once and run it.

.DESCRIPTION
  Compares <Repo>/scripts/ci-loop/push-to-github.ps1 with -Dest. When they differ it shows the diff and asks before
  copying. When they match it copies nothing. Then it runs -Dest with -Repo and any further arguments.

.EXAMPLE
  pwsh -File start-ci-loop.ps1 -Repo <atrium checkout> -Rebuild
.PARAMETER Repo
  The atrium checkout. Default: the checkout this script sits in.
.PARAMETER Dest
  Your copy of the loop script. Default: ~/.atrium/push-to-github.ps1.
#>
param(
    [string]$Repo,
    [string]$Dest = (Join-Path $HOME '.atrium/push-to-github.ps1'),
    [Parameter(ValueFromRemainingArguments)][string[]]$PassThrough
)
$ErrorActionPreference = 'Stop'
if (-not $Repo) {
    $Repo = & git -C $PSScriptRoot rev-parse --show-toplevel 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $Repo) { throw 'no repo: pass -Repo <atrium checkout>' }
}
$Repo = [IO.Path]::GetFullPath($Repo)
$source = Join-Path $Repo 'scripts/ci-loop/push-to-github.ps1'
if (-not (Test-Path -LiteralPath $source)) { throw "no loop script at $source" }

$same = (Test-Path -LiteralPath $Dest) -and
    (Get-FileHash -LiteralPath $Dest).Hash -eq (Get-FileHash -LiteralPath $source).Hash
if ($same) {
    'loop script unchanged'
} else {
    if (Test-Path -LiteralPath $Dest) {
        & git diff --no-index --color $Dest $source
    } else {
        "no copy at $Dest yet"
    }
    if ((Read-Host 'copy the new loop script? [y/N]') -eq 'y') {
        New-Item -ItemType Directory -Force (Split-Path -Parent $Dest) | Out-Null
        Copy-Item -LiteralPath $source -Destination $Dest -Force
        'copied'
    } elseif (-not (Test-Path -LiteralPath $Dest)) {
        throw 'no copy to run'
    } else {
        'kept the old copy'
    }
}
& $Dest -Repo $Repo @PassThrough
