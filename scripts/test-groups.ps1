# Runs the integration suite in groups instead of one 11 minute wall of output.
#   scripts/test-groups.ps1 -List                 every group, its purpose, packages, -run pattern, last time
#   scripts/test-groups.ps1 -Group link-git       run one group (several: -Group a,b)
#   scripts/test-groups.ps1 -All                  run every group, keep going after a failure, print a table
#                                                 groups of different packages run in parallel; add -Serial for one at a time
#   scripts/test-groups.ps1 -Failed               re-run only the tests that failed last time
#   scripts/test-groups.ps1 -Check                fail if a test is in no group or in two
# PowerShell 7. Run it from anywhere inside the repository. Full output of each group goes to
# build.claude/test-groups/<group>.txt, the state of the last run to build.claude/test-groups/last.json.
# A group is a list of packages. A package that is too slow to run in one go is split by test FILE: the group
# lists file patterns, and the script turns the tests defined in those files into the -run regex. A group with
# Rest = $true takes the files that no other group of that package names.
[CmdletBinding()]
param(
    [switch] $List,
    [string[]] $Group,
    [switch] $All,
    [switch] $Failed,
    [switch] $Check,
    [switch] $Serial,
    [string] $ResultFile
)

$ErrorActionPreference = 'Stop'
$Root = (git -C $PSScriptRoot rev-parse --show-toplevel).Trim()
Set-Location $Root
$Module = 'github.com/dovholuknf/atrium'
$OutDir = Join-Path $Root 'build.claude/test-groups'
$StateFile = Join-Path $OutDir 'last.json'
$MaxRunChars = 30000   # Windows refuses a command line over 32767 characters

function G($Name, $Purpose, $Packages, $Seconds, $Files = $null, [switch] $Rest) {
    [pscustomobject]@{ Name = $Name; Purpose = $Purpose; Packages = @($Packages); Files = @($Files); Rest = [bool]$Rest; Typical = $Seconds }
}

# Typical is the wall time in seconds measured on a Windows dev machine, a hint for -List until a run replaces it.
$Groups = @(
    G 'link-git-url'       'link: git over the room URL, pass-through of pushes' './internal/link' 73 @('git_url_*', 'git_pass_*')
    G 'link-git'           'link: git receive, hub and store plumbing, end to end git' './internal/link' 55 @('git_receive_*', 'git_integration_*', 'git_store_*', 'git_e2e_*', 'git_hub_*', 'git_push_tool_*')
    G 'link-changerequest' 'link: change requests and deploy-ready gate' './internal/link' 100 @('changerequest_*', 'deployready_*')
    G 'link-forge'         'link: forge routes and CI, announce, upgrade, PR claim/move/prune, rooms events' './internal/link' 21 @('forgeroute_*', 'forgeci_*', 'announce_*', 'upgrade_*', 'roomsevent_*', 'prclaim_*', 'prmove_*', 'prune_*', 'prworktreeroute_*', 'pinorder_*')
    G 'link-messaging'     'link: relay, everywhere, notify, growl, push, fanout' './internal/link' 14 @('relay_*', 'everywhere_*', 'everywhere_relay_*', 'notify_*', 'notify*_*', 'notifyapi_*', 'growl_*', 'growl_ladder_*', 'push_*', 'pushapi_*', 'webpush_*', 'fanout_*', 'says_*', 'events_*', 'hubask_*')
    G 'link-core'          'link: everything else in link (restart gate, routes, MCP, docs, deps, readers)' './internal/link' 82 -Rest

    G 'daemon-cull'        'daemon: timed culling, reclaim, reaper, worktree cleanup' './internal/daemon' 23 @('cull_*', 'reclaim_*', 'reaper_*', 'orphans_*', 'prune_*', 'archiveworkers_*', 'worktreegone_*', 'throwaway_*', 'mergedroutes_*')
    G 'daemon-mergedcull'  'daemon: cull of merged cards, merge proof, close stash, changes' './internal/daemon' 56 @('mergedcull_*', 'mergeproof_*', 'closestash_*', 'changes_*')
    G 'daemon-messaging'   'daemon: say/ask/relay/a2a, replies, FYI, owed, peers, holds, carry-over' './internal/daemon' 19 @('a2a_*', 'relay_*', 'saylog_*', 'saywhen_*', 'says_*', 'tasksays_*', 'replies_*', 'reportto_*', 'report_*', 'fyi_*', 'owed_*', 'askpeer_*', 'message_from_*', 'notification_*', 'queued_say_*', 'turns_*', 'handoff_*', 'decideby_*', 'escalate_*', 'echo_*', 'roomhold_*', 'holdnotices_*', 'held_*', 'heldfor_*', 'carry_*', 'carryover_*', 'dispatch_*', 'peers_*', 'peertype_*', 'peergate_*', 'interview_*', 'lastreply_*', 'seen_*', 'fleet_*')
    G 'daemon-screen'      'daemon: terminal screen, attach, cursor, width, paste, typing, replay' './internal/daemon' 10 @('screen_*', 'attach_*', 'cursor_*', 'width_*', 'flatten_*', 'replay_*', 'resize_*', 'pastewrap_*', 'message_paste_*', 'peer_paste_*', 'bracketed_*', 'escclear_*', 'escstate_*', 'typed_*', 'typedline_*', 'typing_*', 'height_*', 'spinner_*', 'viewport_*', 'idleframe_*', 'looksidle_*', 'settling_*', 'conpty_*', 'terminfo_*', 'banner_*', 'dialogguard_*', 'ring_memory_*', 'outputat_*', 'clearmark_*', 'erase*')
    G 'daemon-lifecycle'   'daemon: keepalive, restart, resume, reopen, shutdown, park, shelve, usage, telemetry' './internal/daemon' 81 @('keepalive_*', 'hostterm_*', 'hostmark_*', 'restart_*', 'restartwrap_*', 'restartwake_*', 'reopen_*', 'resume_*', 'resumeclaim_*', 'resumebusy_*', 'resume_placeholder_*', 'session_*', 'shutdown_*', 'unexpectedexit_*', 'run_exit_*', 'idletick_*', 'idlepark_*', 'finish_*', 'exitonreport_*', 'endonce_*', 'stopflag_*', 'runnerupdate_*', 'supervisor_*', 'daemon_*', 'health_*', 'auto_*', 'autocompact_*', 'park_*', 'shelve_*', 'unshelve_*', 'usage*', 'telemetry_*', 'turncost*')
    G 'daemon-launch'      'daemon: launch, overlay, new-context, PR runner, models, lean agents' './internal/daemon' 60 @('launch_*', 'launchstuck_*', 'overlay_*', 'newcontext_*', 'prepare_*', 'prrunner_*', 'model*', 'preflight_*', 'lean_*', 'lean_agents_*', 'lean_gateway_*', 'repeat_launch_*', 'whichdb_*')
    G 'daemon-core'        'daemon: everything else in daemon (auth, activity, export, sources, overlay-less odds and ends)' './internal/daemon' 75 -Rest

    G 'gitsync-receive'    'gitsync: receive-pack handling' './internal/gitsync' 145 @('receive_*')
    G 'gitsync-store'      'gitsync: store, forge store, pushed refs, commit lookup' './internal/gitsync' 145 @('store*', 'pushed_*', 'lookup_*')
    G 'gitsync-hub'        'gitsync: hub, hub push, remote hub, rooms, served, scm' './internal/gitsync' 117 -Rest

    G 'api'                'api: HTTP API handlers (open, close, worktrees, files, providers, themes)' './internal/api' 81 @('*')
    G 'cli'                'cli: command line' './internal/cli' 37 @('*')
    G 'store'              'store: sqlite store' './internal/store' 52 @('*')
    G 'hubstore'           'hubstore: hub database' './internal/hubstore' 15 @('*')
    G 'deployready'        'deployready: the deploy-ready checks' './internal/deployready' 40 @('*')
    G 'ptyhost'            'ptyhost: pty and ConPTY host' './internal/ptyhost' 16 @('*')
    G 'guard'              'guard: git guard' './internal/guard' 10 @('*')
    G 'misc'               'small packages: cardproc claudeconf edge forge linkfetch nowindow prreview/render requirements resources roomspec roomstats runnersetup safepath testdiag' @(
        './internal/cardproc', './internal/claudeconf', './internal/edge', './internal/forge', './internal/linkfetch', './internal/base/nowindow',
        './internal/prreview/render', './internal/requirements', './internal/resources', './internal/roomspec', './internal/roomstats',
        './internal/runnersetup', './internal/safepath', './internal/testdiag') 11 @('*')
)

# ---- helpers ---------------------------------------------------------------------------------------------

function Get-TestFiles([string] $Pkg) {
    # test name -> file name, read from the sources: `go test -list` does not say which file a test is in.
    $map = @{}
    foreach ($f in Get-ChildItem (Join-Path $Root $Pkg) -Filter '*_test.go' -File) {
        foreach ($line in [IO.File]::ReadLines($f.FullName)) {
            if ($line -match '^func (Test\w*)\(') { $map[$Matches[1]] = $f.Name }
        }
    }
    $map
}

function Get-FileOwner([string] $Pkg, [string] $File) {
    # Groups that claim this file of this package: explicit patterns first, the Rest group only when nobody else does.
    $explicit = @($Groups | Where-Object { $_.Packages -contains $Pkg -and $_.Files.Count -and ($_.Files | Where-Object { $File -like $_ }) })
    if ($explicit.Count) { return $explicit }
    @($Groups | Where-Object { $_.Packages -contains $Pkg -and $_.Rest })
}

function Get-GroupTests($g) {
    # The names this group runs, or $null for "every test of its packages".
    if ($g.Files -contains '*') { return $null }
    $names = foreach ($pkg in $g.Packages) {
        $map = Get-TestFiles $pkg
        foreach ($n in ($map.Keys | Sort-Object)) {
            if (@(Get-FileOwner $pkg $map[$n] | Where-Object Name -eq $g.Name).Count) { $n }
        }
    }
    @($names)
}

function Get-RunRegex($names) {
    if ($null -eq $names) { return $null }
    if ($names.Count -eq 0) { return '^$' }
    '^(' + ($names -join '|') + ')$'
}

function Format-Seconds($s) { if ($null -eq $s) { '-' } else { '{0}m{1:00}s' -f [int][math]::Floor($s / 60), [int]($s % 60) } }

function Read-State {
    if (Test-Path $StateFile) { Get-Content $StateFile -Raw | ConvertFrom-Json -AsHashtable } else { @{} }
}

function Save-State($state) {
    New-Item -ItemType Directory -Force $OutDir | Out-Null
    $state | ConvertTo-Json -Depth 6 | Set-Content $StateFile
}

function Get-Failures([string[]] $lines) {
    # Each failing test: name, file:line, first error line. A panic aborts the package, so it is reported as one too.
    $out = @()
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match '^\s*--- FAIL: (\S+)') {
            $name = $Matches[1]
            $where = ''; $msg = ''
            for ($j = $i + 1; $j -lt $lines.Count -and $lines[$j] -match '^\s+\S' -and $lines[$j] -notmatch '^\s*--- '; $j++) {
                if ($lines[$j] -match '^\s+(\S+_test\.go:\d+):\s*(.*)$') { $where = $Matches[1]; $msg = $Matches[2]; break }
            }
            $out += [pscustomobject]@{ Name = $name; Where = $where; Message = $msg }
        }
        elseif ($lines[$i] -match '^panic: (.*)') {
            $msg = $Matches[1]; $name = '(panic)'; $where = ''
            for ($j = $i + 1; $j -lt [math]::Min($lines.Count, $i + 80); $j++) {
                if (-not $where -and $lines[$j] -match '^\s+(\S+_test\.go:\d+)') { $where = $Matches[1] }
                if ($name -eq '(panic)' -and $lines[$j] -match '\.(Test\w+)\(') { $name = $Matches[1] }
                if ($where -and $name -ne '(panic)') { break }
            }
            $out += [pscustomobject]@{ Name = $name; Where = $where; Message = "panic: $msg" }
        }
        elseif ($lines[$i] -match '^FAIL\s+(\S+)\s+\[(build failed|setup failed)\]') {
            $out += [pscustomobject]@{ Name = '(build)'; Where = $Matches[1]; Message = $Matches[2] }
        }
    }
    $out
}

function Invoke-Group($g, [string[]] $OnlyNames) {
    # Runs one group, prints the command and a short result, saves the full output. Returns the result record.
    New-Item -ItemType Directory -Force $OutDir | Out-Null
    $names = if ($OnlyNames) { $OnlyNames } else { Get-GroupTests $g }
    $re = Get-RunRegex $names
    $goArgs = @('test', '-count=1', '-tags', 'integration', '-timeout', '15m')
    if ($re) {
        if ($re.Length -gt $MaxRunChars) { throw "group $($g.Name): -run pattern is $($re.Length) characters, over the Windows command line limit. Split the group." }
        $goArgs += @('-run', $re)
    }
    $goArgs += $g.Packages
    $shown = ($goArgs | ForEach-Object { if ($_ -match '[\s|()^$]') { "'$_'" } else { $_ } }) -join ' '
    if ($shown.Length -gt 600) { $shown = $shown.Substring(0, 600) + " ...(-run pattern continues, see the Command line in $($g.Name).txt)" }
    Write-Host ""
    Write-Host "=== $($g.Name): $($g.Purpose)" -ForegroundColor Cyan
    Write-Host "go $shown"

    $outFile = Join-Path $OutDir "$($g.Name).txt"
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $saved = @{ ATRIUM_LOCATION = $env:ATRIUM_LOCATION; ATRIUM_DEBUG_INPUTLAG = $env:ATRIUM_DEBUG_INPUTLAG }
    Remove-Item Env:ATRIUM_LOCATION, Env:ATRIUM_DEBUG_INPUTLAG -ErrorAction SilentlyContinue
    try {
        $ErrorActionPreference = 'Continue'
        $text = & go @goArgs 2>&1 | ForEach-Object { "$_" }
        $code = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = 'Stop'
        foreach ($k in $saved.Keys) { if ($null -ne $saved[$k]) { Set-Item "Env:$k" $saved[$k] } }
    }
    $sw.Stop()
    $lines = @($text)
    $header = "Command line: go " + ($goArgs -join ' ')
    Set-Content $outFile (@($header, '') + $lines)

    $fails = @(Get-Failures $lines)
    if ($code -ne 0 -and -not $fails.Count) {
        $first = $lines | Where-Object { $_ -match '^(FAIL|panic:|.*cannot|.*error)' } | Select-Object -First 1
        $fails = @([pscustomobject]@{ Name = '(run)'; Where = ''; Message = "$first" })
    }
    $ok = ($code -eq 0)
    $secs = [int]$sw.Elapsed.TotalSeconds
    if ($ok) { Write-Host ("PASS  {0}  {1}" -f $g.Name, (Format-Seconds $secs)) -ForegroundColor Green }
    else {
        Write-Host ("FAIL  {0}  {1}  {2} failing" -f $g.Name, (Format-Seconds $secs), $fails.Count) -ForegroundColor Red
        foreach ($f in $fails) { Write-Host ("  {0}  {1}  {2}" -f $f.Name, $f.Where, $f.Message) }
        Write-Host "  full output: $outFile"
    }
    [pscustomobject]@{
        Name = $g.Name; Ok = $ok; Seconds = $secs; Failed = @($fails | Where-Object { $_.Name -match '^Test' } | ForEach-Object { ($_.Name -split '/')[0] } | Sort-Object -Unique)
        Failures = @($fails | ForEach-Object { "$($_.Name) $($_.Where) $($_.Message)" })
    }
}

function Save-Result($r) {
    $state = Read-State
    $state[$r.Name] = @{ ok = $r.Ok; seconds = $r.Seconds; failed = $r.Failed; at = (Get-Date).ToString('s') }
    Save-State $state
}

function Show-Summary($results) {
    Write-Host ""
    Write-Host '=== Summary' -ForegroundColor Cyan
    $rows = $results | ForEach-Object {
        [pscustomobject]@{ Group = $_.Name; Result = $(if ($_.Ok) { 'PASS' } else { 'FAIL' }); Time = Format-Seconds $_.Seconds; Failing = $(if ($_.Ok) { '' } else { ($_.Failures | Measure-Object).Count }) }
    }
    $rows | Format-Table -AutoSize | Out-String | Write-Host
    $total = ($results | Measure-Object Seconds -Sum).Sum
    Write-Host ("{0} groups, {1} failed, total {2}" -f $results.Count, @($results | Where-Object { -not $_.Ok }).Count, (Format-Seconds $total))
    foreach ($r in $results | Where-Object { -not $_.Ok }) {
        Write-Host "  $($r.Name):"
        foreach ($f in $r.Failures) { Write-Host "    $f" }
    }
}

# ---- modes -----------------------------------------------------------------------------------------------

if ($List) {
    $state = Read-State
    foreach ($g in $Groups) {
        $names = Get-GroupTests $g
        $re = Get-RunRegex $names
        $last = if ($state.ContainsKey($g.Name)) { "$(Format-Seconds $state[$g.Name].seconds) (last run $($state[$g.Name].at), $(if ($state[$g.Name].ok) { 'pass' } else { 'FAIL' }))" } elseif ($g.Typical) { "$(Format-Seconds $g.Typical) (typical)" } else { 'not run yet' }
        Write-Host $g.Name -ForegroundColor Cyan -NoNewline
        Write-Host "  $($g.Purpose)"
        Write-Host "    packages: $($g.Packages -join ' ')"
        $pat = if ($null -eq $re) { 'all tests in the packages (no -run)' } else { "$($names.Count) tests: -run '" + $(if ($re.Length -gt 90) { $re.Substring(0, 90) + '...' } else { $re }) + "'" }
        Write-Host "    run:      $pat"
        Write-Host "    time:     $last"
    }
    return
}

if ($Check) {
    $bad = 0
    # 1. every package that has an integration test file is in a group
    $pkgs = Get-ChildItem (Join-Path $Root 'internal') -Recurse -Filter '*_integration_test.go' | ForEach-Object { $_.Directory.FullName } | Sort-Object -Unique |
        ForEach-Object { './' + ([IO.Path]::GetRelativePath($Root, $_) -replace '\\', '/') }
    $grouped = $Groups | ForEach-Object { $_.Packages } | Sort-Object -Unique
    foreach ($p in $pkgs) { if ($grouped -notcontains $p) { Write-Host "NO GROUP: package $p has integration tests but is in no group"; $bad++ } }
    foreach ($p in $grouped) { if ($pkgs -notcontains $p) { Write-Host "STALE: package $p is in a group but has no integration test file"; $bad++ } }
    # 2. names are unique, every group has a way to be told apart
    $dupe = $Groups | Group-Object Name | Where-Object Count -gt 1
    foreach ($d in $dupe) { Write-Host "DUPLICATE group name $($d.Name)"; $bad++ }
    # 3. per file: exactly one group of its package claims it
    foreach ($p in $grouped) {
        foreach ($f in Get-ChildItem (Join-Path $Root $p) -Filter '*_test.go' -File) {
            $owners = @(Get-FileOwner $p $f.Name)
            if ($owners.Count -eq 0) { Write-Host "NO GROUP: $p/$($f.Name)"; $bad++ }
            if ($owners.Count -gt 1) { Write-Host "TWO GROUPS: $p/$($f.Name) is in $($owners.Name -join ', ')"; $bad++ }
        }
    }
    # 4. per test: `go test -list` names, each found in the sources and owned by exactly one group
    $listOut = & go test -tags integration -list '.*' @grouped 2>&1 | ForEach-Object { "$_" }
    if ($LASTEXITCODE -ne 0) { Write-Host ($listOut -join "`n"); Write-Host 'go test -list failed'; exit 1 }
    $pending = @(); $total = 0; $perGroup = @{}
    foreach ($line in $listOut) {
        if ($line -match '^Test') { $pending += $line; continue }
        if ($line -match '^(ok|FAIL)\s+(\S+)') {
            $pkg = './' + $Matches[2].Substring($Module.Length + 1)
            $map = Get-TestFiles $pkg
            foreach ($n in $pending) {
                $total++
                if (-not $map.ContainsKey($n)) { Write-Host "NO GROUP: $pkg $n is not defined by a plain func in a _test.go file, so the script cannot place it"; $bad++; continue }
                $owners = @(Get-FileOwner $pkg $map[$n])
                if ($owners.Count -ne 1) { Write-Host "$($owners.Count) GROUPS: $pkg $n"; $bad++ }
                else { $perGroup[$owners[0].Name] = 1 + [int]$perGroup[$owners[0].Name] }
            }
            $pending = @()
        }
    }
    foreach ($g in $Groups) { Write-Host ("  {0,-20} {1,5} tests" -f $g.Name, [int]$perGroup[$g.Name]) }
    if ($bad) { Write-Host "CHECK FAILED: $bad problem(s)" -ForegroundColor Red; exit 1 }
    Write-Host "CHECK OK: $total tests, each in exactly one of $($Groups.Count) groups" -ForegroundColor Green
    return
}

if ($Failed) {
    $state = Read-State
    $results = @()
    foreach ($g in $Groups) {
        if (-not $state.ContainsKey($g.Name)) { continue }
        $names = @($state[$g.Name].failed)
        if ($state[$g.Name].ok -or -not $names.Count) { continue }
        $r = Invoke-Group $g $names
        Save-Result $r
        $results += $r
    }
    if (-not $results.Count) { Write-Host 'Nothing failed last time (or no run recorded). Run -Group or -All first.'; return }
    Show-Summary $results
    if ($results | Where-Object { -not $_.Ok }) { exit 1 }
    return
}

if ($All -or $Group) {
    $sel = if ($All) { $Groups } else {
        foreach ($n in $Group | ForEach-Object { $_ -split ',' }) {
            $g = $Groups | Where-Object Name -eq $n
            if (-not $g) { throw "no group named '$n'. Try -List." }
            $g
        }
    }
    $sel = @($sel)
    $results = @()
    if ($ResultFile) {
        # A child of the parallel run: run the groups in order, hand the records back, leave last.json to the parent.
        $results = @($sel | ForEach-Object { Invoke-Group $_ $null })
        ConvertTo-Json -InputObject $results -Depth 6 | Set-Content $ResultFile
        return
    }
    $byPackage = @($sel | Group-Object { $_.Packages[0] })
    if ($Serial -or $byPackage.Count -lt 2) {
        foreach ($g in $sel) {
            $r = Invoke-Group $g $null
            Save-Result $r
            $results += $r
        }
    }
    else {
        # One child pwsh per package, four at a time. Groups of one package stay in order inside their child.
        New-Item -ItemType Directory -Force $OutDir | Out-Null
        $pwsh = (Get-Process -Id $PID).Path
        $jobs = @()
        $queue = [System.Collections.Queue]::new($byPackage)
        while ($queue.Count -or $jobs.Count) {
            while ($queue.Count -and $jobs.Count -lt 4) {
                $pk = $queue.Dequeue()
                $tag = ($pk.Name -replace '[^\w]+', '_').Trim('_')
                $job = [pscustomobject]@{
                    Names = $pk.Group.Name; Json = Join-Path $OutDir "job-$tag.json"; Log = Join-Path $OutDir "job-$tag.log"
                }
                $job | Add-Member Proc (Start-Process $pwsh -PassThru -WindowStyle Hidden -RedirectStandardOutput $job.Log -ArgumentList (
                    '-NoProfile', '-File', $PSCommandPath, '-Group', ($job.Names -join ','), '-ResultFile', $job.Json))
                Write-Host "started $($pk.Name): $($job.Names -join ', ')"
                $jobs += $job
            }
            Start-Sleep -Milliseconds 500
            foreach ($job in @($jobs | Where-Object { $_.Proc.HasExited })) {
                Get-Content $job.Log | Write-Host
                $jobs = @($jobs | Where-Object { $_ -ne $job })
            }
        }
        $byName = @{}
        foreach ($pk in $byPackage) {
            $tag = ($pk.Name -replace '[^\w]+', '_').Trim('_')
            $json = Join-Path $OutDir "job-$tag.json"
            if (-not (Test-Path $json)) { throw "the run of $($pk.Name) wrote no result, see $(Join-Path $OutDir "job-$tag.log")" }
            foreach ($r in @(Get-Content $json -Raw | ConvertFrom-Json)) { $byName[$r.Name] = $r }
        }
        foreach ($g in $sel) {
            $r = $byName[$g.Name]
            $r.Failed = @($r.Failed); $r.Failures = @($r.Failures)
            Save-Result $r
            $results += $r
        }
    }
    if ($results.Count -gt 1) { Show-Summary $results }
    if ($results | Where-Object { -not $_.Ok }) { exit 1 }
    return
}

Get-Help $PSCommandPath | Out-Null
Write-Host 'Usage: scripts/test-groups.ps1 -List | -Group <name>[,<name>] | -All | -Failed | -Check'
