# Ask a running hub whether it may be restarted now. See docs/fabric/hub-restart-gate.md.
#
# A hub-only deploy calls this before it stops the hub. The hub waits until no
# board has been used for -Idle seconds, shows every board a -Countdown second
# toast that pauses the restart when clicked, and then answers. This script
# prints that answer and exits with it:
#
#   0  restart now. The hub said go, or it is older than the gate (404), or no
#      hub is answering at all, so there is nobody to warn.
#   3  do not restart. The boards never went quiet within -Wait seconds, or
#      another deploy is already asking.
#   4  do not restart. The hub went away or restarted while the ask was
#      waiting, so whatever it would have answered is gone with it.
#
# A PAUSE HOLDS UNTIL SOMEBODY RESUMES IT. -Wait is how long the boards may stay
# busy. It does not run while the restart is paused, and a resume starts it
# over. The hub holds each request for at most -Hold seconds and this script
# asks again, so no one request hangs for the whole pause.
#
# The gate restarts nothing itself. The caller does the restart, and only on 0.
#
#   & scripts/hub-restart-gate.ps1
#   if ($LASTEXITCODE -ne 0) { exit 0 }
param(
  [string]$Hub = 'http://127.0.0.1:7778',
  [double]$Countdown = 5,
  [double]$Idle = 10,
  [double]$Wait = 300,
  [double]$Hold = 25
)
$ErrorActionPreference = 'Stop'

function Answered($res) {
  $answer = ''
  $why = ''
  try { $reply = $res.Content | ConvertFrom-Json; $answer = $reply.answer; $why = $reply.why } catch {}
  # The hub says which go it is: no board open, or a countdown the boards saw.
  # A hub older than that says only go.
  if (-not $why) { $why = 'the hub said go' }
  switch ($answer) {
    'go' { Write-Output "go: $why"; exit 0 }
    'paused' { Write-Output 'held: the restart is paused from the board'; exit 3 }
    'busy' { Write-Output "held: a board stayed in use for $Wait seconds"; exit 3 }
  }
}

$body = @{ countdown = $Countdown; idle = $Idle; wait = $Wait; hold = $Hold } | ConvertTo-Json -Compress
try {
  # A hub older than re-polling ignores `hold` and holds this one request for
  # its whole wait, so the first timeout still covers that.
  $res = Invoke-WebRequest -Method Post -Uri "$Hub/_hub/restart" -Body $body `
    -ContentType 'application/json' -TimeoutSec ([int]($Wait + $Countdown + 30)) -SkipHttpErrorCheck
} catch {
  Write-Output "go: no hub answering at $Hub ($($_.Exception.Message))"
  exit 0
}

switch ($res.StatusCode) {
  404 { Write-Output 'go: this hub is older than the restart gate'; exit 0 }
  409 { Write-Output 'held: another restart is already waiting for an answer'; exit 3 }
  200 { }
  default { Write-Output "held: the hub answered $($res.StatusCode): $($res.Content)"; exit 3 }
}

$paused = $false
while ($true) {
  Answered $res
  $reply = $null
  try { $reply = $res.Content | ConvertFrom-Json } catch {}
  if (-not $reply -or $reply.answer -ne 'waiting' -or -not $reply.ask) {
    Write-Output "held: the hub gave no answer ($($res.Content))"
    exit 3
  }
  if ([bool]$reply.paused -ne $paused) {
    $paused = [bool]$reply.paused
    if ($paused) { Write-Output 'waiting: the restart is paused from the board, until somebody resumes it' }
    else { Write-Output 'waiting: resumed from the board, waiting for the boards to go quiet' }
  }
  $poll = @{ ask = $reply.ask; hold = $Hold } | ConvertTo-Json -Compress
  try {
    $res = Invoke-WebRequest -Method Post -Uri "$Hub/_hub/restart" -Body $poll `
      -ContentType 'application/json' -TimeoutSec ([int]($Hold + $Countdown + 30)) -SkipHttpErrorCheck
  } catch {
    # THE HUB DIED WHILE THE RESTART WAS HELD. Not a go: nobody said go, and a
    # hub that is back by itself is a hub the deploy knows nothing about.
    Write-Output "held: the hub went away while the restart was waiting ($($_.Exception.Message))"
    exit 4
  }
  if ($res.StatusCode -eq 410 -or $res.StatusCode -eq 404) {
    Write-Output 'held: the hub restarted while the restart was waiting, and forgot the ask'
    exit 4
  }
  if ($res.StatusCode -ne 200) {
    Write-Output "held: the hub answered $($res.StatusCode): $($res.Content)"
    exit 4
  }
}
