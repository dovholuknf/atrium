<#
.SYNOPSIS
  Split docs/backlog-2.md into one file per item, docs/backlog/<dept>/<id>.md, and leave a one-line pointer.

.DESCRIPTION
  One-off, re-runnable on a newer base. backlog-2.md was the file every branch appended to, so every merge conflicted
  on it. One file per item means two branches never write the same file.

  - An item is a heading `## <id>. <title>` or, in the first groups, `### <id>. <title>`. `<id>` is a number with an
    optional letter (`12a`) or a lettered id (`u-005`).
  - Its dept: the id's prefix (u=ui, t=terminal, r=runtime, f=fabric, m=release), else the first `Owned by @<dir>`
    line in its section, else $NumOwner below.
  - Its table row, if it has one, becomes a `Status:` line under the title, unless the section already opens with
    one.
  - Headings in the body are raised so the item's own title is `#`. Code fences are left alone.
  - A file that already exists (a director wrote the item into the new layout already) is KEPT, and the backlog-2
    text is appended under `## From backlog-2.md, at the split` only when it is not already in there.
  - Group introductions (the text under `## Paused`, `## Bugs` and so on) go into docs/backlog/README.md's history.

  Nothing is committed.
#>
param(
  [string]$Root = (Split-Path -Parent $PSScriptRoot),
  [string]$Source = 'docs/backlog-2.md',
  [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
Set-Location $Root

$Prefix = @{ 'u' = 'ui'; 't' = 'terminal'; 'r' = 'runtime'; 'f' = 'fabric'; 'm' = 'release' }
$OwnerDir = @{ 'ui' = 'ui'; 'terminal' = 'terminal'; 'runtime' = 'runtime'; 'fabric' = 'fabric'; 'merge' = 'release';
  'release' = 'release'; 'review' = 'review'; 'rnd' = 'rnd' }

# Old numeric items, by the area that owns the code each one is about. An `Owned by @x` line in the item wins.
$NumOwner = @{
  '1' = 'terminal'; '2' = 'runtime'; '3' = 'fabric'; '4' = 'ui'; '5' = 'fabric'; '6' = 'ui'; '7' = 'ui'
  '8' = 'terminal'; '9' = 'ui'; '10' = 'runtime'; '11' = 'ui'; '12' = 'runtime'; '12a' = 'runtime'; '13' = 'release'
  '14' = 'ui'; '15' = 'runtime'; '16' = 'review'; '17' = 'runtime'; '18' = 'ui'; '19' = 'ui'; '20' = 'terminal'
  '21' = 'runtime'; '22' = 'terminal'; '23' = 'runtime'; '24' = 'terminal'; '25' = 'runtime'; '26' = 'ui'
  '27' = 'runtime'; '28' = 'ui'; '29' = 'runtime'; '30' = 'review'; '31' = 'runtime'; '32' = 'runtime'
  '33' = 'terminal'; '34' = 'runtime'; '35' = 'runtime'; '36' = 'runtime'; '37' = 'runtime'; '38' = 'runtime'
  '39' = 'runtime'; '40' = 'runtime'; '41' = 'runtime'; '42' = 'ui'; '43' = 'ui'; '44' = 'ui'; '45' = 'ui'
  '46' = 'fabric'; '47' = 'runtime'; '48' = 'runtime'; '49' = 'fabric'; '50' = 'ui'; '51' = 'release'; '52' = 'fabric'
  '53' = 'terminal'; '54' = 'terminal'; '55' = 'runtime'; '56' = 'ui'; '57' = 'runtime'; '58' = 'fabric'
  '59' = 'fabric'; '60' = 'runtime'; '61' = 'terminal'; '62' = 'runtime'; '63' = 'fabric'; '64' = 'runtime'
  '65' = 'release'; '66' = 'runtime'; '67' = 'runtime'; '68' = 'fabric'; '69' = 'ui'; '70' = 'runtime'; '71' = 'ui'
  '72' = 'ui'; '73' = 'runtime'; '74' = 'terminal'; '75' = 'fabric'; '76' = 'release'; '77' = 'release'; '78' = 'ui'
  '79' = 'ui'; '80' = 'ui'; '81' = 'terminal'; '82' = 'terminal'; '83' = 'runtime'; '84' = 'runtime'; '85' = 'ui'
  '86' = 'terminal'; '87' = 'runtime'; '88' = 'terminal'; '89' = 'runtime'; '90' = 'terminal'; '91' = 'runtime'
  '92' = 'fabric'; '93' = 'terminal'; '94' = 'ui'; '95' = 'ui'; '96' = 'ui'
}

$text = [IO.File]::ReadAllText((Resolve-Path $Source))
$eol = if ($text.Contains("`r`n")) { "`r`n" } else { "`n" }
$lines = $text -split "`r?`n"
if ($lines.Count -lt 5) { throw "$Source is already split (it has $($lines.Count) lines)" }

$idRx = '(\d+[a-z]?|[a-z]-\d{3})'
$itemRx = [regex]("^(#{2,3}) $idRx\. (.+)$")
$rowRx = [regex]("^\| $idRx \| (.+) \| (.+?) \| (.+) \|\s*$")

# the table
$rows = @{}
foreach ($l in $lines) {
  $m = $rowRx.Match($l)
  if ($m.Success) { $rows[$m.Groups[1].Value] = @{ Item = $m.Groups[2].Value; Group = $m.Groups[3].Value; State = $m.Groups[4].Value } }
}

# the sections
$items = [ordered]@{}
$history = New-Object System.Collections.Generic.List[string]
$cur = $null
$groupName = $null
$inFence = $false
for ($i = 0; $i -lt $lines.Count; $i++) {
  $l = $lines[$i]
  if ($l -match '^\s*```') { $inFence = -not $inFence }
  if (-not $inFence) {
    $m = $itemRx.Match($l)
    if ($m.Success) {
      $cur = @{ Id = $m.Groups[2].Value; Level = $m.Groups[1].Value.Length; Title = $m.Groups[3].Value
        Group = $groupName; Body = New-Object System.Collections.Generic.List[string] }
      if ($items.Contains($cur.Id)) { throw "item $($cur.Id) appears twice in $Source" }
      $items[$cur.Id] = $cur
      continue
    }
    if ($l -match '^## (.+)$') {
      # a group heading ends whatever item was open
      $cur = $null
      $groupName = $Matches[1]
      $history.Add("## $groupName")
      continue
    }
  }
  if ($cur) { $cur.Body.Add($l) } elseif ($groupName) { $history.Add($l) }
}

function Raise([System.Collections.Generic.List[string]]$body, [int]$by) {
  $out = New-Object System.Collections.Generic.List[string]
  $fence = $false
  foreach ($b in $body) {
    if ($b -match '^\s*```') { $fence = -not $fence }
    if (-not $fence -and $by -gt 0 -and $b -match '^(#+) ') {
      $n = [Math]::Max(2, $Matches[1].Length - $by)
      $b = ('#' * $n) + $b.Substring($Matches[1].Length)
    }
    if ($b -match '^-{4,}\s*$') { continue }
    $out.Add($b)
  }
  while ($out.Count -and -not $out[0].Trim()) { $out.RemoveAt(0) }
  while ($out.Count -and -not $out[$out.Count - 1].Trim()) { $out.RemoveAt($out.Count - 1) }
  return , $out
}

# ids that have a table row and no section still get a file
foreach ($id in $rows.Keys) {
  if (-not $items.Contains($id)) {
    $items[$id] = @{ Id = $id; Level = 2; Title = $rows[$id].Item; Group = $null; Body = New-Object System.Collections.Generic.List[string] }
  }
}

$written = 0; $merged = 0; $kept = 0
$byDept = @{}
foreach ($id in $items.Keys) {
  $it = $items[$id]
  $body = Raise $it.Body ($it.Level - 1)
  $dept = $null
  if ($id -match '^([a-z])-') { $dept = $Prefix[$Matches[1]] }
  if (-not $dept) {
    $own = $body | Where-Object { $_ -match '(?i)\bowned by @([a-z]+)' } | Select-Object -First 1
    if ($own -and $own -match '(?i)\bowned by @([a-z]+)') { $dept = $OwnerDir[$Matches[1].ToLower()] }
  }
  if (-not $dept) { $dept = $NumOwner[$id] }
  if (-not $dept) { throw "no dept for item $id ($($it.Title)). Add it to `$NumOwner." }

  $head = New-Object System.Collections.Generic.List[string]
  $head.Add("# $id. $($it.Title)")
  $head.Add('')
  $opensWithStatus = ($body | Select-Object -First 3 | Where-Object { $_ -match '^Status:' })
  if ($rows.Contains($id) -and -not $opensWithStatus) {
    $r = $rows[$id]
    $head.Add("Status: $($r.State). Group: $($r.Group).")
    $head.Add('')
  } elseif (-not $opensWithStatus -and $it.Group) {
    $head.Add("Status: see below. Group: $($it.Group).")
    $head.Add('')
  }
  $content = (($head + $body) -join $eol) + $eol

  $path = "docs/backlog/$dept/$id.md"
  $byDept[$dept] = 1 + [int]$byDept[$dept]
  if ($DryRun) { Write-Host "$path  <- $($it.Title)"; continue }
  New-Item -ItemType Directory -Force (Split-Path -Parent $path) | Out-Null
  $existing = Get-ChildItem -Path 'docs/backlog' -Recurse -Filter "$id.md" -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($existing) {
    $have = [IO.File]::ReadAllText($existing.FullName)
    $bodyText = ($body -join $eol).Trim()
    if ($bodyText -and -not $have.Contains($bodyText)) {
      $add = $eol + '## From backlog-2.md, at the split' + $eol + $eol + $bodyText + $eol
      [IO.File]::AppendAllText($existing.FullName, $add, (New-Object Text.UTF8Encoding($false)))
      $merged++
      Write-Host "MERGED   $($existing.FullName) (kept, backlog-2 text appended)"
    } else { $kept++ }
    continue
  }
  [IO.File]::WriteAllText((Join-Path $Root $path), $content, (New-Object Text.UTF8Encoding($false)))
  $written++
}

Write-Host ""
Write-Host "items: $($items.Count). written $written, merged into an existing file $merged, already there $kept"
$byDept.Keys | Sort-Object | ForEach-Object { Write-Host ("  {0,-9} {1}" -f $_, $byDept[$_]) }
if ($DryRun) { return }

# history: the group introductions, which belong to no one item
$hist = '# What backlog-2.md said between its items' + $eol + $eol +
  'The group introductions from `docs/backlog-2.md`, kept when it was split into one file per item. Nothing here is' + $eol +
  'an item. Each item says its group on its `Status:` line.' + $eol + $eol + ($history -join $eol).Trim()
[IO.File]::WriteAllText((Join-Path $Root 'docs/backlog/HISTORY.md'), $hist + $eol, (New-Object Text.UTF8Encoding($false)))

# the pointer
$pointer = 'Split 2026-09-29: every item is its own file, `docs/backlog/<dept>/<id>.md`. See `docs/backlog/README.md`.'
[IO.File]::WriteAllText((Resolve-Path $Source), $pointer + $eol, (New-Object Text.UTF8Encoding($false)))
Write-Host "wrote the pointer in $Source and the group history in docs/backlog/HISTORY.md"
