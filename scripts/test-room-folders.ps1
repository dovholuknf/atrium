# Tests for scripts/room-folders.ps1 and for how provision-room.ps1 takes -AllowedFolders. Needs no ssh, no hub and no
# room:
#
#   pwsh -NoProfile -File scripts/test-room-folders.ps1
#
# The text helpers are called directly. The script that runs the verb on the remote is BUILT the way provision builds
# it and then RUN here, locally, against a fake `atrium` that prints what the contract says the verb prints (or says
# unknown command, or is not there), once as sh and once as PowerShell, so the quoting of a folder with a space or a
# quote in it is really exercised. What this cannot run: ssh to a real room, a Windows host (the PowerShell form is run
# by pwsh here), and the step's own flow (Invoke-AllowedFolders, the smoke), which need a hub and a room.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'room-folders.ps1')

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

# ── Test-FolderArg ──────────────────────────────────────────────────────────

Check 'arg: unix absolute' (Test-FolderArg '/srv/work') $null
Check 'arg: windows drive, forward slash' (Test-FolderArg 'C:/work') $null
Check 'arg: windows drive, back slash' (Test-FolderArg 'C:\Users\me\work') $null
Check 'arg: tilde' (Test-FolderArg '~/work') $null
Check 'arg: with a space' (Test-FolderArg '/srv/my work') $null
Check 'arg: relative is refused' ([bool] (Test-FolderArg 'work/here')) $true
Check 'arg: dot is refused' ([bool] (Test-FolderArg '.')) $true
Check 'arg: empty is refused' ([bool] (Test-FolderArg '')) $true
Check 'arg: semicolon is refused' ([bool] (Test-FolderArg '/srv/a;/srv/b')) $true
Check 'arg: newline is refused' ([bool] (Test-FolderArg "/srv/a`n/srv/b")) $true
Check 'arg: unix root is refused' ([bool] (Test-FolderArg '/')) $true
Check 'arg: windows root is refused' ([bool] (Test-FolderArg 'C:\')) $true
Check 'arg: bare drive is refused' ([bool] (Test-FolderArg 'D:')) $true

# ── Expand-FolderArg and Get-DefaultFolders ─────────────────────────────────

Check 'expand: tilde alone' (Expand-FolderArg '~' '/home/u') '/home/u'
Check 'expand: tilde slash' (Expand-FolderArg '~/work' '/home/u') '/home/u/work'
Check 'expand: windows home' (Expand-FolderArg '~\work' 'C:/Users/u') 'C:/Users/u/work'
Check 'expand: absolute is left alone' (Expand-FolderArg '/srv/work' '/home/u') '/srv/work'
Check 'default: clone, worktrees, root' (Get-DefaultFolders '/h/git/github/o/r' '/h/wt') @('/h/git/github/o/r', '/h/git/github/o/r-worktrees', '/h/wt')
Check 'default: no WORKTREE_ROOT' (Get-DefaultFolders 'C:/Users/u/git/github/o/r' $null) @('C:/Users/u/git/github/o/r', 'C:/Users/u/git/github/o/r-worktrees')
Check 'default: only WORKTREE_ROOT' (Get-DefaultFolders $null '/h/wt') @('/h/wt')
Check 'default: none' @(Get-DefaultFolders $null $null).Count 0
Check 'default: no duplicate' @(Get-DefaultFolders '/h/c' '/h/c').Count 2

# ── what the verb prints ────────────────────────────────────────────────────

$allow = @(ConvertFrom-FolderAllow @('allowed /srv/a', 'trusted /srv/a', 'skipped /root/h: is the home directory', 'note: something', 'trusted /srv/my work'))
Check 'allow: lines read' $allow.Count 4
Check 'allow: kinds' ($allow | ForEach-Object { $_.Kind }) @('allowed', 'trusted', 'skipped', 'trusted')
Check 'allow: a dir with a space' $allow[3].Dir '/srv/my work'
Check 'allow: skipped has its reason' $allow[2].Reason 'is the home directory'
Check 'allow: skipped has its dir' $allow[2].Dir '/root/h'
Check 'allow: nothing readable' @(ConvertFrom-FolderAllow @('Error: boom')).Count 0

$on = ConvertFrom-FolderList @('{"roots":["/srv/a","/srv/b"],"enforced":true}')
Check 'list: enforced' $on.Enforced $true
Check 'list: roots' $on.Roots @('/srv/a', '/srv/b')
$off = ConvertFrom-FolderList @('{"roots":[],"enforced":false}')
Check 'list: not enforced' $off.Enforced $false
Check 'list: empty ok' $off.Ok $true
Check 'list: noise around the json' (ConvertFrom-FolderList @('warning: x', '{"roots":["/a"],"enforced":true}', 'bye')).Roots '/a'
Check 'list: not json is not ok' (ConvertFrom-FolderList @('Error: unknown flag: --json')).Ok $false
Check 'list: broken json is not ok' (ConvertFrom-FolderList @('{"roots": [')).Ok $false

# ── verb missing and the gate's refusal ─────────────────────────────────────

Check 'missing: unknown command' (Test-FolderVerbMissing 1 @('Error: unknown command "folders" for "atrium room"')) $true
Check 'missing: unknown flag on list --json' (Test-FolderVerbMissing 1 @('Error: unknown flag: --json')) $true
Check 'missing: no binary' (Test-FolderVerbMissing 127 @('atrium is not at /h/.local/bin/atrium')) $true
Check 'missing: not when it worked' (Test-FolderVerbMissing 0 @('unknown command')) $false
Check 'missing: not when the room did not answer' (Test-FolderVerbMissing 1 @('dial tcp 127.0.0.1:7781: connection refused')) $false

Check 'refusal: names the allowed folders' (Test-FolderRefusal '{"error":"/h is not in this room''s allowed folders (/srv/a)"}' @('/srv/a')) $true
Check 'refusal: names a root' (Test-FolderRefusal 'nope, only /srv/a' @('/srv/a')) $true
Check 'refusal: some other 400' (Test-FolderRefusal '{"error":"no such directory"}' @('/srv/a')) $false

# ── under the list ──────────────────────────────────────────────────────────

Check 'under: the root itself' (Test-UnderFolders '/srv/a' @('/srv/a')) $true
Check 'under: inside' (Test-UnderFolders '/srv/a/b/c' @('/srv/a')) $true
Check 'under: a sibling with the same prefix is not' (Test-UnderFolders '/srv/ab' @('/srv/a')) $false
Check 'under: the parent is not' (Test-UnderFolders '/srv' @('/srv/a')) $false
Check 'under: trailing slash on both' (Test-UnderFolders '/srv/a/' @('/srv/a/')) $true
Check 'under: windows slashes and case' (Test-UnderFolders 'c:\Work\Repo' @('C:/work') $true) $true
Check 'under: case matters on linux' (Test-UnderFolders '/Srv/a' @('/srv/a') $false) $false
Check 'under: any of several' (Test-UnderFolders '/srv/b/x' @('/srv/a', '/srv/b')) $true
Check 'under: no roots' (Test-UnderFolders '/srv/a' @()) $false

# ── the remote script, run for real against a fake atrium ───────────────────

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("room-folders-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force $tmp | Out-Null
try {
    $weird = @("/srv/plain", "/srv/my work", "/srv/it's", 'C:\Users\me\a b')

    # THE FAKE VERB. `list --json` prints the contract's JSON, and `allow` prints one `allowed` and one `trusted` line per
    # dir, or `skipped <dir>: is the home directory` for a dir named home, and exits 1 then. The args it got are echoed
    # to a file, one per line, so the test can see what survived the quoting.
    $argsFile = Join-Path $tmp 'args.txt'
    $fakeSh = Join-Path $tmp 'atrium-ok'
    Set-Content -LiteralPath $fakeSh -NoNewline -Value @"
#!/bin/sh
: > '$argsFile'
for a in "`$@"; do printf '%s\n' "`$a" >> '$argsFile'; done
[ "`$1 `$2" = "room folders" ] || { echo "Error: unknown command" >&2; exit 1; }
if [ "`$3" = list ]; then echo '{"roots":["/srv/plain"],"enforced":true}'; exit 0; fi
rc=0
shift 3
for d in "`$@"; do
  case "`$d" in */home) printf '%s\n' "skipped `$d: is the home directory"; rc=1;; *) printf '%s\n' "allowed `$d" "trusted `$d";; esac
done
exit `$rc
"@
    $fakeOld = Join-Path $tmp 'atrium-old'
    Set-Content -LiteralPath $fakeOld -NoNewline -Value "#!/bin/sh`necho 'Error: unknown flag: --json' >&2`nexit 1`n"
    $unixOk = $IsWindows -ne $true -and (Get-Command sh -ErrorAction SilentlyContinue)
    if ($unixOk) {
        & chmod +x $fakeSh $fakeOld

        function Invoke-AsSh {
            param([string] $script)
            $out = ($script -replace "`r", '') + "`n#" | & sh -s 2>&1
            [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
        }

        $r = Invoke-AsSh (Get-FolderScript 'linux' (@('allow') + $weird) $fakeSh)
        Check 'sh: exit code is the verb''s' $r.Code 0
        Check 'sh: prints the verb''s lines' ($r.Out | Where-Object { $_ -like 'allowed *' }) @('allowed /srv/plain', 'allowed /srv/my work', "allowed /srv/it's", 'allowed C:\Users\me\a b')
        Check 'sh: every dir arrives whole' (Get-Content -LiteralPath $argsFile) (@('room', 'folders', 'allow') + $weird)

        $r = Invoke-AsSh (Get-FolderScript 'darwin' @('allow', '/srv/a', '/home/home') $fakeSh)
        Check 'sh: a skipped dir exits 1' $r.Code 1
        Check 'sh: and says why' ($r.Out | Where-Object { $_ -like 'skipped *' }) 'skipped /home/home: is the home directory'

        $r = Invoke-AsSh (Get-FolderScript 'linux' @('list', '--json') $fakeSh)
        Check 'sh: list prints the json' (ConvertFrom-FolderList $r.Out).Enforced $true

        $r = Invoke-AsSh (Get-FolderScript 'linux' @('list', '--json') $fakeOld)
        Check 'sh: an old binary exits non-zero' ($r.Code -ne 0) $true
        Check 'sh: and reads as missing' (Test-FolderVerbMissing $r.Code $r.Out) $true

        $r = Invoke-AsSh (Get-FolderScript 'linux' @('list', '--json') (Join-Path $tmp 'nothing-here'))
        Check 'sh: no binary is 127' $r.Code 127
        Check 'sh: and reads as missing too' (Test-FolderVerbMissing $r.Code $r.Out) $true

        # The default location is "$HOME/.local/bin/atrium": a fake home holding the fake there.
        $fh = Join-Path $tmp 'home'
        New-Item -ItemType Directory -Force (Join-Path $fh '.local/bin') | Out-Null
        Copy-Item $fakeSh (Join-Path $fh '.local/bin/atrium')
        & chmod +x (Join-Path $fh '.local/bin/atrium')
        $env:HOME = $fh
        $r = Invoke-AsSh (Get-FolderScript 'linux' @('list', '--json'))
        Check 'sh: the default binary is under HOME/.local/bin' (ConvertFrom-FolderList $r.Out).Ok $true
    } else {
        Write-Host 'skip sh: no sh here'
    }

    # THE POWERSHELL FORM, which a Windows room runs under Windows PowerShell, run here by pwsh. The fake is a .ps1.
    $fakePs = Join-Path $tmp 'atrium-ok.ps1'
    Set-Content -LiteralPath $fakePs -Value @"
Set-Content -LiteralPath '$argsFile' -Value `$args
if ("`$(`$args[0]) `$(`$args[1])" -ne 'room folders') { 'Error: unknown command'; exit 1 }
if (`$args[2] -eq 'list') { '{"roots":["/srv/plain"],"enforced":false}'; exit 0 }
foreach (`$d in `$args[3..(`$args.Count - 1)]) { "allowed `$d"; "trusted `$d" }
exit 0
"@
    $pw = (Get-Command pwsh).Source
    function Invoke-AsPs {
        param([string] $script)
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($script))
        $out = & $pw -NoProfile -NonInteractive -EncodedCommand $enc 2>&1
        [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
    }
    $r = Invoke-AsPs (Get-FolderScript 'windows' (@('allow') + $weird) $fakePs)
    Check 'ps: exit code is the verb''s' $r.Code 0
    Check 'ps: prints the verb''s lines' ($r.Out | Where-Object { $_ -like 'allowed *' }) @('allowed /srv/plain', 'allowed /srv/my work', "allowed /srv/it's", 'allowed C:\Users\me\a b')
    Check 'ps: every dir arrives whole' (Get-Content -LiteralPath $argsFile) (@('room', 'folders', 'allow') + $weird)
    $r = Invoke-AsPs (Get-FolderScript 'windows' @('list', '--json') $fakePs)
    Check 'ps: list reads as not enforced' (ConvertFrom-FolderList $r.Out).Enforced $false
    $r = Invoke-AsPs (Get-FolderScript 'windows' @('list', '--json') (Join-Path $tmp 'nothing-here.exe'))
    Check 'ps: no binary is 127' $r.Code 127
    Check 'ps: and reads as missing' (Test-FolderVerbMissing $r.Code $r.Out) $true
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# ── the step itself, Invoke-AllowedFolders, with the remote faked ───────────

# THE REAL FUNCTIONS, lifted out of provision-room.ps1 by parsing it, so what is tested is what ships. Invoke-Remote is
# replaced by one that runs the script on THIS machine under sh with a fake home, and Step, Save-Manifest and
# Get-StateDirs record instead of print. The fake atrium keeps its list in a file, so a second call sees the first.
$ast = [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'provision-room.ps1'), [ref]$null, [ref]$null)
$lift = 'Invoke-RoomFolders', 'Get-RoomFolderList', 'Get-FoldersCommand', 'Get-FolderRemoteHome', 'Invoke-AllowedFolders',
    'Quote-Sh', 'Quote-Ps', 'ConvertFrom-KeyValue'
foreach ($n in $lift) {
    $fn = $ast.Find({ param($x) $x -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $x.Name -eq $n }, $true)
    if (-not $fn) { throw "provision-room.ps1 has no function $n" }
    Invoke-Expression $fn.Extent.Text
}
if ($unixOk) {
    $sh2 = Join-Path ([IO.Path]::GetTempPath()) ("room-folders-step-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
    $fakeHome = Join-Path $sh2 'home'
    New-Item -ItemType Directory -Force (Join-Path $fakeHome '.local/bin') | Out-Null
    $stateFile = Join-Path $sh2 'roots.txt'
    $callsFile = Join-Path $sh2 'calls.txt'
    try {
        # A STATEFUL FAKE. `allow` appends each dir to the file and prints allowed and trusted, except a dir named home
        # or one that does not exist (a dir under /nonexistent), which are skipped, and exit 1 then. `list --json`
        # prints the file. MODE=old makes it an atrium without the verb.
        $fake = Join-Path $fakeHome '.local/bin/atrium'
        Set-Content -LiteralPath $fake -NoNewline -Value @"
#!/bin/sh
echo "`$*" >> '$callsFile'
if [ -f '$sh2/old' ]; then echo 'Error: unknown flag: --json' >&2; exit 1; fi
touch '$stateFile'
if [ "`$3" = list ]; then
  r=`$(sed 's/.*/"&"/' '$stateFile' | paste -sd, -)
  e=false; [ -s '$stateFile' ] && e=true
  printf '{"roots":[%s],"enforced":%s}\n' "`$r" "`$e"; exit 0
fi
rc=0
shift 3
for d in "`$@"; do
  case "`$d" in
    */home|/nonexistent/*) printf 'skipped %s: not allowed here\n' "`$d"; rc=1;;
    *) grep -qxF -- "`$d" '$stateFile' || printf '%s\n' "`$d" >> '$stateFile'; printf 'allowed %s\ntrusted %s\n' "`$d" "`$d";;
  esac
done
exit `$rc
"@
        & chmod +x $fake
        $env:HOME = $fakeHome

        function Invoke-Remote {
            param([string] $script)
            $pre = "A=`"`$HOME/.atrium`"; Bin=`"`$HOME/.local/bin/atrium`"`n"
            $out = ($pre + ($script -replace "`r", '') + "`n#") | & sh -s 2>&1
            [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
        }
        $script:steps = New-Object System.Collections.Generic.List[string]
        function Step { param([string] $step, [string] $status, [string] $detail = '') $script:steps.Add("$step $status $detail".Trim()) }
        $script:saved = 0
        function Save-Manifest { $script:saved++ }
        function Get-StateDirs { @{ wtr = '' } }
        $os = 'linux'; $Name = 'lab1'; $Target = 'u@lab1'; ${Ssh} = 'ssh'; $SshOption = @()
        function Reset-Case {
            Remove-Item -LiteralPath $stateFile, $callsFile, (Join-Path $sh2 'old') -Force -ErrorAction SilentlyContinue
            $script:roomFolders = $null; $script:steps.Clear(); $script:saved = 0
            $script:manifest = [pscustomobject]@{ name = 'lab1' }
        }
        $clonePath = "$sh2/git/github/o/r"
        New-Item -ItemType Directory -Force $clonePath | Out-Null

        # 1. a new provision, no -AllowedFolders: the clone and its worktrees folder, which is made.
        Reset-Case; $freshManifest = $true; $AllowedFolders = @()
        Invoke-AllowedFolders *>$null
        Check 'step: new provision allows the clone and its worktrees' (Get-Content -LiteralPath $stateFile) @($clonePath, "$clonePath-worktrees")
        Check 'step: the worktrees folder was made' (Test-Path -LiteralPath "$clonePath-worktrees") $true
        Check 'step: one folders line, done' @($script:steps | Where-Object { $_ -like 'folders *' }) ("folders done $clonePath, $clonePath-worktrees allowed on lab1, claude's folder trust written for 2 of 2")
        Check 'step: the manifest records them' $script:manifest.allowed_folders @($clonePath, "$clonePath-worktrees")
        Check 'step: and was saved' $script:saved 1

        # 2. the same again (a second call): nothing new, ok.
        $script:roomFolders = $null; $script:steps.Clear(); $script:saved = 0
        Invoke-AllowedFolders *>$null
        Check 'step: the same list again is ok' (@($script:steps)[0] -like 'folders ok *(all were already in the list)') $true
        Check 'step: and the manifest is not saved again' $script:saved 0

        # 3. a rerun on a room that was provisioned (not fresh) and no -AllowedFolders: skip, and the verb is never called.
        Reset-Case; $freshManifest = $false
        Invoke-AllowedFolders *>$null
        Check 'step: a rerun skips' (@($script:steps)[0] -like 'folders skip a rerun keeps the list as it is*') $true
        Check 'step: and runs nothing on the room' (Test-Path -LiteralPath $callsFile) $false

        # 4. a rerun WITH -AllowedFolders adds to the list, ~ expanded to the remote home, the manifest keeps both.
        Reset-Case; $freshManifest = $false
        $script:manifest | Add-Member -NotePropertyName allowed_folders -NotePropertyValue @('/srv/old') -Force
        $AllowedFolders = @('~/work', '/srv/b')
        Invoke-AllowedFolders *>$null
        Check 'step: explicit list reaches the verb, ~ expanded' (Get-Content -LiteralPath $stateFile) @("$fakeHome/work", '/srv/b')
        Check 'step: the manifest keeps the old folder and adds the new' $script:manifest.allowed_folders @('/srv/old', "$fakeHome/work", '/srv/b')
        Check 'step: done' (@($script:steps)[0] -like 'folders done *') $true

        # 5. a folder the room skips is a warn naming it, the rest are still allowed.
        Reset-Case; $freshManifest = $true; $AllowedFolders = @('/srv/a', '/nonexistent/x')
        Invoke-AllowedFolders *>$null
        Check 'step: a skipped dir is a warn that names it' (@($script:steps | Where-Object { $_ -like 'folders warn /nonexistent/x was skipped: not allowed here*' }).Count) 1
        Check 'step: the other is still done' (@($script:steps | Where-Object { $_ -like 'folders done /srv/a allowed on lab1*' }).Count) 1
        Check 'step: and only it is recorded' $script:manifest.allowed_folders @('/srv/a')

        # 6. an atrium without the verb: one warn with the command to run later, nothing recorded, never a failure.
        Reset-Case; $freshManifest = $true; $AllowedFolders = @('/srv/a', '/srv/my work')
        Set-Content -LiteralPath (Join-Path $sh2 'old') -Value 1
        Invoke-AllowedFolders *>$null
        Check 'step: no verb is a single warn' @($script:steps).Count 1
        Check 'step: carrying the command, quoted' (@($script:steps)[0] -like 'folders warn this atrium has no room folders verb yet*run: ssh u@lab1 ~/.local/bin/atrium room folders allow /srv/a "/srv/my work"') $true
        Check 'step: nothing recorded' ($null -eq $script:manifest.allowed_folders) $true
        Check 'step: and no allow was attempted' (@(Get-Content -LiteralPath $callsFile).Count) 1

        # 7. a new provision with no clone and no WORKTREE_ROOT: nothing to default to, a warn.
        Reset-Case; $freshManifest = $true; $AllowedFolders = @(); $clonePath = $null
        Invoke-AllowedFolders *>$null
        Check 'step: no clone and no root is a warn' (@($script:steps)[0] -like 'folders warn no clone and no WORKTREE_ROOT*') $true
    } finally {
        Remove-Item -LiteralPath $sh2 -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# ── provision-room.ps1 takes the argument before it touches ssh ─────────────

$prov = Join-Path $PSScriptRoot 'provision-room.ps1'
function Invoke-Prov {
    param([string[]] $a)
    $out = & (Get-Command pwsh).Source -NoProfile -File $prov @a 2>&1
    [pscustomobject]@{ Out = (@($out | ForEach-Object { "$_" }) -join "`n"); Code = $LASTEXITCODE }
}
$r = Invoke-Prov @('nobody@nowhere.invalid', '-AllowedFolders', 'relative/dir')
Check 'provision: a relative folder is exit 1' $r.Code 1
Check 'provision: and names it' ($r.Out -match "provision args fail -AllowedFolders 'relative/dir' is not an absolute path") $true
$r = Invoke-Prov @('nobody@nowhere.invalid', '-AllowedFolders', '/srv/a;/srv/b')
Check 'provision: a semicolon is exit 1' $r.Code 1
$r = Invoke-Prov @('nobody@nowhere.invalid', '-AllowedFolders', '/')
Check 'provision: a filesystem root is exit 1' $r.Code 1
foreach ($other in '-Remove', '-Restart', '-SmokeOnly') {
    $r = Invoke-Prov @('nobody@nowhere.invalid', '-AllowedFolders', '/srv/a', $other)
    Check "provision: -AllowedFolders with $other is exit 1" $r.Code 1
    Check "provision: and says why for $other" ($r.Out -match 'provision args fail -AllowedFolders changes the room') $true
}
# Commas split, as for -Runners: this reaches ssh, which cannot connect to a name that does not exist, and is exit 2.
$r = Invoke-Prov @('nobody@nowhere.invalid', '-AllowedFolders', '/srv/a,/srv/b', '-SshOption', '-oConnectTimeout=2')
Check 'provision: a comma list passes the argument checks and fails at ssh (exit 2)' $r.Code 2

Write-Host ""
if ($script:failed) { Write-Host "$script:failed of $script:ran checks failed."; exit 1 }
Write-Host "all $script:ran checks pass."
