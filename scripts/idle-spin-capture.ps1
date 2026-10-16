# Capture a hot claude BEFORE killing it. Usage: pwsh scripts/idle-spin-capture.ps1 -ProcId <pid> [-Dir .]
# Writes hot-<pid>.txt: claude version, command line, five one-second CPU samples, the busiest threads. Takes three
# dumps five seconds apart when procdump is on PATH (procdump -s 5 -n 3).
param([Parameter(Mandatory)][int]$ProcId, [string]$Dir = '.')
$ErrorActionPreference = 'Continue'
New-Item -ItemType Directory -Force $Dir | Out-Null
$f = Join-Path $Dir "hot-$ProcId.txt"
$p = Get-Process -Id $ProcId
"claude version: $(& claude --version 2>&1)" | Tee-Object $f
"image: $($p.Path) started: $($p.StartTime) ws_mb: $([int]($p.WS/1MB)) threads: $($p.Threads.Count)" | Tee-Object $f -Append
(Get-CimInstance Win32_Process -Filter "ProcessId=$ProcId").CommandLine | Tee-Object $f -Append
$a = $p.TotalProcessorTime.TotalSeconds
1..5 | ForEach-Object {
  Start-Sleep 1
  $p.Refresh()
  $b = $p.TotalProcessorTime.TotalSeconds
  "cpu 1s: $([math]::Round(100 * ($b - $a), 0))%" | Tee-Object $f -Append
  $a = $b
}
$p.Threads | Sort-Object TotalProcessorTime -Descending | Select-Object -First 5 Id, ThreadState, @{n = 'cpu_s'; e = { $_.TotalProcessorTime.TotalSeconds } } |
  Format-Table | Out-String | Tee-Object $f -Append
if (Get-Command procdump -ErrorAction SilentlyContinue) {
  procdump -accepteula -s 5 -n 3 -ma $ProcId $Dir
} else {
  'procdump is not on PATH (Sysinternals). The thread table above is the evidence without it.' | Tee-Object $f -Append
}
"saved: $f"
