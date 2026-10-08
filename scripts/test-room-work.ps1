# Tests for scripts/room-work.ps1: the work root, the tool caches, and the agent pack. Needs no ssh, no hub and no room:
#
#   pwsh -NoProfile -File scripts/test-room-work.ps1
#
# The text helpers are called directly. The remote scripts are BUILT the way provision builds them and then RUN here, locally:
# the Windows ones by a child pwsh with HOME, USERPROFILE and APPDATA pointed into a temp folder, the Unix ones by sh (skipped
# when this machine has none), so the files they edit are really edited, twice, to prove the second run changes nothing. The
# one thing replaced is the user environment variable CARGO_HOME, which on Windows is the registry: the test swaps that call for
# a process variable so it never writes the real one. What this cannot run: ssh, a second drive, icacls and the permission
# difference between a standard account and an administrator (the parent-folder check is run for an account that CAN and for a
# path that does not exist, never for one that is denied), Windows PowerShell 5.1, and the flow in provision-room.ps1, which
# needs a hub and a room.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'room-work.ps1')

$script:failed = 0
$script:ran = 0
function Check {
    param([string] $name, $got, $want)
    $script:ran++
    $g = (@($got) | ForEach-Object { "$_" }) -join '|'
    $w = (@($want) | ForEach-Object { "$_" }) -join '|'
    if ($g -ceq $w) { Write-Host "ok   $name" }
    else { $script:failed++; Write-Host "FAIL $name`n     want: $w`n     got:  $g" }
}
$isWin = $IsWindows -or $env:OS -eq 'Windows_NT'
$tmp = Join-Path ([IO.Path]::GetTempPath()) "room-work-test-$([guid]::NewGuid().ToString('N').Substring(0, 8))"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$sh = (Get-Command sh -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source

# ── Test-WorkRootArg ────────────────────────────────────────────────────────

Check 'arg: a folder on a drive' (Test-WorkRootArg 'V:\localai') $null
Check 'arg: forward slashes' (Test-WorkRootArg 'V:/localai') $null
Check 'arg: unix folder' (Test-WorkRootArg '/srv/work/localai') $null
Check 'arg: a drive root is refused' ([bool](Test-WorkRootArg 'V:\')) $true
Check 'arg: a bare drive is refused' ([bool](Test-WorkRootArg 'V:')) $true
Check 'arg: the unix root is refused' ([bool](Test-WorkRootArg '/')) $true
Check 'arg: relative is refused' ([bool](Test-WorkRootArg 'work/localai')) $true
Check 'arg: tilde is refused (a work root names the work drive)' ([bool](Test-WorkRootArg '~/work')) $true
Check 'arg: UNC is refused' ([bool](Test-WorkRootArg '\\srv\share\localai')) $true
Check 'arg: dot dot is refused' ([bool](Test-WorkRootArg 'V:\work\..\localai')) $true
Check 'arg: semicolon is refused' ([bool](Test-WorkRootArg 'V:\a;b')) $true
Check 'arg: empty is refused' ([bool](Test-WorkRootArg '')) $true

# ── the layout and the parents ──────────────────────────────────────────────

$l = Get-WorkLayout 'V:\localai\'
Check 'layout: git, reviews, handoff' @($l.Root, $l.Git, $l.Reviews, $l.Handoff) @('V:/localai', 'V:/localai/git', 'V:/localai/reviews', 'V:/localai/handoff')
Check 'layout: caches' @($l.Npm, $l.GoMod, $l.GoBuild, $l.Pip, $l.Cargo) @('V:/localai/cache/npm', 'V:/localai/cache/go-mod', 'V:/localai/cache/go-build', 'V:/localai/cache/pip', 'V:/localai/cache/cargo')
Check 'dirs: the root first, every one under it' (@(Get-WorkDirs $l | Where-Object { $_ -notlike 'V:/localai*' }).Count, (Get-WorkDirs $l)[0]) @(0, 'V:/localai')
Check 'parents: one below a drive is the drive' (Get-WorkParents 'V:\localai' 'windows') @('V:\')
Check 'parents: two below' (Get-WorkParents 'V:\work\localai' 'windows') @('V:\', 'V:\work')
Check 'parents: forward slashes in, back slashes out' (Get-WorkParents 'V:/a/b/c' 'windows') @('V:\', 'V:\a', 'V:\a\b')
Check 'parents: unix' (Get-WorkParents '/srv/work/localai' 'linux') @('/', '/srv', '/srv/work')
Check 'parents: unix one below the root' (Get-WorkParents '/work' 'linux') @('/')

# ── what an administrator is told to run ────────────────────────────────────

$a = Get-WorkAdminLines 'windows' 'SG3\localai' 'V:\localai' @('V:\') $false $false
Check 'admin: the drive gets the attributes only, not inherited, the root gets nothing when it is fine' $a @("icacls V:\ /grant 'SG3\localai:(RA,REA)'")
$a = Get-WorkAdminLines 'windows' 'SG3\localai' 'V:\localai' @('V:\') $true $false
Check 'admin: a missing root is made and granted full control, inherited' $a @("icacls V:\ /grant 'SG3\localai:(RA,REA)'", 'New-Item -ItemType Directory V:\localai', "icacls V:\localai /grant 'SG3\localai:(OI)(CI)F'")
$a = Get-WorkAdminLines 'windows' 'SG3\localai' 'V:\work\localai' @('V:\', 'V:\work') $false $true
Check 'admin: every parent that failed, each its own line' @($a.Count, $a[0], $a[1], $a[2]) @(3, "icacls V:\ /grant 'SG3\localai:(RA,REA)'", "icacls V:\work /grant 'SG3\localai:(RA,REA)'", "icacls V:\work\localai /grant 'SG3\localai:(OI)(CI)F'")
Check 'admin: no line ever grants a parent more than the attributes, or inherits it' (@(Get-WorkAdminLines 'windows' 'SG3\localai' 'V:\w\localai' @('V:\', 'V:\w') $true $true | Where-Object { $_ -like 'icacls *' -and $_ -notlike '*localai /grant*' -and $_ -match '\((OI|CI)\)|:\(F\)|:\(M\)|RX' }).Count) 0
Check 'admin: a name with a space is quoted' (Get-WorkAdminLines 'windows' 'PC\my user' 'V:\a b' @('V:\') $false $true) @("icacls V:\ /grant 'PC\my user:(RA,REA)'", "icacls 'V:\a b' /grant 'PC\my user:(OI)(CI)F'")
Check 'admin: unix opens each parent to the account alone, by ACL, and chowns the root' (Get-WorkAdminLines 'linux' 'localai' '/srv/work/localai' @('/srv', '/srv/work') $false $true) @('sudo setfacl -m u:localai:x /srv', 'sudo setfacl -m u:localai:x /srv/work', 'sudo chown -R localai: /srv/work/localai')
Check 'admin: unix never says chmod o+x' (@(Get-WorkAdminLines 'linux' 'localai' '/srv/a/localai' @('/', '/srv', '/srv/a') $true $false @('/srv', '/srv/a') | Where-Object { $_ -match 'chmod o' }).Count) 0
Check 'admin: unix has no line for the filesystem root' (@(Get-WorkAdminLines 'linux' 'localai' '/work/localai' @('/') $true $false | Where-Object { $_ -match ' /$' -or $_ -match ' / ' }).Count) 0
Check 'admin: a mac gets chmod +a, not setfacl' (Get-WorkAdminLines 'darwin' 'localai' '/Volumes/w/localai' @('/Volumes/w') $false $false) @("sudo chmod +a 'user:localai allow search' /Volumes/w")
Check 'admin: unix makes a missing root' (Get-WorkAdminLines 'linux' 'localai' '/srv/localai' @() $true $false) @('sudo install -d -o localai -m 755 /srv/localai')
Check 'admin: unix quotes for sh, a quote in a name included' (Get-WorkAdminLines 'linux' 'localai' "/srv/it's/localai" @("/srv/it's") $false $false) @("sudo setfacl -m u:localai:x '/srv/it'\''s'")
$a = Get-WorkAdminLines 'windows' 'SG3\localai' 'V:\a\localai' @('V:\', 'V:\a') $true $false @('V:\a')
Check 'admin: a missing parent is made BEFORE it is granted, and the root after both' $a @('New-Item -ItemType Directory V:\a', "icacls V:\ /grant 'SG3\localai:(RA,REA)'", "icacls V:\a /grant 'SG3\localai:(RA,REA)'", 'New-Item -ItemType Directory V:\a\localai', "icacls V:\a\localai /grant 'SG3\localai:(OI)(CI)F'")
$a = Get-WorkAdminLines 'linux' 'localai' '/srv/a/b/localai' @('/srv/a', '/srv/a/b') $true $false @('/srv/a', '/srv/a/b')
Check 'admin: unix makes missing parents top down before it opens them' $a @('sudo install -d -m 755 /srv/a', 'sudo install -d -m 755 /srv/a/b', 'sudo setfacl -m u:localai:x /srv/a', 'sudo setfacl -m u:localai:x /srv/a/b', 'sudo install -d -o localai -m 755 /srv/a/b/localai')

# ── the argument against the room's OS, and the pack's arguments ────────────

Check 'os: a drive path on a windows room' (Test-WorkRootOs 'windows' 'V:\localai') $null
Check 'os: a unix path on a unix room' (Test-WorkRootOs 'linux' '/srv/localai') $null
Check 'os: a drive path on a unix room is refused, not made under $HOME' ([bool](Test-WorkRootOs 'linux' 'V:\x' | Where-Object { $_ -like '*drive path*linux*' })) $true
Check 'os: a unix path on a windows room is refused, naming a drive' ([bool](Test-WorkRootOs 'windows' '/srv/x' | Where-Object { $_ -like '*Unix path*Windows*V:\localai*' })) $true
$r = Invoke-WorkRoot { param($s) throw 'the remote must not be asked' } 'windows' '/srv/x' $true 'h'
Check 'os: Invoke-WorkRoot stops before asking the remote' @($r.Code, $r.Steps[0].Status, [bool]($r.Steps[0].Detail -like '*Unix path*')) @(1, 'fail', $true)
Check 'pack args: the default repo and branch' (Test-AgentPackArg 'dovholuknf/dotfiles' 'main') $null
Check 'pack args: a branch with a slash' (Test-AgentPackArg 'o/r' 'feature/x-1') $null
Check 'pack args: a branch that starts with - is refused (it would be a git option)' ([bool](Test-AgentPackArg 'o/r' '--upload-pack=x')) $true
Check 'pack args: a branch with .. is refused' ([bool](Test-AgentPackArg 'o/r' 'a..b')) $true
Check 'pack args: a repo that is not owner/name is refused' @([bool](Test-AgentPackArg 'o' 'main'), [bool](Test-AgentPackArg '-o/r' 'main'), [bool](Test-AgentPackArg 'o/r;x' 'main')) @($true, $false, $true)

# ── the verdict on a probe ──────────────────────────────────────────────────

function New-Probe { param([string[]] $lines, [int] $n) ConvertFrom-WorkProbe $lines $n }
$okP = New-Probe @('login=SG3\localai', 'drive=True', 'parents=1', 'parent0=ok', 'exists=True', 'writable=True') 1
$v = Get-WorkRootVerdict 'windows' 'V:\localai' $okP $true 'sg3'
Check 'verdict: sound is ok, code 0' @($v.Status, $v.Code, [bool]($v.Detail -like '*V:\localai is there, writable by SG3\localai*every parent (V:\) is examinable*')) @('ok', 0, $true)
$madeP = New-Probe @('login=SG3\localai', 'drive=True', 'parents=1', 'parent0=ok', 'exists=False', 'made=True', 'writable=True') 1
Check 'verdict: made now is done' (Get-WorkRootVerdict 'windows' 'V:\localai' $madeP $true 'sg3').Status 'done'
$badP = New-Probe @('login=SG3\localai', 'drive=True', 'parents=2', 'parent0=fail', 'parent1=ok', 'exists=False', 'made=False') 2
$v = Get-WorkRootVerdict 'windows' 'V:\work\localai' $badP $true 'sg3'
Check 'verdict: an unexaminable parent is a fail with code 13' @($v.Status, $v.Code) @('fail', 13)
Check 'verdict: and the administrator lines are the attributes grant and the root' $v.AdminLines @("icacls V:\ /grant 'SG3\localai:(RA,REA)'", 'New-Item -ItemType Directory V:\work\localai', "icacls V:\work\localai /grant 'SG3\localai:(OI)(CI)F'")
Check 'verdict: the detail names the Claude Code prompt and that nothing is listed or created' ([bool]($v.Detail -like '*V:\*Claude Code*unanswerable*no listing, no creating, not inherited*')) $true
$v = Get-WorkRootVerdict 'windows' 'V:\work\localai' $badP $false 'sg3'
Check 'verdict: -Check says the same, so the run is read only and still exits 13' @($v.Status, $v.Code) @('fail', 13)
$noDrive = New-Probe @('login=SG3\localai', 'drive=False', 'parents=1', 'parent0=fail', 'exists=False') 1
$v = Get-WorkRootVerdict 'windows' 'V:\localai' $noDrive $true 'sg3'
Check 'verdict: a drive that is not there is 13 and offers no icacls line' @($v.Status, $v.Code, $v.AdminLines.Count, [bool]($v.Detail -like '*V:\ is not there*')) @('fail', 13, 0, $true)
$noWrite = New-Probe @('login=SG3\localai', 'drive=True', 'parents=1', 'parent0=ok', 'exists=True', 'writable=False') 1
$v = Get-WorkRootVerdict 'windows' 'V:\localai' $noWrite $true 'sg3'
Check 'verdict: a root the account cannot write is 13 with a grant on the root only' @($v.Code, $v.AdminLines) @(13, @("icacls V:\localai /grant 'SG3\localai:(OI)(CI)F'"))
$missingCheck = New-Probe @('login=SG3\localai', 'drive=True', 'parents=1', 'parent0=ok', 'exists=False') 1
$v = Get-WorkRootVerdict 'windows' 'V:\localai' $missingCheck $false 'sg3'
Check 'verdict: -Check with a missing root and good parents is a warn, code 0' @($v.Status, $v.Code) @('warn', 0)
Check 'verdict: a probe that said nothing is a fail, not a pass' (Get-WorkRootVerdict 'windows' 'V:\localai' (New-Probe @() 1) $true 'sg3').Status 'fail'
Check 'verdict: a root directly below the unix root has one parent' @((Get-WorkParents '/work' 'linux').Count, (New-Probe @('login=u', 'drive=True', 'parents=1', 'parent0=ok', 'exists=True', 'writable=True') 1).Ok) @(1, $true)

$goneP = New-Probe @('login=SG3\localai', 'drive=True', 'parents=2', 'parent0=ok', 'parent1=missing', 'exists=False', 'made=False') 2
Check 'probe: a parent that is missing is bad, and missing' @($goneP.BadIndexes, $goneP.MissingIndexes) @(1, 1)
$v = Get-WorkRootVerdict 'windows' 'V:\a\localai' $goneP $true 'sg3'
Check 'verdict: a missing parent is 13, the lines make it before they grant it, and it is called missing not unexaminable' @($v.Code, $v.AdminLines[0], [bool]($v.Detail -like '*V:\a is not there*'), [bool]($v.Detail -notlike '*cannot examine*')) @(13, 'New-Item -ItemType Directory V:\a', $true, $true)
$v = Get-WorkRootVerdict 'linux' '/srv/a/localai' (New-Probe @('login=localai', 'drive=True', 'parents=3', 'parent0=ok', 'parent1=ok', 'parent2=missing', 'exists=False', 'made=False') 3) $true 'h'
Check 'verdict: unix, a missing parent is made and opened to the account only' $v.AdminLines @('sudo install -d -m 755 /srv/a', 'sudo setfacl -m u:localai:x /srv/a', 'sudo install -d -o localai -m 755 /srv/a/localai')

# ── the probe, run for real ─────────────────────────────────────────────────

if ($isWin) {
    $pr = Join-Path $tmp 'a\localai'
    $parents = @(Get-WorkParents $pr 'windows')
    function Run-Ps { param([string] $script, [hashtable] $env, [string] $exe = 'pwsh')
        $f = Join-Path $tmp "run-$([guid]::NewGuid().ToString('N').Substring(0, 6)).ps1"
        Set-Content -LiteralPath $f -Value $script -Encoding UTF8
        $envs = foreach ($k in $env.Keys) { "`$env:$k = '$($env[$k])'" }
        $o = & $exe -NoProfile -Command ((@($envs) -join '; ') + "; & '$f'") 2>&1
        @($o | ForEach-Object { "$_" })
    }
    New-Item -ItemType Directory -Force -Path (Join-Path $tmp 'a') | Out-Null
    $o = Run-Ps (Get-WorkProbeScript 'windows' $pr $false) @{}
    $p = ConvertFrom-WorkProbe $o $parents.Count
    Check 'probe -Check: every parent is read, the root is not made' @($p.Ok, $p.BadIndexes.Count, $p.Exists, (Test-Path -LiteralPath $pr)) @($true, 0, $false, $false)
    $o = Run-Ps (Get-WorkProbeScript 'windows' $pr $true) @{}
    $p = ConvertFrom-WorkProbe $o $parents.Count
    Check 'probe: the root is made and writable, and the probe file is gone' @($p.Made, $p.Writable, @(Get-ChildItem -LiteralPath $pr -Force).Count) @($true, $true, 0)
    $o = Run-Ps (Get-WorkProbeScript 'windows' $pr $true) @{}
    $p = ConvertFrom-WorkProbe $o $parents.Count
    Check 'probe, again: it is there, nothing made' @($p.Exists, $p.Made, $p.Writable) @($true, $false, $true)
    $none = 'C:\no-such-folder-' + [guid]::NewGuid().ToString('N').Substring(0, 8) + '\localai'
    $o = Run-Ps (Get-WorkProbeScript 'windows' $none $true) @{}
    $p = ConvertFrom-WorkProbe $o 2
    Check 'probe: a parent that is not there cannot be examined, and no root is made' @($p.Ok, @($p.BadIndexes), $p.Exists) @($true, 1, $false)
    Check 'probe: and it is called missing, so the lines make it' @($p.MissingIndexes) @(1)
    # -Check writes NOTHING, not even a probe file: the answer comes from the ACL
    $wt = (Get-Item -LiteralPath $pr).LastWriteTimeUtc.Ticks
    $o = Run-Ps (Get-WorkProbeScript 'windows' $pr $false) @{}
    $p = ConvertFrom-WorkProbe $o $parents.Count
    Check 'probe -Check on a root: writable is read from the ACL, and the folder is untouched' @($p.Exists, $p.Writable, @(Get-ChildItem -LiteralPath $pr -Force).Count, ((Get-Item -LiteralPath $pr).LastWriteTimeUtc.Ticks -eq $wt)) @($true, $true, 0, $true)
    $me = [Security.Principal.WindowsIdentity]::GetCurrent().User
    $deny = New-Object Security.AccessControl.FileSystemAccessRule($me, 'CreateFiles,AppendData', 'Deny')
    $acl = Get-Acl -LiteralPath $pr; $acl.AddAccessRule($deny); Set-Acl -LiteralPath $pr -AclObject $acl
    try {
        $o = Run-Ps (Get-WorkProbeScript 'windows' $pr $false) @{}
        $p = ConvertFrom-WorkProbe $o $parents.Count
        Check 'probe -Check: a Deny entry for the login on the root makes it not writable' @($p.Exists, $p.Writable) @($true, $false)
    } finally {
        $acl = Get-Acl -LiteralPath $pr; [void]$acl.RemoveAccessRule($deny); Set-Acl -LiteralPath $pr -AclObject $acl
    }
    $q = $null; foreach ($c in 'Q', 'R', 'S', 'T', 'U', 'W', 'X', 'Y', 'Z') { if (-not (Test-Path "${c}:\")) { $q = $c; break } }
    if ($q) {
        $o = Run-Ps (Get-WorkProbeScript 'windows' "${q}:\localai" $true) @{}
        $p = ConvertFrom-WorkProbe $o 1
        Check 'probe: a drive letter with no drive says so' @($p.DriveMissing, @($p.BadIndexes)) @($true, 0)
    }

    # ── the caches, run for real, twice ─────────────────────────────────────
    $home1 = Join-Path $tmp 'home'; $app = Join-Path $home1 'AppData'
    New-Item -ItemType Directory -Force -Path $home1, $app | Out-Null
    $w = (Join-Path $tmp 'work') -replace '\\', '/'
    $lay = Get-WorkLayout $w
    $e = @{ HOME = $home1; USERPROFILE = $home1; APPDATA = $app; TEST_CARGO = '' }
    function Cache-Script { param([bool] $make)
        $s = Get-WorkCacheScript 'windows' $lay $make
        $s = $s.Replace("[Environment]::GetEnvironmentVariable('CARGO_HOME', 'User')", '$env:TEST_CARGO')
        $s = $s.Replace("[Environment]::SetEnvironmentVariable('CARGO_HOME', `$cargo, 'User')", '$env:TEST_CARGO = $cargo; Set-Content (Join-Path $env:HOME ''cargo.env'') $cargo')
        $s
    }
    # an npmrc with other lines and a repeated cache, a pip.ini with another section first
    Set-Content -LiteralPath (Join-Path $home1 '.npmrc') -Value @('registry=https://r.example/', 'cache=C:/old/npm', '; keep', 'cache=C:/older') -Encoding UTF8
    New-Item -ItemType Directory -Force -Path (Join-Path $app 'pip') | Out-Null
    Set-Content -LiteralPath (Join-Path $app 'pip\pip.ini') -Value @('[install]', 'timeout = 5', '', '[global]', 'index-url = https://i.example/') -Encoding UTF8
    $o = Run-Ps (Cache-Script $false) $e
    $kv = @{}; foreach ($l in $o) { $i = $l.IndexOf('='); if ($i -gt 0) { $kv[$l.Substring(0, $i)] = $l.Substring($i + 1) } }
    Check '-Check: every cache says todo, the folders say todo' @(($kv.dirs -like 'todo*'), $kv.npm, $kv.gomod, $kv.gocache, $kv.pip, $kv.cargo) @($true, 'todo', 'todo', 'todo', 'todo', 'todo')
    Check '-Check: nothing was written' @((Get-Content -LiteralPath (Join-Path $home1 '.npmrc') | Where-Object { $_ -like 'cache=*' }).Count, (Test-Path (Join-Path $app 'go\env')), (Test-Path (Join-Path $tmp 'work'))) @(2, $false, $false)
    $o = Run-Ps (Cache-Script $true) $e
    $kv = @{}; foreach ($l in $o) { $i = $l.IndexOf('='); if ($i -gt 0) { $kv[$l.Substring(0, $i)] = $l.Substring($i + 1) } }
    Check 'run: every cache says done' @(($kv.dirs -like 'made*'), $kv.npm, $kv.gomod, $kv.gocache, $kv.pip, $kv.cargo) @($true, 'done', 'done', 'done', 'done', 'done')
    Check 'run: the folders are under the work root' @((Test-Path (Join-Path $tmp 'work\git')), (Test-Path (Join-Path $tmp 'work\reviews')), (Test-Path (Join-Path $tmp 'work\handoff')), (Test-Path (Join-Path $tmp 'work\cache\npm')), (Test-Path (Join-Path $tmp 'work\cache\cargo'))) @($true, $true, $true, $true, $true)
    Check 'run: .npmrc keeps its other lines and has one cache line, the work root' @((Get-Content -LiteralPath (Join-Path $home1 '.npmrc'))) @('registry=https://r.example/', "cache=$($lay.Npm)", '; keep')
    Check 'run: go env has GOMODCACHE and GOCACHE' @((Get-Content -LiteralPath (Join-Path $app 'go\env'))) @("GOMODCACHE=$($lay.GoMod)", "GOCACHE=$($lay.GoBuild)")
    Check 'run: pip.ini keeps [install] and the other global key, and gains cache-dir inside [global]' @((Get-Content -LiteralPath (Join-Path $app 'pip\pip.ini'))) @('[install]', 'timeout = 5', '', '[global]', "cache-dir = $($lay.Pip)", 'index-url = https://i.example/')
    Check 'run: CARGO_HOME is the work root' (Get-Content -LiteralPath (Join-Path $home1 'cargo.env')) $lay.Cargo
    $before = @(Get-ChildItem -LiteralPath $home1 -Recurse -File | Sort-Object FullName | ForEach-Object { "$($_.FullName)|$($_.Length)|$($_.LastWriteTimeUtc.Ticks)" })
    $e2 = $e.Clone(); $e2.TEST_CARGO = $lay.Cargo
    $o = Run-Ps (Cache-Script $true) $e2
    $kv = @{}; foreach ($l in $o) { $i = $l.IndexOf('='); if ($i -gt 0) { $kv[$l.Substring(0, $i)] = $l.Substring($i + 1) } }
    Check 'rerun: everything says ok' @($kv.dirs, $kv.npm, $kv.gomod, $kv.gocache, $kv.pip, $kv.cargo) @('ok', 'ok', 'ok', 'ok', 'ok', 'ok')
    $after = @(Get-ChildItem -LiteralPath $home1 -Recurse -File | Sort-Object FullName | ForEach-Object { "$($_.FullName)|$($_.Length)|$($_.LastWriteTimeUtc.Ticks)" })
    Check 'rerun: no file was written' ($after -join "`n") ($before -join "`n")
    $vs = @(Get-WorkCacheVerdict @($o | Where-Object { $_ -notmatch 'tool=' }) $lay $true)
    Check 'rerun verdict: the folders and the caches are ok' @(@($vs | ForEach-Object { $_.Step }), @($vs | ForEach-Object { $_.Status })) @(@('work-dirs', 'work-cache'), @('ok', 'ok'))

    # NON-ASCII in the user's own files survives, under Windows PowerShell 5.1 (what the remote runs), which reads a BOM-less file as ANSI
    $ps51 = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-Path -LiteralPath $ps51)) { $ps51 = $null }
    if ($ps51) {
        $home2 = Join-Path $tmp 'home51'; $app2 = Join-Path $home2 'AppData'
        New-Item -ItemType Directory -Force -Path $home2, (Join-Path $app2 'pip') | Out-Null
        $u8 = New-Object Text.UTF8Encoding $false
        $caf = "caf$([char]0xE9) $([char]0x4E2D)$([char]0x6587)"
        [IO.File]::WriteAllText((Join-Path $home2 '.npmrc'), "; $caf`r`nregistry=https://r.example/`r`n", $u8)
        [IO.File]::WriteAllText((Join-Path $app2 'pip\pip.ini'), "[global]`r`n; $caf`r`n", $u8)
        $e5 = @{ HOME = $home2; USERPROFILE = $home2; APPDATA = $app2; TEST_CARGO = '' }
        [void](Run-Ps (Cache-Script $true) $e5 $ps51)
        $np = [IO.File]::ReadAllText((Join-Path $home2 '.npmrc'), $u8); $pp = [IO.File]::ReadAllText((Join-Path $app2 'pip\pip.ini'), $u8)
        Check 'ps 5.1: non-ASCII in .npmrc and pip.ini is kept as it was, and the key is added' @(($np -like "*; $caf*"), ($np -like "*cache=$($lay.Npm)*"), ($pp -like "*; $caf*"), ($pp -like "*cache-dir = $($lay.Pip)*")) @($true, $true, $true, $true)
    } else { Write-Host 'skip ps 5.1 non-ASCII: no powershell.exe on this machine' }
}

# ── the unix cache script, run for real when there is an sh ─────────────────

if ($sh) {
    $uh = Join-Path $tmp 'uhome'
    New-Item -ItemType Directory -Force -Path $uh | Out-Null
    $uw = (Join-Path $tmp 'uwork') -replace '\\', '/'
    if ($uw -match '^([A-Za-z]):(.*)$') { $uw = "/$($Matches[1].ToLower())$($Matches[2])" }
    $ulay = Get-WorkLayout $uw
    $uhs = ($uh -replace '\\', '/'); if ($uhs -match '^([A-Za-z]):(.*)$') { $uhs = "/$($Matches[1].ToLower())$($Matches[2])" }
    function ConvertTo-MsysPath { param([string] $p) $p = $p -replace '\\', '/'; if ($p -match '^([A-Za-z]):(.*)$') { "/$($Matches[1].ToLower())$($Matches[2])" } else { $p } }
    function Run-Sh { param([string] $script, [string] $h = $uhs)
        $f = Join-Path $tmp "run-$([guid]::NewGuid().ToString('N').Substring(0, 6)).sh"
        [IO.File]::WriteAllText($f, ($script -replace "`r", ''), [Text.UTF8Encoding]::new($false))
        $env:HOME = $h; $env:XDG_CONFIG_HOME = ''
        $o = & $sh $f 2>&1
        @($o | ForEach-Object { "$_" })
    }
    $realHome = $env:HOME
    try {
        Set-Content -LiteralPath (Join-Path $uh '.npmrc') -Value @('registry=https://r.example/', 'cache=/old/npm', 'cache=/older') -Encoding ascii
        New-Item -ItemType Directory -Force -Path (Join-Path $uh '.config/pip') | Out-Null
        Set-Content -LiteralPath (Join-Path $uh '.config/pip/pip.conf') -Value @('[install]', 'timeout = 5', '[global]', 'index-url = https://i.example/') -Encoding ascii
        $o = Run-Sh (Get-WorkCacheScript 'linux' $ulay $false)
        $kv = @{}; foreach ($l in $o) { $i = $l.IndexOf('='); if ($i -gt 0) { $kv[$l.Substring(0, $i)] = $l.Substring($i + 1) } }
        Check 'unix -Check: todo for each, nothing written' @(($kv.dirs -like 'todo*'), $kv.npm, $kv.gomod, $kv.pip, $kv.cargo, (Test-Path (Join-Path $uh '.profile')), (Test-Path (Join-Path $tmp 'uwork'))) @($true, 'todo', 'todo', 'todo', 'todo', $false, $false)
        $o = Run-Sh (Get-WorkCacheScript 'linux' $ulay $true)
        $kv = @{}; foreach ($l in $o) { $i = $l.IndexOf('='); if ($i -gt 0) { $kv[$l.Substring(0, $i)] = $l.Substring($i + 1) } }
        Check 'unix run: done for each' @(($kv.dirs -like 'made*'), $kv.npm, $kv.gomod, $kv.gocache, $kv.pip, $kv.cargo) @($true, 'done', 'done', 'done', 'done', 'done')
        Check 'unix run: .npmrc keeps the registry and has one cache line' @((Get-Content -LiteralPath (Join-Path $uh '.npmrc'))) @('registry=https://r.example/', "cache=$($ulay.Npm)")
        Check 'unix run: pip.conf has cache-dir inside [global]' @((Get-Content -LiteralPath (Join-Path $uh '.config/pip/pip.conf'))) @('[install]', 'timeout = 5', '[global]', "cache-dir = $($ulay.Pip)", 'index-url = https://i.example/')
        Check 'unix run: the profile exports CARGO_HOME' @((Get-Content -LiteralPath (Join-Path $uh '.profile'))) @("export CARGO_HOME='$($ulay.Cargo)'  # atrium work root")
        $o = Run-Sh (Get-WorkCacheScript 'linux' $ulay $true)
        $kv = @{}; foreach ($l in $o) { $i = $l.IndexOf('='); if ($i -gt 0) { $kv[$l.Substring(0, $i)] = $l.Substring($i + 1) } }
        Check 'unix rerun: ok for each' @($kv.dirs, $kv.npm, $kv.gomod, $kv.gocache, $kv.pip, $kv.cargo) @('ok', 'ok', 'ok', 'ok', 'ok', 'ok')
        $o = Run-Sh (Get-WorkProbeScript 'linux' "$uw/sub" $true)
        $p = ConvertFrom-WorkProbe $o @(Get-WorkParents "$uw/sub" 'linux').Count
        Check 'unix probe: the root is made, writable, every parent examinable' @($p.Ok, $p.BadIndexes.Count, $p.Made, $p.Writable) @($true, 0, $true, $true)
        # -Check on a bare account writes NOTHING: no ~/.config, no temp file left, in the home or in TMPDIR
        $uh2 = Join-Path $tmp 'uhome2'; $ut = Join-Path $tmp 'utmp'
        New-Item -ItemType Directory -Force -Path $uh2, $ut | Out-Null
        $env:TMPDIR = ConvertTo-MsysPath $ut
        $o = Run-Sh (Get-WorkCacheScript 'linux' $ulay $false) (ConvertTo-MsysPath $uh2)
        Check 'unix -Check on a bare home: the home is still empty and TMPDIR holds nothing' @(@(Get-ChildItem -LiteralPath $uh2 -Recurse -Force).Count, @(Get-ChildItem -LiteralPath $ut -Force).Count) @(0, 0)
        $o = Run-Sh (Get-WorkCacheScript 'linux' $ulay $true) (ConvertTo-MsysPath $uh2)
        Check 'unix run on a bare home: the config folders are made by the run, and TMPDIR is left clean' @((Test-Path (Join-Path $uh2 '.config/go/env')), (Test-Path (Join-Path $uh2 '.config/pip/pip.conf')), @(Get-ChildItem -LiteralPath $ut -Force).Count) @($true, $true, 0)
        # a quote in the work root cannot break out of the profile line
        $qlay = Get-WorkLayout "/tmp/it's/work"
        $uh3 = Join-Path $tmp 'uhome3'; New-Item -ItemType Directory -Force -Path $uh3 | Out-Null
        $o = Run-Sh (Get-WorkCacheScript 'linux' $qlay $true) (ConvertTo-MsysPath $uh3)
        $got = & $sh -c ". '$(ConvertTo-MsysPath $uh3)/.profile'; printf %s ""`$CARGO_HOME""" 2>&1
        Check 'unix: a work root with a quote in it is exported intact by the profile line' @($got) @("/tmp/it's/work/cache/cargo")
        $env:TMPDIR = $null
    } finally { $env:HOME = $realHome; $env:TMPDIR = $null }
} else { Write-Host 'skip unix cache and probe scripts: no sh on this machine' }

# ── the unix scripts are POSIX sh, not bash ─────────────────────────────────

# Invoke-Remote pipes them to `sh -s`, which is dash on Debian and Ubuntu. A bash array, [[, local or <<< is a syntax error there.
$bashisms = '(?m)(\w+=\(|\[@\]|\[\*\]|\$\{[A-Za-z_]+\[|\[\[ |\blocal\s+\w|<<<|^\s*function\s+\w+|\bsource\s)'
$ulay2 = Get-WorkLayout '/srv/localai'
$shScripts = [ordered]@{
    'probe -Check' = (Get-WorkProbeScript 'linux' '/srv/localai' $false); 'probe' = (Get-WorkProbeScript 'linux' '/srv/localai' $true)
    'cache -Check' = (Get-WorkCacheScript 'linux' $ulay2 $false); 'cache' = (Get-WorkCacheScript 'linux' $ulay2 $true)
    'pack install' = (Get-AgentPackInstallScript 'linux' $true); 'pack state' = (Get-AgentPackStateScript 'linux')
}
foreach ($k in $shScripts.Keys) { Check "posix: the unix $k script has no bashism" @([regex]::Matches($shScripts[$k], $bashisms) | ForEach-Object { $_.Value }) @() }
# A real Linux dash first: a container of ubuntu:24.04, whose /bin/sh is dash. Else any dash on the PATH (an msys dash makes
# `ln -s` a copy, so the linked-folder checks are skipped there).
$docker = $null
if (Get-Command docker -CommandType Application -ErrorAction SilentlyContinue) {
    & docker image inspect ubuntu:24.04 *> $null
    if ($LASTEXITCODE -eq 0) { $docker = 'ubuntu:24.04' }
}
$dash = if ($docker) { $null } else { (Get-Command dash -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source }
# Run-Dash runs a script as `sh -s` on dash. In the container the script rides in an environment variable and the output is read
# back with `docker logs`: a foreground `docker run` of some hosts loses stdout when it is not a terminal. No folder is mounted.
function Run-Dash { param([string] $script)
    $s = $script -replace "`r", ''
    if ($dash) { return @(($s | & $dash -s 2>&1) | ForEach-Object { "$_" }) }
    $b = [Convert]::ToBase64String([Text.UTF8Encoding]::new($false).GetBytes($s))
    $id = (& docker run -d -e "ATRIUM_TEST_SCRIPT=$b" $docker sh -c 'printf %s "$ATRIUM_TEST_SCRIPT" | base64 -d | sh -s 2>&1' | Select-Object -First 1)
    [void](& docker wait $id)
    $o = & docker logs $id 2>&1
    [void](& docker rm -f $id)
    @($o | ForEach-Object { "$_".TrimEnd("`r") })
}
if ($dash -or $docker) {
    $via = if ($docker) { 'dash in docker' } else { 'dash' }
    $kvOf = { param($lines) $h = @{}; foreach ($l in $lines) { $i = $l.IndexOf('='); if ($i -gt 0) { $h[$l.Substring(0, $i)] = $l.Substring($i + 1) } }; $h }
    $dl = Get-WorkLayout '/tmp/dashwork'
    $setup = "rm -rf /tmp/dashwork /tmp/dashhome; mkdir -p /tmp/dashhome; export HOME=/tmp/dashhome XDG_CONFIG_HOME=`n"
    $all = $setup + (Get-WorkProbeScript 'linux' '/tmp/dashwork' $false) + "`necho ===PROBE-CHECK-DONE`n" +
        (Get-WorkCacheScript 'linux' $dl $false) + "`necho ===CACHE-CHECK-DONE`nfind /tmp/dashhome -mindepth 1 | wc -l; ls /tmp/dashwork 2>&1 | head -1`necho ===NOTHING-WRITTEN`n" +
        (Get-WorkProbeScript 'linux' '/tmp/dashwork' $true) + "`necho ===PROBE-DONE`n" +
        (Get-WorkCacheScript 'linux' $dl $true) + "`necho ===CACHE-DONE`n" + (Get-WorkCacheScript 'linux' $dl $true) + "`necho ===CACHE-AGAIN-DONE`n"
    $o = Run-Dash $all
    $text = $o -join "`n"
    $seg = { param($from, $to) $a = $text.IndexOf($from); $b = $text.IndexOf($to); if ($a -lt 0 -or $b -lt 0) { @() } else { @($text.Substring($a + $from.Length, $b - $a - $from.Length) -split "`n" | Where-Object { $_ }) } }
    $pc = & $seg '' '===PROBE-CHECK-DONE'
    $cc = & $seg '===PROBE-CHECK-DONE' '===CACHE-CHECK-DONE'
    $nw = & $seg '===CACHE-CHECK-DONE' '===NOTHING-WRITTEN'
    $c1 = & $kvOf (& $seg '===PROBE-DONE' '===CACHE-DONE')
    $c2 = & $kvOf (& $seg '===CACHE-DONE' '===CACHE-AGAIN-DONE')
    $pk = & $kvOf $pc
    Check "$via -Check probe: runs, every parent examinable, nothing made" @($pk.parents, $pk.parent0, $pk.parent1, $pk.exists) @('2', 'ok', 'ok', 'False')
    $ck = & $kvOf $cc
    Check "$via -Check cache: todo for each, no syntax error" @($ck.dirs, $ck.npm, $ck.gomod, $ck.pip, $ck.cargo, [bool]($text -match 'Syntax error|not found')) @('todo 10', 'todo', 'todo', 'todo', 'todo', $false)
    Check "$via -Check wrote nothing: the home has no files and the work root was not made" @($nw[0], [bool]($nw[1] -like '*No such file*')) @('0', $true)
    Check "$via run: every cache done" @($c1.dirs, $c1.npm, $c1.gomod, $c1.gocache, $c1.pip, $c1.cargo) @('made 9', 'done', 'done', 'done', 'done', 'done')
    Check "$via rerun: everything ok" @($c2.dirs, $c2.npm, $c2.gomod, $c2.gocache, $c2.pip, $c2.cargo) @('ok', 'ok', 'ok', 'ok', 'ok', 'ok')
} else { Write-Host 'skip dash: neither dash nor a docker image ubuntu:24.04 is on this machine' }

# ── the verdict on what the caches printed ──────────────────────────────────

$lay = Get-WorkLayout 'V:\localai'
$kvLines = @('dirs=ok', 'npm=ok', 'gomod=ok', 'gocache=ok', 'pip=ok', 'cargo=ok')
$vs = @(Get-WorkCacheVerdict $kvLines $lay $true)
Check 'cache verdict: all in place is ok' @($vs.Status) @('ok', 'ok')
$vs = @(Get-WorkCacheVerdict ($kvLines + 'npmtool=C:/Users/x/AppData/Local/npm-cache') $lay $true)
Check 'cache verdict: a tool that says another folder is a warn naming it' @($vs[1].Status, [bool]($vs[1].Detail -like '*npm says C:/Users/x/AppData/Local/npm-cache*GOENV*')) @('warn', $true)
$vs = @(Get-WorkCacheVerdict ($kvLines + 'npmtool=V:\localai\cache\npm') $lay $true)
Check 'cache verdict: a tool that agrees, in either slash, is fine' $vs[1].Status 'ok'
$vs = @(Get-WorkCacheVerdict @('dirs=todo 9', 'npm=todo', 'gomod=ok', 'gocache=ok', 'pip=ok', 'cargo=todo') $lay $false)
Check 'cache verdict: todo is a warn that names the ones not done' @($vs[0].Status, $vs[1].Status, [bool]($vs[1].Detail -like 'npm, cargo not yet*')) @('warn', 'warn', $true)
Check 'cache verdict: an answer with a cache missing is a fail' (@(Get-WorkCacheVerdict @('dirs=ok', 'npm=ok') $lay $true))[0].Status 'fail'
$vs = @(Get-WorkCacheVerdict @('dirs=made 10', 'npm=done', 'gomod=done', 'gocache=done', 'pip=done', 'cargo=done') $lay $true)
Check 'cache verdict: made and set is done' @($vs.Status) @('done', 'done')

# ── Invoke-WorkRoot, with a fake remote ─────────────────────────────────────

$script:said = @()
$fake = {
    param($s)
    $script:said += $s
    if ($script:fakeProbe -and $s -like '*$parents*') { return [pscustomobject]@{ Out = $script:fakeProbe; Code = 0 } }
    [pscustomobject]@{ Out = $script:fakeCache; Code = 0 }
}
$script:fakeProbe = @('login=SG3\localai', 'drive=True', 'parents=1', 'parent0=fail', 'exists=False', 'made=False')
$script:fakeCache = $kvLines
$r = Invoke-WorkRoot $fake 'windows' 'V:\localai' $true 'sg3'
Check 'flow: an unexaminable parent stops at 13 before any cache is touched' @($r.Code, $r.Steps.Count, $script:said.Count, $r.AdminLines.Count) @(13, 1, 1, 3)
$script:said = @()
$script:fakeProbe = @('login=SG3\localai', 'drive=True', 'parents=1', 'parent0=ok', 'exists=True', 'writable=True')
$r = Invoke-WorkRoot $fake 'windows' 'V:\localai' $true 'sg3'
Check 'flow: a sound root goes on to the folders and caches' @($r.Code, @($r.Steps.Step), $script:said.Count) @(0, @('work-root', 'work-dirs', 'work-cache'), 2)

# ── the hint ────────────────────────────────────────────────────────────────

Check 'hint: a big second drive is named, with the flag to pass' ([bool](Get-WorkHintDetail @('drive=V:\|820') -like '*V:\ with 820 GB free*-WorkRoot V:\localai*')) $true
Check 'hint: a small drive says nothing' (Get-WorkHintDetail @('drive=E:\|8')) $null
Check 'hint: no drive says nothing' (Get-WorkHintDetail @()) $null
Check 'hint: the roomiest of two' ([bool](Get-WorkHintDetail @('drive=E:\|60', 'drive=V:\|820') -like '*V:\*')) $true

# ── the agent pack ──────────────────────────────────────────────────────────

$claude = Join-Path $tmp 'dot/claude'
New-Item -ItemType Directory -Force -Path (Join-Path $claude 'agents'), (Join-Path $claude 'skills/afk/scripts'), (Join-Path $claude 'skills/recap'), (Join-Path $claude 'skills/notaskill'), (Join-Path $claude 'hooks') | Out-Null
foreach ($n in 'c-systems-reviewer', 'go-security-reviewer', 'functional-tester', 'nonfunctional-tester', 'persona') { Set-Content -LiteralPath (Join-Path $claude "agents/$n.md") -Value "agent $n" }
Set-Content -LiteralPath (Join-Path $claude 'agents/README.txt') -Value 'not an agent'
Set-Content -LiteralPath (Join-Path $claude 'skills/afk/SKILL.md') -Value 'afk'
Set-Content -LiteralPath (Join-Path $claude 'skills/afk/scripts/snap.sh') -Value 'echo snap'
Set-Content -LiteralPath (Join-Path $claude 'skills/recap/SKILL.md') -Value 'recap'
Set-Content -LiteralPath (Join-Path $claude 'skills/notaskill/notes.txt') -Value 'no SKILL.md here'
Set-Content -LiteralPath (Join-Path $claude 'hooks/x.ps1') -Value 'a hook is not in the pack'
$pf = Get-AgentPackFiles $claude
Check 'pack files: agents, then each skill with a SKILL.md and everything under it' @($pf.Files) @('agents/c-systems-reviewer.md', 'agents/functional-tester.md', 'agents/go-security-reviewer.md', 'agents/nonfunctional-tester.md', 'agents/persona.md', 'skills/afk/SKILL.md', 'skills/afk/scripts/snap.sh', 'skills/recap/SKILL.md')
Check 'pack files: nothing else of claude/ is taken' (@($pf.Files | Where-Object { $_ -match 'hooks|README|notaskill' }).Count) 0
$meta = New-AgentPackMeta $claude $pf.Files 'dovholuknf/dotfiles' 'main' ('a' * 40)
Check 'pack meta: the agents, the skills and the commit' @($meta.agents.Count, @($meta.skills), $meta.commit, $meta.repo) @(5, @('afk', 'recap'), ('a' * 40), 'dovholuknf/dotfiles')
Check 'pack meta: every file has a 64 hex SHA-256 that is the file''s own' @(@($meta.files.PSObject.Properties | Where-Object { $_.Value -match '^[0-9a-f]{64}$' }).Count, [bool]($meta.files.'agents/persona.md' -eq (Get-FileHash -LiteralPath (Join-Path $claude 'agents/persona.md') -Algorithm SHA256).Hash.ToLower())) @(8, $true)
if ($isWin) {
    $lnk = Join-Path $claude 'skills/debug-ziti'
    $tgt = Join-Path $tmp 'elsewhere'; New-Item -ItemType Directory -Force -Path $tgt | Out-Null; Set-Content -LiteralPath (Join-Path $tgt 'SKILL.md') -Value 'x'
    $made = $false; try { New-Item -ItemType SymbolicLink -Path $lnk -Target $tgt -ErrorAction Stop | Out-Null; $made = $true } catch { }
    if ($made) {
        $pf2 = Get-AgentPackFiles $claude
        Check 'pack files: a skill that is a link into another repository is skipped and named, not fetched' @(@($pf2.Files | Where-Object { $_ -like '*debug-ziti*' }).Count, [bool](@($pf2.Skipped) -like 'skills/debug-ziti (a link to *')) @(0, $true)
        Remove-Item -LiteralPath $lnk -Force
    } else { Write-Host 'skip the symlinked skill: this account may not make a symlink' }
}

# The install script, run for real on Windows: pack -> tar -> unpack into a temp home, then again.
if ($isWin -and (Get-Command tar -ErrorAction SilentlyContinue)) {
    $stage = Join-Path $tmp 'stage'
    foreach ($rel in $pf.Files) { $to = Join-Path $stage $rel; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $to) | Out-Null; Copy-Item -LiteralPath (Join-Path $claude $rel) -Destination $to }
    [IO.File]::WriteAllText((Join-Path $stage '.atrium-pack.json'), ($meta | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
    $ph = Join-Path $tmp 'phome'; $P = Join-Path $ph '.atrium\provision'
    New-Item -ItemType Directory -Force -Path $P, (Join-Path $ph '.claude\agents') | Out-Null
    Set-Content -LiteralPath (Join-Path $ph '.claude\agents\persona.md') -Value 'an older persona'
    Set-Content -LiteralPath (Join-Path $ph '.claude\agents\mine.md') -Value 'the room owner''s own agent'
    function Install-Pack { param([bool] $make)
        $t = Join-Path $P 'agent-pack.tgz'; if (Test-Path $t) { Remove-Item $t -Force }
        & (Get-PackTar) -czf $t -C $stage . | Out-Null
        $s = "`$P = '$P'`n" + (Get-AgentPackInstallScript 'windows' $make)
        $f = Join-Path $tmp "inst-$([guid]::NewGuid().ToString('N').Substring(0, 6)).ps1"
        Set-Content -LiteralPath $f -Value $s -Encoding UTF8
        $o = & pwsh -NoProfile -Command "`$env:HOME = '$ph'; `$env:USERPROFILE = '$ph'; & '$f'" 2>&1
        $kv = @{}; foreach ($l in @($o | ForEach-Object { "$_" })) { $i = $l.IndexOf('='); if ($i -gt 0) { $kv[$l.Substring(0, $i)] = $l.Substring($i + 1) } }
        $kv
    }
    $kv = Install-Pack $false
    Check 'install -Check: 8 files, 8 would change, nothing written' @($kv.files, $kv.changed, (Test-Path (Join-Path $ph '.claude\skills')), (Test-Path (Join-Path $ph '.claude\atrium-agent-pack.json'))) @('8', '8', $false, $false)
    $kv = Install-Pack $true
    Check 'install: 8 changed, record written' @($kv.files, $kv.changed, $kv.record, $kv.commit) @('8', '8', 'written', ('a' * 40))
    Check 'install: the files are real files with the pack''s content' @((Get-Content -LiteralPath (Join-Path $ph '.claude\agents\persona.md')), (Get-Content -LiteralPath (Join-Path $ph '.claude\skills\afk\scripts\snap.sh')), [bool](Get-Item -LiteralPath (Join-Path $ph '.claude\skills\afk\SKILL.md')).LinkType) @('agent persona', 'echo snap', $false)
    Check 'install: an agent that is not in the pack is left alone' (Get-Content -LiteralPath (Join-Path $ph '.claude\agents\mine.md')) 'the room owner''s own agent'
    $rec = Get-Content -LiteralPath (Join-Path $ph '.claude\atrium-agent-pack.json') -Raw | ConvertFrom-Json
    Check 'install: the record names the commit, the repo and when' @($rec.commit, $rec.repo, [bool]$rec.installed_at) @(('a' * 40), 'dovholuknf/dotfiles', $true)
    $b = (Get-Item (Join-Path $ph '.claude\atrium-agent-pack.json')).LastWriteTimeUtc.Ticks
    $kv = Install-Pack $true
    Check 'install again: nothing changed, no record written' @($kv.changed, $kv.record, ((Get-Item (Join-Path $ph '.claude\atrium-agent-pack.json')).LastWriteTimeUtc.Ticks -eq $b)) @('0', $null, $true)
    Set-Content -LiteralPath (Join-Path $ph '.claude\agents\functional-tester.md') -Value 'edited on the room'
    $kv = Install-Pack $true
    Check 'install: a file edited on the room is put back, and only that one' @($kv.changed, (Get-Content -LiteralPath (Join-Path $ph '.claude\agents\functional-tester.md'))) @('1', 'agent functional-tester')
    $state = & pwsh -NoProfile -Command "`$env:USERPROFILE = '$ph'; & { $(Get-AgentPackStateScript 'windows') }" 2>&1
    $vd = Get-AgentPackVerdict @($state | ForEach-Object { "$_" }) ('a' * 40)
    Check 'state: read back, the pack is current and the panel agents are there' @($vd.Status, [bool]($vd.Detail -like 'the agent pack at aaaaaaaaa, 6 agents and 2 skills*')) @('ok', $true)
}

if ($dash -or $docker) {
    # the agent pack, installed on dash: pack files in, a linked parent refused, an edited agent said and replaced
    $pstage = Join-Path $tmp 'dstage'
    foreach ($rel in $pf.Files) { $to = Join-Path $pstage $rel; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $to) | Out-Null; Copy-Item -LiteralPath (Join-Path $claude $rel) -Destination $to }
    $pmeta = New-AgentPackMeta $claude $pf.Files 'dovholuknf/dotfiles' 'main' ('d' * 40)
    [IO.File]::WriteAllText((Join-Path $pstage '.atrium-pack.json'), ($pmeta | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
    $ptgz = Join-Path $tmp 'dpack.tgz'
    & (Get-PackTar) -czf $ptgz -C $pstage . | Out-Null
    $b64 = [Convert]::ToBase64String([IO.File]::ReadAllBytes($ptgz))
    $inst = { param($make, $pre)
        "export HOME=/tmp/phome P=/tmp/pwork; $pre`nmkdir -p `"`$P`"; printf %s '$b64' | base64 -d > `"`$P/agent-pack.tgz`"`n" + (Get-AgentPackInstallScript 'linux' $make) }
    $reset = 'rm -rf /tmp/phome /tmp/pwork /tmp/outside; mkdir -p /tmp/phome/.claude/agents'
    $o = Run-Dash (& $inst $false "$reset; echo 'an older persona' > /tmp/phome/.claude/agents/persona.md")
    $k = & $kvOf $o
    Check "$via pack -Check: 8 files, 8 would change, the older persona is named as edited, nothing refused" @($k.files, $k.changed, $k.edited, $k.edited_files, $k.refused, $k.record) @('8', '8', '1', 'agents/persona.md', '0', $null)
    $o = Run-Dash (& $inst $true "$reset; echo 'an older persona' > /tmp/phome/.claude/agents/persona.md")
    $k = & $kvOf $o
    Check "$via pack: written, the record is there, and the edited file is the pack's now" @($k.changed, $k.record, $k.edited, [bool]($o -match 'Syntax error')) @('8', 'written', '1', $false)
    $o = Run-Dash ((& $inst $true "$reset") + "`ncat /tmp/phome/.claude/agents/persona.md; ls /tmp/phome/.claude/skills/afk/scripts")
    Check "$via pack: the files are real and have the pack's content" @(($o -contains 'agent persona'), ($o -contains 'snap.sh')) @($true, $true)
    # a second install over the first: a file the LAST PACK wrote and nobody touched is an update, not an edit
    $two = (& $inst $true $reset) + "`necho ===SECOND`n" + (& $inst $true 'true')
    $o = Run-Dash $two
    $k2 = & $kvOf @($o | Select-Object -Skip ([array]::IndexOf($o, '===SECOND') + 1))
    Check "$via pack again: nothing changed, nothing edited, no record rewritten" @($k2.changed, $k2.edited, $k2.refused, $k2.record) @('0', '0', '0', $null)
    $edit = (& $inst $true $reset) + "`necho 'mine' > /tmp/phome/.claude/agents/functional-tester.md`necho ===SECOND`n" + (& $inst $true 'true')
    $o = Run-Dash $edit
    $k3 = & $kvOf @($o | Select-Object -Skip ([array]::IndexOf($o, '===SECOND') + 1))
    Check "$via pack: an agent edited since the last pack is named, and put back" @($k3.changed, $k3.edited, $k3.edited_files) @('1', '1', 'agents/functional-tester.md')
    if ($docker) {
    $link = "$reset; mkdir -p /tmp/outside; ln -s /tmp/outside /tmp/phome/.claude/skills"
    $o = Run-Dash ((& $inst $true $link) + "`nls /tmp/outside | wc -l; ls /tmp/phome/.claude/agents | wc -l; ls /tmp/phome/.claude/atrium-agent-pack.json 2>&1 | head -1")
    $k4 = & $kvOf $o
    Check "$via pack: a linked skills folder is refused, nothing goes through it, the agents still install, and no record claims the pack current" @($k4.refused, [bool]($k4.refused_files -like '*skills/afk/SKILL.md (/tmp/phome/.claude/skills is a link)*'), $k4.record, $o[-3], [bool]($o[-1] -like '*No such file*')) @('3', $true, $null, '0', $true)
    } else { Write-Host 'skip the linked-folder check on dash: ln -s needs a real linux (docker)' }
}

# ── the verdict on the pack ─────────────────────────────────────────────────

$cur = @(('commit=' + ('b' * 40)), 'installed_at=2026-10-08T00:00:00Z', 'agents=c-systems-reviewer,go-security-reviewer,functional-tester,nonfunctional-tester,persona', 'skills=afk,recap')
Check 'pack verdict: current is ok' (Get-AgentPackVerdict $cur ('b' * 40)).Status 'ok'
$vd = Get-AgentPackVerdict $cur ('c' * 40)
Check 'pack verdict: a newer commit on the hub is stale, naming both' @($vd.Status, [bool]($vd.Detail -like 'the agent pack is stale: bbbbbbbbb is installed and the hub*ccccccccc*')) @('warn', $true)
$vd = Get-AgentPackVerdict @('record=missing', 'agents=', 'skills=') ('b' * 40)
Check 'pack verdict: no record is a warn that says to install it, and names the panel agents' @($vd.Status, [bool]($vd.Detail -like 'the agent pack is not installed.*c-systems-reviewer*go-security-reviewer*')) @('warn', $true)
$vd = Get-AgentPackVerdict @(('commit=' + ('b' * 40)), 'agents=persona,c-systems-reviewer', 'skills=afk') ('b' * 40)
Check 'pack verdict: an agent the panel names that is missing is a warn naming it' @($vd.Status, [bool]($vd.Detail -like 'the agents go-security-reviewer, functional-tester, nonfunctional-tester that the review panel names are missing*')) @('warn', $true)
$vd = Get-AgentPackVerdict $cur $null
Check 'pack verdict: a hub that could not be asked is ok and says so' @($vd.Status, [bool]($vd.Detail -like '*could not be asked*')) @('ok', $true)
Check 'pack verdict: needing nothing in particular, a record and no agents is ok' (Get-AgentPackVerdict @(('commit=' + ('b' * 40)), 'agents=', 'skills=') ('b' * 40) @()).Status 'ok'

# THE PANEL'S AGENTS ARE THE STORE'S. DefaultPRPanel in internal/store/prs.go names them, and this list must be the same.
$prs = Join-Path (Split-Path -Parent $PSScriptRoot) 'internal/store/prs.go'
if (Test-Path -LiteralPath $prs) {
    $text = Get-Content -LiteralPath $prs -Raw
    $m = [regex]::Match($text, 'const DefaultPRPanel = (?<body>(?:`[^`]*`\s*\+?\s*)+)')
    $names = @([regex]::Matches($m.Groups['body'].Value, '"agent":"([^"]+)"') | ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique)
    Check 'panel agents: the same as DefaultPRPanel in the store' @($script:PanelAgents | Sort-Object) @($names | Sort-Object)
} else { Write-Host 'skip panel agents: internal/store/prs.go is not beside these scripts' }

# ── the hub mirror ──────────────────────────────────────────────────────────

Check 'mirror url: the host is github, as in the hub''s own git urls' (Get-HubMirrorUrl '127.0.0.1:7778' 'dovholuknf/dotfiles') 'http://127.0.0.1:7778/git/hub/github/dovholuknf/dotfiles.git'
$env:GIT_TERMINAL_PROMPT = $null
$null = Get-HubMirrorCommit '127.0.0.1:1' 'o/r' 'main'
Check 'git env: the operator''s session has no GIT_TERMINAL_PROMPT left behind' ([bool]$env:GIT_TERMINAL_PROMPT) $false
$env:GIT_TERMINAL_PROMPT = '1'
$null = Get-HubMirrorCommit '127.0.0.1:1' 'o/r' 'main'
Check 'git env: and one the operator set is put back as it was' $env:GIT_TERMINAL_PROMPT '1'
$env:GIT_TERMINAL_PROMPT = $null
$np = New-AgentPack -HubAddr '127.0.0.1:1' -Repo 'o/r' -Branch '--upload-pack=x' -OutDir (Join-Path $tmp 'np')
Check 'pack: a branch that looks like an option is refused before git is run' @($np.Ok, [bool]($np.Why -like '*AgentPackBranch*'), (Test-Path (Join-Path $tmp 'np'))) @($false, $true, $false)
Check 'mirror url: slashes around the repo are dropped' (Get-HubMirrorUrl 'h:1' '/o/r/') 'http://h:1/git/hub/github/o/r.git'

# ── the wiring: provision-room.ps1 and room-check.ps1 ───────────────────────

foreach ($n in 'provision-room.ps1', 'room-check.ps1') {
    $errs = $null; [void][Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $n), [ref]$null, [ref]$errs)
    Check "parse: $n" @($errs).Count 0
}
$pr = Join-Path $PSScriptRoot 'provision-room.ps1'
function Run-Pr { param([string[]] $a) $o = & pwsh -NoProfile -File $pr 'x@nowhere' @a 2>&1; [pscustomobject]@{ Out = @($o | ForEach-Object { "$_" }); Code = $LASTEXITCODE } }
foreach ($c in @(
        @('a relative folder', @('-WorkRoot', 'work\localai'), 'is not an absolute path'),
        @('a drive root', @('-WorkRoot', 'V:\'), 'filesystem root'),
        @('a UNC path', @('-WorkRoot', '\\h\s\localai'), 'network path'),
        @('a semicolon', @('-WorkRoot', 'V:\a;b'), 'semicolon'),
        @('-Remove', @('-WorkRoot', 'V:\localai', '-Remove'), 'belong to a provision run'),
        @('-Restart', @('-WorkRoot', 'V:\localai', '-Restart'), 'belong to a provision run'),
        @('-SmokeOnly', @('-WorkRoot', 'V:\localai', '-SmokeOnly'), 'belong to a provision run'),
        @('-NoAgentPack with -Remove', @('-NoAgentPack', '-Remove'), 'belong to a provision run'),
        @('-NoSharedFolder', @('-WorkRoot', 'V:\localai', '-NoSharedFolder'), 'takes the place of the shared folder'))) {
    $r = Run-Pr $c[1]
    Check "provision: -WorkRoot, $($c[0]) is exit 1 and says why" @($r.Code, [bool](@($r.Out | Where-Object { $_ -like "provision args fail*$($c[2])*" }).Count)) @(1, $true)
}
$r = Run-Pr @('-WorkRoot', 'V:\localai', '-Check')
Check 'provision: a good -WorkRoot passes the argument checks and fails at ssh (exit 2)' $r.Code 2
$text = Get-Content -LiteralPath $pr -Raw
Check 'provision: the work root is wired in, with exit 13 and the agent pack' @([bool]($text -match 'Invoke-WorkRoot'), [bool]($text -match 'Finish \$wr\.Code'), [bool]($text -match 'Invoke-AgentPack'), [bool]($text -match '#  13  ')) @($true, $true, $true, $true)
$rc = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'room-check.ps1') -Raw
Check 'room-check: the work-root and agent-pack rows are wired in' @([bool]($rc -match "Row 'agent-pack'"), [bool]($rc -match 'Invoke-WorkRoot')) @($true, $true)

Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
Write-Host ''
if ($script:failed) { Write-Host "$script:failed of $script:ran checks failed."; exit 1 }
Write-Host "all $script:ran checks passed."
