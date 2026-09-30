# How often an idle Claude session comes back, per idle hour, from Claude Code's own transcripts.
#
# The cache keep-alive's stop rule (docs/runtime/cache-keepalive-design.md, "The stop rule") rests on this table: a refresh at
# idle hour t pays while hazard(t) x (W - R) > R. Re-run it as the data grows and compare with the design's table.
#
# Reads main-thread assistant replies (subagents excluded) from transcripts written in the last -Days days. An idle
# stretch is the gap after a reply on a context of -MinCtx or more. A stretch still open now counts as not resumed.
# Needs jq on PATH.
param([int]$Days = 21, [double]$MinGapH = 1.0, [int]$MaxH = 30, [double]$MinCtx = 50000)
$ErrorActionPreference = 'Stop'
$cut = (Get-Date).AddDays(-$Days)
$files = Get-ChildItem "$env:USERPROFILE\.claude\projects\*\*.jsonl" | Where-Object LastWriteTime -gt $cut
$filter = 'select(.type=="assistant" and (.isSidechain|not) and .message.usage != null) | ' +
    '[input_filename, .timestamp, ((.message.usage.cache_read_input_tokens // 0) + ' +
    '(.message.usage.cache_creation_input_tokens // 0) + (.message.usage.input_tokens // 0))] | @tsv'
$rows = $files | ForEach-Object { jq -r $filter $_.FullName 2>$null } |
    ConvertFrom-Csv -Delimiter "`t" -Header file, ts, ctx
"files: $($files.Count)  replies: $(@($rows).Count)"
$now = [DateTimeOffset]::UtcNow
$stretches = [System.Collections.Generic.List[object]]::new()
foreach ($g in ($rows | Group-Object file)) {
    $calls = $g.Group | ForEach-Object {
        [pscustomobject]@{ t = [DateTimeOffset]::Parse($_.ts); ctx = [double]$_.ctx }
    } | Sort-Object t
    for ($i = 1; $i -lt $calls.Count; $i++) {
        $h = ($calls[$i].t - $calls[$i - 1].t).TotalHours
        if ($h -ge $MinGapH -and $calls[$i - 1].ctx -ge $MinCtx) {
            $stretches.Add([pscustomobject]@{ h = $h; resumed = $true })
        }
    }
    $last = $calls[-1]
    $h = ($now - $last.t).TotalHours
    if ($h -ge $MinGapH -and $last.ctx -ge $MinCtx) {
        $stretches.Add([pscustomobject]@{ h = $h; resumed = $false })
    }
}
$res = @($stretches | Where-Object resumed)
"stretches over ${MinGapH}h at ${MinCtx}+ context: $($stretches.Count), resumed $($res.Count), " +
    "open $($stretches.Count - $res.Count)"
"hour  still-idle  resumed  hazard"
for ($t = [int]$MinGapH; $t -lt $MaxH; $t++) {
    $atRisk = @($stretches | Where-Object { $_.h -ge $t }).Count
    $ev = @($res | Where-Object { $_.h -ge $t -and $_.h -lt $t + 1 }).Count
    $hz = if ($atRisk) { $ev / $atRisk } else { 0 }
    "{0,4}  {1,10}  {2,7}  {3,6:P1}" -f $t, $atRisk, $ev, $hz
}
