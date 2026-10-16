# Measures idle claude CPU and pty output per card. Read docs/changes/bug-idle-minion-spins-a-core.md.
# Usage: pwsh scripts/idle-spin-measure.ps1 -Cards arm1=<cardid>:<pid>,arm2=<cardid>:<pid> [-Minutes 10] [-Room http://127.0.0.1:7781]
# Each card is `label=cardid:pid` (the pid of that card's claude). Run it with every arm idle at once.
# Output: one table and a CSV in the cwd. cpu_avg >= 50 is hot. ring_growth is bytes the card's pty ring gained
# while idle, ring_changes how many samples saw different bytes. Either one non-zero means the terminal is being
# fed or repainted. Hot cpu with a steady ring means claude spins with no terminal traffic, which clears atrium.
# The ring offset is not exposed on any route, so the raw ring (collapse=0) is read and hashed instead. When the ring
# is full its length stops growing, so ring_changes is the signal then.
param(
  [Parameter(Mandatory)][string[]]$Cards,
  [int]$Minutes = 10,
  [int]$EverySec = 30,
  [string]$Room = 'http://127.0.0.1:7781',
  [string]$Out = "idle-spin-$(Get-Date -Format yyyyMMdd-HHmmss).csv"
)
$ErrorActionPreference = 'Stop'
$arms = foreach ($c in $Cards) {
  $label, $rest = $c -split '=', 2
  $id, $procId = $rest -split ':', 2
  [pscustomobject]@{ Label = $label; Id = $id; ProcId = [int]$procId; Cpu0 = 0.0; T0 = $null }
}
function RingOf($id) {
  try {
    $r = Invoke-WebRequest -UseBasicParsing "$Room/v1/tasks/$id/scrollback/raw?collapse=0" -TimeoutSec 10
    $b = $r.RawContentStream.ToArray()
    $h = [BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash($b)).Replace('-', '').Substring(0, 12)
    return @{ Len = $b.Length; Hash = $h }
  } catch { return @{ Len = -1; Hash = 'err' } }
}
$rows = @()
$end = (Get-Date).AddMinutes($Minutes)
while ((Get-Date) -lt $end) {
  foreach ($a in $arms) {
    $p = Get-Process -Id $a.ProcId -ErrorAction SilentlyContinue
    if (-not $p) { continue }
    $now = Get-Date
    $cpu = $p.TotalProcessorTime.TotalSeconds
    $pct = $null
    if ($a.T0) { $pct = [math]::Round(100 * ($cpu - $a.Cpu0) / ($now - $a.T0).TotalSeconds, 1) }
    $a.Cpu0 = $cpu
    $a.T0 = $now
    $ring = RingOf $a.Id
    $rows += [pscustomobject]@{ t = $now.ToString('s'); arm = $a.Label; card = $a.Id; pid = $a.ProcId; cpu_pct = $pct; ring_bytes = $ring.Len; ring_hash = $ring.Hash }
  }
  Start-Sleep -Seconds $EverySec
}
$rows | Export-Csv $Out -NoTypeInformation
$rows | Group-Object arm | ForEach-Object {
  $g = $_.Group
  $cpu = $g | Where-Object { $null -ne $_.cpu_pct } | Measure-Object cpu_pct -Average -Maximum
  [pscustomobject]@{
    arm          = $_.Name
    cpu_avg      = [math]::Round($cpu.Average, 1)
    cpu_max      = $cpu.Maximum
    ring_growth  = $g[-1].ring_bytes - $g[0].ring_bytes
    ring_changes = ($g.ring_hash | Select-Object -Unique).Count - 1
  }
} | Format-Table -AutoSize
"csv: $Out"
