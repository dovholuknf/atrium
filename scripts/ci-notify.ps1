# Tell the hub a CI run finished. The script decides what is sent, and the workflow only carries it.
#
#   ci-notify.ps1 EventFile [-Out FILE] [-Url URL] [-Secret S] [-Local]
#
# EventFile is the workflow_run event JSON. Anything else exits 3 and writes nothing. The body is the event as is,
# written to -Out (default build.claude/ci-notify-body.json), and that path is printed. With no -Url that is all it does.
# With -Url it POSTs the body with the secret in X-Ziti-Hook-Secret. -Local is http://127.0.0.1:7778/_hub/ci-hook.
# The secret comes from -Secret or ATRIUM_CI_HOOK_SECRET, and is never printed.
#
# Exit codes: 0 sent or written, 2 a usage problem, 3 not a workflow_run event, 1 the POST failed.
param(
    [Parameter(Position = 0)][string]$EventFile = '',
    [string]$Out = $(if ($env:CI_NOTIFY_OUT) { $env:CI_NOTIFY_OUT } else { 'build.claude/ci-notify-body.json' }),
    [string]$Url = '',
    [string]$Secret = $env:ATRIUM_CI_HOOK_SECRET,
    [switch]$Local
)
$ErrorActionPreference = 'Stop'

if ($Local) { $Url = 'http://127.0.0.1:7778/_hub/ci-hook' }
if (-not $EventFile) { [Console]::Error.WriteLine('ci-notify: name the event file'); exit 2 }
if (-not (Test-Path -LiteralPath $EventFile -PathType Leaf)) { [Console]::Error.WriteLine("ci-notify: no file $EventFile"); exit 2 }

$raw = [IO.File]::ReadAllText((Resolve-Path -LiteralPath $EventFile))
$ok = $false
try {
    $ev = $raw | ConvertFrom-Json
    $ok = ($null -ne $ev.workflow_run) -and ($ev.workflow_run -isnot [string]) -and [bool]$ev.workflow_run.conclusion
} catch { $ok = $false }
if (-not $ok) {
    [Console]::Error.WriteLine("ci-notify: $EventFile is not a finished workflow_run event, so nothing is sent")
    exit 3
}

$dir = Split-Path -Parent $Out
if ($dir) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
[IO.File]::WriteAllText([IO.Path]::GetFullPath($Out), $raw, [Text.UTF8Encoding]::new($false))
Write-Output $Out

if (-not $Url) { exit 0 }
if (-not $Secret) { [Console]::Error.WriteLine('ci-notify: a url needs a secret: -Secret or ATRIUM_CI_HOOK_SECRET'); exit 2 }

try {
    $r = Invoke-WebRequest -Uri $Url -Method Post -ContentType 'application/json' -Body $raw `
        -Headers @{ 'X-Ziti-Hook-Secret' = $Secret } -TimeoutSec 30 -UseBasicParsing
    [Console]::Error.WriteLine($r.Content)
} catch {
    [Console]::Error.WriteLine('ci-notify: the hub did not take it')
    exit 1
}
