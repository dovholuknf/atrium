# Posts one event to the hub's POST /_hub/events from the machine the hub runs on. See docs/fabric/land-webhook-design.md, 6.
#
#   pwsh -File scripts/atrium-event.ps1 -Kind land -Repo dovholuknf/atrium -Branch main -Outcome pushed -From 3f1c0de -To 9a0b1c2 -Commits 14
#
# Prints one line: `event: 2 subscribers` or `event: duplicate`. On any failure it prints one warning and exits 0, because a
# land must never fail since the hub was down.
param(
    [Parameter(Mandatory = $true)][string]$Kind,
    [string]$Repo = '',
    [string]$Branch = '',
    [string]$Outcome = '',
    [string]$From = '',
    [string]$To = '',
    [int]$Commits = 0,
    [string]$Reason = '',
    [string]$HubUrl = ''
)

if (-not $HubUrl) { $HubUrl = $env:ATRIUM_HUB_URL }
if (-not $HubUrl) { $HubUrl = 'http://127.0.0.1:7778' }

$body = [ordered]@{
    kind    = $Kind
    repo    = $Repo
    branch  = $Branch
    outcome = $Outcome
    from    = $From
    to      = $To
    commits = $Commits
    reason  = $Reason
} | ConvertTo-Json -Compress

try {
    $res = Invoke-WebRequest -Uri ($HubUrl.TrimEnd('/') + '/_hub/events') -Method Post -Body $body `
        -ContentType 'application/json' -TimeoutSec 5 -UseBasicParsing
    $answer = $res.Content | ConvertFrom-Json
    if ($answer.duplicate) {
        Write-Output 'event: duplicate'
    } else {
        Write-Output ("event: {0} subscribers" -f $answer.subscribers)
    }
} catch {
    $why = $_.Exception.Message
    if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $why = $_.ErrorDetails.Message }
    Write-Warning ("atrium-event: the hub did not take the event: " + ($why -replace '\s+', ' '))
}
exit 0
