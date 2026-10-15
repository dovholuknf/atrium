#!/usr/bin/env pwsh
# Build the token-efficiency CSVs for one window from the inputs fetch.ps1 collected. See README.md.
param(
    [Parameter(Mandatory)][string]$Start,
    [Parameter(Mandatory)][string]$End,
    [Parameter(Mandatory)][string]$InDir,
    [Parameter(Mandatory)][string]$OutDir,
    [string]$HubUrl = 'http://127.0.0.1:7778',
    # Names the output files <kind>-<label>.csv. Defaults to overnight-<start date>.
    [string]$Label = '',
    # Optional CSV with columns title,landed: the claude/main commit each card landed as. Cards not in it say none.
    [string]$LandedFile = ''
)
$ErrorActionPreference = 'Stop'
$HubUrl = $HubUrl.TrimEnd('/')
$from = [datetime]::Parse($Start).ToUniversalTime()
$to = [datetime]::Parse($End).ToUniversalTime()
if (-not $Label) { $Label = "overnight-$($from.ToString('yyyy-MM-dd'))" }
New-Item -ItemType Directory -Force $OutDir | Out-Null

# the newest tasks-<room>-<stamp>.json per room names the rooms
$latest = @{}
foreach ($f in Get-ChildItem $InDir -Filter 'tasks-*.json' | Sort-Object Name) {
    $base = [IO.Path]::GetFileNameWithoutExtension($f.Name).Substring(6)
    $latest[($base -replace '-\d{8}-\d{4}$', '')] = $base
}
$rooms = @($latest.Keys | Sort-Object)
if (-not $rooms.Count) { throw "no tasks-*.json in $InDir" }

$usage = foreach ($r in $rooms) {
    Get-Content (Join-Path $InDir "usage-$($latest[$r]).jsonl") | Where-Object { $_.Trim() } | ForEach-Object {
        $_ | ConvertFrom-Json | Add-Member room $r -PassThru
    }
}
$usage = @($usage | Where-Object {
        $t = ([datetime]$_.started_at).ToUniversalTime(); $t -ge $from -and $t -le $to
    })

# cards, by room~id
$cards = @{}
foreach ($r in $rooms) {
    $t = Get-Content (Join-Path $InDir "tasks-$($latest[$r]).json") -Raw | ConvertFrom-Json
    foreach ($c in $t.tasks) { $cards["$r~$($c.id)"] = $c }
}
function Title($room, $id) {
    $c = $cards["$room~$id"]
    if ($c) { if ($c.display_title) { $c.display_title } else { $c.title } } else { $id }
}

# says, every room, paged
$sinceQ = $from.ToString("yyyy-MM-dd'T'HH:mm:ss'Z'")
$says = foreach ($r in $rooms) {
    $before = ''
    do {
        $u = "$HubUrl/v1/says?since=$sinceQ" + $(if ($before) { "&before=$before" } else { '' })
        try {
            $p = Invoke-RestMethod -Uri $u -Headers @{ 'X-Atrium-Room' = $r }
        } catch {
            # a room that is offline or too old for /v1/says costs its says, not the whole report
            Write-Warning "no says from $r : $($_.Exception.Message)"
            break
        }
        foreach ($s in $p.says) { $s | Add-Member at_room $r -PassThru }
        if ($p.says.Count) { $before = $p.says[-1].id }
    } while ($p.more)
}
$says = @($says | Where-Object { ([datetime]$_.sent_at).ToUniversalTime() -le $to })

# a say starts the receiver's first say-caused turn that begins within 10 minutes after it.
# Rows whose `to` has an @ are the cross-room copy of a say, so they are dropped to count each once.
$used = @{}
$sayRows = foreach ($s in ($says | Where-Object { $_.to -notlike '*@*' } | Sort-Object sent_at)) {
    $at = ([datetime]$s.sent_at).ToUniversalTime()
    $turn = $usage | Where-Object {
        $_.room -eq $s.at_room -and $_.task_id -eq $s.to_task -and $_.cause -eq 'say' -and -not $used[$_.id] -and
        ([datetime]$_.started_at).ToUniversalTime() -ge $at.AddSeconds(-5) -and
        ([datetime]$_.started_at).ToUniversalTime() -le $at.AddMinutes(10)
    } | Sort-Object started_at | Select-Object -First 1
    if ($turn) { $used[$turn.id] = $true }
    [pscustomobject]@{
        time = $s.sent_at; room = $s.at_room; from = $s.from; to = $s.to; kind = $s.kind
        when = $s.when; delivered = $s.state; channel = $s.channel; chars = $s.chars
        turn_started = [bool]$turn
        input = $turn.input; cache_write = $(if ($turn) { $turn.cache_write_5m + $turn.cache_write_1h } else { $null })
        cache_read = $turn.cache_read; output = $turn.output
    }
}
$sayRows | Export-Csv (Join-Path $OutDir "says-$Label.csv") -NoTypeInformation

# token totals per session and overall
$tok = $usage | Group-Object room, task_id | ForEach-Object {
    $g = $_.Group; $f = $g[0]
    [pscustomobject]@{
        room = $f.room; card = Title $f.room $f.task_id; turns = $g.Count
        say_turns = @($g | Where-Object cause -eq 'say').Count
        input = ($g | Measure-Object input -Sum).Sum
        cache_write = ($g | ForEach-Object { $_.cache_write_5m + $_.cache_write_1h } | Measure-Object -Sum).Sum
        cache_read = ($g | Measure-Object cache_read -Sum).Sum
        output = ($g | Measure-Object output -Sum).Sum
    }
} | Sort-Object cache_read -Descending
$all = [pscustomobject]@{
    room = 'ALL'; card = 'ALL'; turns = ($tok | Measure-Object turns -Sum).Sum; say_turns = ($tok | Measure-Object say_turns -Sum).Sum
    input = ($tok | Measure-Object input -Sum).Sum; cache_write = ($tok | Measure-Object cache_write -Sum).Sum
    cache_read = ($tok | Measure-Object cache_read -Sum).Sum; output = ($tok | Measure-Object output -Sum).Sum
}
@($tok) + $all | Export-Csv (Join-Path $OutDir "tokens-$Label.csv") -NoTypeInformation

# cards created in the window, and the claude/main commit each landed as
$landed = @{}
if ($LandedFile) { foreach ($l in Import-Csv $LandedFile) { $landed[$l.title] = $l.landed } }
$cardRows = foreach ($k in $cards.Keys) {
    $c = $cards[$k]; $room = $k.Split('~')[0]
    $made = ([datetime]$c.created_at).ToUniversalTime()
    if ($made -lt $from -or $made -gt $to) { continue }
    $g = @($usage | Where-Object { $_.room -eq $room -and $_.task_id -eq $c.id })
    $in = ($g | Measure-Object input -Sum).Sum; $cw = ($g | ForEach-Object { $_.cache_write_5m + $_.cache_write_1h } | Measure-Object -Sum).Sum
    $cr = ($g | Measure-Object cache_read -Sum).Sum; $out = ($g | Measure-Object output -Sum).Sum
    [pscustomobject]@{
        card = Title $room $c.id; room = $room; status = $c.status; launcher = $c.spawned_by
        model = (($g | Select-Object -ExpandProperty model -Unique) -join ' ')
        started = $c.created_at; ended = ($g | Sort-Object ended_at | Select-Object -Last 1).ended_at; turns = $g.Count
        input = $in; cache_write = $cw; cache_read = $cr; output = $out; total = $in + $cw + $cr + $out
        landed = $(if ($landed[$c.title]) { $landed[$c.title] } else { 'none' })
    }
}
$cardRows | Sort-Object started | Export-Csv (Join-Path $OutDir "cards-$Label.csv") -NoTypeInformation
"says $(@($sayRows).Count) (turn-starting $(@($sayRows | Where-Object turn_started).Count)), sessions $(@($tok).Count), cards $(@($cardRows).Count)"
$all | Format-List
