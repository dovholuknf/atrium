# Ask a running hub whether it may be restarted now. See docs/hub-restart-gate.md.
#
# A hub-only deploy calls this before it stops the hub. The hub waits until no
# board has been used for -Idle seconds, shows every board a -Countdown second
# toast that pauses the restart when clicked, and then answers. This script
# prints that answer and exits with it:
#
#   0  restart now. The hub said go, or it is older than the gate (404), or no
#      hub is answering at all, so there is nobody to warn.
#   3  do not restart. Somebody paused it from the board, the boards never went
#      quiet within -Wait seconds, or another deploy is already asking.
#
# The gate restarts nothing itself. The caller does the restart, and only on 0.
#
#   & scripts/hub-restart-gate.ps1
#   if ($LASTEXITCODE -ne 0) { exit 0 }
param(
  [string]$Hub = 'http://127.0.0.1:7778',
  [double]$Countdown = 5,
  [double]$Idle = 10,
  [double]$Wait = 300
)
$ErrorActionPreference = 'Stop'

$body = @{ countdown = $Countdown; idle = $Idle; wait = $Wait } | ConvertTo-Json -Compress
try {
  # The hub holds the request until it has an answer, so the timeout is the
  # wait plus room for the countdown and the reply.
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

$answer = ''
try { $answer = ($res.Content | ConvertFrom-Json).answer } catch {}
switch ($answer) {
  'go' { Write-Output 'go: nobody is using a board and nobody paused'; exit 0 }
  'paused' { Write-Output 'held: the restart is paused from the board'; exit 3 }
  'busy' { Write-Output "held: a board stayed in use for $Wait seconds"; exit 3 }
  default { Write-Output "held: the hub gave no answer ($($res.Content))"; exit 3 }
}
