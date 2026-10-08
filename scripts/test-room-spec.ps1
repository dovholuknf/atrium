# Tests for scripts/room-spec.ps1: the room.yaml the provision and check scripts write, the remote script that hands it to
# `atrium room setup`, and the reading of what that printed. Needs no ssh, no hub and no room:
#
#   pwsh -NoProfile -File scripts/test-room-spec.ps1
#
# The Unix remote script is RUN here by sh (skipped when this machine has none) against a stand-in `atrium`, to prove the spec
# arrives on stdin with the account filled in, the arguments are the ones `room setup` takes, and the exit code comes back. When
# a built atrium is named in $env:ATRIUM_TEST_BIN (go build -o build.claude/atrium-roomspec.exe ./cmd/atrium) the spec is also
# read by the REAL `room setup --plan`, as the account running this, so the field names are proved against the binary and not
# against this file's idea of them, and the Windows remote script is run by a child pwsh with that binary as the room's.
# What this cannot run: ssh, Windows PowerShell 5.1, and the flow in provision-room.ps1 and room-check.ps1, which need a hub
# and a room (see docs/changes/f-room-spec-scripts.md).
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'room-spec.ps1')

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
$tmp = Join-Path ([IO.Path]::GetTempPath()) "room-spec-test-$([guid]::NewGuid().ToString('N').Substring(0, 8))"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$sh = (Get-Command sh -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
# A path as the sh on this machine spells it: C:\a\b is /c/a/b under Git for Windows, whose tar reads C: as a host.
function ConvertTo-MsysPath { param([string] $p) if ($isWin -and $p -match '^([A-Za-z]):(.*)$') { "/$($Matches[1].ToLower())$($Matches[2] -replace '\\', '/')" } else { $p -replace '\\', '/' } }
$bin = if ($env:ATRIUM_TEST_BIN -and (Test-Path -LiteralPath $env:ATRIUM_TEST_BIN)) { (Resolve-Path $env:ATRIUM_TEST_BIN).Path } else { $null }

# ── New-RoomSpecYaml ────────────────────────────────────────────────────────

$y = New-RoomSpecYaml -Name 'sg3' -Os 'windows' -WorkRoot 'V:\localai' -PackRepo 'dovholuknf/dotfiles'
Check 'yaml: the fields, in the contract names' ($y -split "`n" | Where-Object { $_ }) @(
    'version: 1', "name: 'sg3'", "os: 'windows'", "account: '@@ACCOUNT@@'", "work_root: 'V:\localai'",
    'packs:', '  - runner: claude', "    repo: 'dovholuknf/dotfiles'", "    branch: 'main'")
$ym = New-RoomSpecYaml -Name 'r' -Os 'linux' -PackRepo 'o/r' -PackRunners 'claude', 'codex', 'gemini', 'codex'
Check 'yaml: one pack entry for each runner, none twice' @(($ym -split "`n") | Where-Object { $_ -like '  - runner: *' }) @('  - runner: claude', '  - runner: codex', '  - runner: gemini')
Check 'yaml: a runner with no pack is refused' ($(try { $null = New-RoomSpecYaml -Name 'r' -Os 'linux' -PackRepo 'o/r' -PackRunners 'x: y'; $false } catch { $true })) $true
Check 'yaml: ends with one newline' ($y.EndsWith("`n") -and -not $y.EndsWith("`n`n")) $true
Check 'yaml: a branch is written' ((New-RoomSpecYaml -Name 'r' -Os 'linux' -WorkRoot '/srv/w' -PackRepo 'o/r' -PackBranch 'feature/x') -like "*branch: 'feature/x'*") $true
Check 'yaml: no pack repo, no packs' ((New-RoomSpecYaml -Name 'r' -Os 'linux' -WorkRoot '/srv/w') -like '*packs*') $false
Check 'yaml: a quote in a path is doubled, never ends the scalar' ((New-RoomSpecYaml -Name 'r' -Os 'linux' -WorkRoot "/srv/it's") -like "*work_root: '/srv/it''s'*") $true
Check 'yaml: a folder with a hash and a colon stays inside its quotes' ((New-RoomSpecYaml -Name 'r' -Os 'linux' -WorkRoot '/srv/a #b: c') -like "*work_root: '/srv/a #b: c'*") $true
Check 'yaml: a newline in the work root is refused, not folded' ($(try { $null = New-RoomSpecYaml -Name 'r' -Os 'linux' -WorkRoot "/srv/a`nb"; $false } catch { $true })) $true
Check 'yaml: a control character in the name is refused' ($(try { $null = New-RoomSpecYaml -Name "a`0b" -Os 'linux' -WorkRoot '/srv/w'; $false } catch { $true })) $true
# The binary refuses a key named like a credential, at any depth. A spec built here never has one.
$keys = @(($y -split "`n") | ForEach-Object { if ($_ -match '^\s*(?:- )?([a-z_]+):') { $Matches[1] } })
Check 'yaml: no key is named like a credential' @($keys | Where-Object { $_ -match '(?i)token|pass|secret|key|cred|auth|bearer' }).Count 0

# ── ConvertFrom-RoomSetup ───────────────────────────────────────────────────

$out = @(
    'setup work-root ok /srv/work is there, writable by claude',
    'setup work-dirs done made 3 under /srv/work',
    'setup work-cache todo npm not yet pointed at the work root. `atrium room setup --apply` does it',
    'setup git-root warn the room is running, so git_root cannot be set. stop the room and run: atrium room set git_root /srv/work/git',
    'setup agent-pack skip',
    'an administrator runs:',
    '    sudo setfacl -m u:claude:x /srv',
    "    sudo install -d -o claude -m 755 /srv/work")
$r = ConvertFrom-RoomSetup $out 13
Check 'parse: a run with rows is ok and keeps its code' @($r.Kind, $r.Code, $r.Rows.Count) @('ok', 13, 5)
Check 'parse: a row is step, status, detail' @($r.Rows[1].Step, $r.Rows[1].Status, $r.Rows[1].Detail) @('work-dirs', 'done', 'made 3 under /srv/work')
Check 'parse: a row with no detail has an empty one' @($r.Rows[4].Step, $r.Rows[4].Status, $r.Rows[4].Detail) @('agent-pack', 'skip', '')
Check 'parse: the administrator lines are kept whole, without their indent' $r.AdminLines @('sudo setfacl -m u:claude:x /srv', 'sudo install -d -o claude -m 755 /srv/work')
Check 'parse: nothing else was said' $r.Other.Count 0
$r = ConvertFrom-RoomSetup @('setup account fail this runs as a and the spec is b', 'warning: something else') 3
Check 'parse: a failing run, and a line that is not a row is kept as other' @($r.Kind, $r.Code, $r.Rows[0].Status, ($r.Other -join '|')) @('ok', 3, 'fail', 'warning: something else')
$r = ConvertFrom-RoomSetup @('setup-missing=binary') 127
Check 'parse: no binary' @($r.Kind, $r.Rows.Count) @('nobinary', 0)
$r = ConvertFrom-RoomSetup @('Error: unknown command "setup" for "atrium room"') 1
Check 'parse: an atrium with no room setup' @($r.Kind, $r.Code) @('unsupported', 1)
$r = ConvertFrom-RoomSetup @('room spec: work_root "/" is a filesystem root') 1
Check 'parse: a refused spec is an error that says why' @($r.Kind, $r.Code, $r.Other[0]) @('error', 1, 'room spec: work_root "/" is a filesystem root')
$r = ConvertFrom-RoomSetup @('#< CLIXML', '<Objs Version="1.1.0.1"></Objs>', 'setup work-root ok fine') 0
Check 'parse: PowerShell remoting noise is not output' @($r.Kind, $r.Other.Count) @('ok', 0)
$r = ConvertFrom-RoomSetup @('setup work-root ok fine', 'unrelated line', '    indented after other text') 0
Check 'parse: indented text is an administrator line only after the header' $r.AdminLines.Count 0
Check 'status: todo is a warn and human a fail in provision words' @((ConvertTo-StepStatus 'todo'), (ConvertTo-StepStatus 'human'), (ConvertTo-StepStatus 'done'), (ConvertTo-StepStatus 'skip')) @('warn', 'fail', 'done', 'skip')

# ── Set-PackRowLatest ───────────────────────────────────────────────────────

$latest = 'a1b2c3d4e5f6' + ('0' * 28)
function New-PackRow { param($detail, $status = 'ok') [pscustomobject]@{ Step = 'agent-pack'; Status = $status; Detail = $detail } }
$rows = @(New-PackRow 'the agent pack at a1b2c3d4e, 11 agents and 4 skills (the hub mirror could not be asked, so whether it is current is not known)')
$rows = @(Set-PackRowLatest $rows $latest)
Check 'pack row: the same commit drops the could-not-be-asked note' @($rows[0].Status, $rows[0].Detail) @('ok', 'the agent pack at a1b2c3d4e, 11 agents and 4 skills')
$rows = @(Set-PackRowLatest @(New-PackRow 'the agent pack at 111111111, 11 agents and 4 skills (the hub mirror could not be asked, so whether it is current is not known)') $latest)
Check 'pack row: another commit is stale, naming both' @($rows[0].Status, [bool]($rows[0].Detail -like 'the agent pack is stale: 111111111 is installed and the hub''s mirror has a1b2c3d4e.*')) @('warn', $true)
$rows = @(Set-PackRowLatest @(New-PackRow 'the agent pack at 111111111, 1 agents (the hub mirror could not be asked, so whether it is current is not known)') $null)
Check 'pack row: a mirror that could not be asked leaves the row alone' @($rows[0].Status, [bool]($rows[0].Detail -like '*could not be asked*')) @('ok', $true)
$rows = @(Set-PackRowLatest @(New-PackRow 'the agent pack is not installed.' 'todo') $latest)
Check 'pack row: a row that is not ok is left alone' $rows[0].Status 'todo'
$other = [pscustomobject]@{ Step = 'work-root'; Status = 'ok'; Detail = 'the agent pack at 1234, x' }
Check 'pack row: only the agent-pack step' @((Set-PackRowLatest @($other) $latest)[0].Status) @('ok')
$cx = [pscustomobject]@{ Step = 'agent-pack-codex'; Status = 'ok'; Detail = 'the agent pack at 111111111, 2 skills (the hub mirror could not be asked, so whether it is current is not known)' }
$rows = @(Set-PackRowLatest @($cx) $latest)
Check 'pack row: a codex row is judged like the claude one' @($rows[0].Status, [bool]($rows[0].Detail -like 'the agent pack is stale: 111111111*')) @('warn', $true)

# ── the second drive hint ───────────────────────────────────────────────────

Check 'hint: a big second drive is named, with the flag to pass' ([bool](Get-WorkHintDetail @('drive=V:\|820') -like '*V:\ with 820 GB free*-WorkRoot V:\localai*')) $true
Check 'hint: a small drive says nothing' (Get-WorkHintDetail @('drive=E:\|8')) $null
Check 'hint: no drive says nothing' (Get-WorkHintDetail @()) $null
Check 'hint: the roomiest of two' ([bool](Get-WorkHintDetail @('drive=E:\|60', 'drive=V:\|820') -like '*V:\*')) $true
Check 'path: slashes forward, none at the end' @((Format-WorkPath 'V:\localai\'), (Format-WorkPath '/srv/x/')) @('V:/localai', '/srv/x')

# ── the pack source arguments and the mirror url ────────────────────────────

Check 'mirror url: the host is github, as in the hub''s own git urls' (Get-HubMirrorUrl '127.0.0.1:7778' 'dovholuknf/dotfiles') 'http://127.0.0.1:7778/git/hub/github/dovholuknf/dotfiles.git'
Check 'mirror url: slashes around the repo are dropped' (Get-HubMirrorUrl 'h:1' '/o/r/') 'http://h:1/git/hub/github/o/r.git'
Check 'pack args: the default repo and branch' (Test-PackArg 'dovholuknf/dotfiles' 'main') $null
Check 'pack args: a branch with a slash' (Test-PackArg 'o/r' 'feature/x-1') $null
Check 'pack args: a branch that starts with - is refused (it would be a git option)' ([bool](Test-PackArg 'o/r' '--upload-pack=x')) $true
Check 'pack args: a branch with .. is refused' ([bool](Test-PackArg 'o/r' 'a..b')) $true
Check 'pack args: a repo that is not owner/name, or starts with -, is refused' @([bool](Test-PackArg 'o' 'main'), [bool](Test-PackArg '-o/r' 'main'), [bool](Test-PackArg 'o/r;x' 'main')) @($true, $true, $true)
$was = $env:GIT_TERMINAL_PROMPT; $env:GIT_TERMINAL_PROMPT = 'keep'
$null = Get-HubMirrorCommit '127.0.0.1:1' 'o/r' 'main'
Check 'mirror commit: GIT_TERMINAL_PROMPT is put back' $env:GIT_TERMINAL_PROMPT 'keep'
if ($null -eq $was) { Remove-Item Env:GIT_TERMINAL_PROMPT -ErrorAction SilentlyContinue } else { $env:GIT_TERMINAL_PROMPT = $was }
Check 'mirror commit: a hub that is not there is $null, not a throw' (Get-HubMirrorCommit '127.0.0.1:1' 'o/r' 'main') $null
$np = New-PackSource -HubAddr '127.0.0.1:1' -Repo 'o/r' -Branch '--upload-pack=x' -OutDir (Join-Path $tmp 'np')
Check 'pack source: a branch that would be a git option is refused before git runs' @($np.Ok, [bool]($np.Why -like '*not a branch name*')) @($false, $true)
$np = New-PackSource -HubAddr '127.0.0.1:1' -Repo 'o/r' -Branch 'main' -OutDir (Join-Path $tmp 'np')
Check 'pack source: a hub that is not there is a reason, not a throw' @($np.Ok, [bool]($np.Why -like 'could not fetch main of http://127.0.0.1:1/*')) @($false, $true)

# a local "hub": a bare repository served from a file path stands in for the mirror, to prove the tarball holds the checkout AND its .git
$git = Get-Command git -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($git) {
    $work = Join-Path $tmp 'mk'; New-Item -ItemType Directory -Force -Path $work | Out-Null
    & git -C $work init -q -b main 2>&1 | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $work 'claude/agents') | Out-Null
    Set-Content -LiteralPath (Join-Path $work 'claude/agents/a.md') -Value 'agent a'
    New-Item -ItemType Directory -Force -Path (Join-Path $work 'gemini/skills/s') | Out-Null
    Set-Content -LiteralPath (Join-Path $work 'gemini/skills/s/SKILL.md') -Value 'skill s'
    New-Item -ItemType Directory -Force -Path (Join-Path $work 'codex') | Out-Null
    Set-Content -LiteralPath (Join-Path $work 'codex/README.md') -Value 'no agents or skills in here'
    & git -C $work add -A 2>&1 | Out-Null
    & git -C $work -c user.name=t -c user.email=t@t commit -q -m one 2>&1 | Out-Null
    $head = (& git -C $work rev-parse HEAD).Trim()
    # New-PackSource builds http://<addr>/git/hub/github/<repo>.git, so a url rewrite points that at the folder
    $env:GIT_CONFIG_COUNT = '1'; $env:GIT_CONFIG_KEY_0 = "url.$($work -replace '\\', '/').insteadOf"; $env:GIT_CONFIG_VALUE_0 = 'http://h:1/git/hub/github/o/r.git'
    try {
        $ps = New-PackSource -HubAddr 'h:1' -Repo 'o/r' -Branch 'main' -OutDir (Join-Path $tmp 'ps')
        Check 'pack source: a clone of the mirror, with its commit' @($ps.Ok, $ps.Commit) @($true, $head)
        Check 'pack source: claude always, gemini for its skills folder, codex not for a folder with neither' @($ps.Runners) @('claude', 'gemini')
        $x = Join-Path $tmp 'unpacked'; New-Item -ItemType Directory -Force -Path $x | Out-Null
        & (Get-PackTar) -xzf $ps.Tgz -C $x
        Check 'pack source: the tarball holds the agents and the .git the room asks for its commit' @((Test-Path -LiteralPath (Join-Path $x 'claude/agents/a.md')), (Test-Path -LiteralPath (Join-Path $x '.git'))) @($true, $true)
        Check 'pack source: and the checkout there says the same commit' ((& git -C $x rev-parse HEAD).Trim()) $head
    } finally { Remove-Item Env:GIT_CONFIG_COUNT, Env:GIT_CONFIG_KEY_0, Env:GIT_CONFIG_VALUE_0 -ErrorAction SilentlyContinue }
}

# ── Get-RoomSetupScript, built and run ──────────────────────────────────────

$yaml = New-RoomSpecYaml -Name 'r1' -Os 'linux' -WorkRoot "/srv/it's work" -PackRepo 'o/r'
$u = Get-RoomSetupScript -Os 'linux' -Yaml $yaml -Mode 'plan'
Check 'unix script: asks for plan, never apply, and passes no pack dir' @([bool]($u -like '* room setup --spec - --plan*'), [bool]($u -like '*--apply*'), [bool]($u -like '*--pack-dir*')) @($true, $false, $false)
$ua = Get-RoomSetupScript -Os 'linux' -Yaml $yaml -Mode 'apply' -UsePackDir
Check 'unix script: apply with a pack dir, which it removes after' @([bool]($ua -like '* room setup --spec - --apply --pack-dir "$packdir"*'), [bool]($ua -like '*rm -rf "$packdir" "$packdir.tgz"*')) @($true, $true)
Check 'unix script: says where the binary is when it is not' ([bool]($u -like '*setup-missing=binary*exit 127*')) $true
$w = Get-RoomSetupScript -Os 'windows' -Yaml $yaml -Mode 'plan'
Check 'windows script: the spec rides as base64, so no quote of the path is in the script text' @([bool]($w -like "*it's work*"), [bool]($w -like '*FromBase64String*')) @($false, $true)
Check 'windows script: plan, and no pack dir' @([bool]($w -like '* room setup --spec - --plan *'), [bool]($w -like '*--pack-dir*')) @($true, $false)
$wa = Get-RoomSetupScript -Os 'windows' -Yaml $yaml -Mode 'apply' -UsePackDir
Check 'windows script: apply with a pack dir, removed after' @([bool]($wa -like '* --apply --pack-dir $packdir*'), [bool]($wa -like '*Remove-Item -LiteralPath $packdir*')) @($true, $true)
$errs = $null; [void][Management.Automation.Language.Parser]::ParseInput($w, [ref]$null, [ref]$errs)
Check 'windows script: parses as PowerShell' @($errs).Count 0
$errs = $null; [void][Management.Automation.Language.Parser]::ParseInput((Get-PackUnpackScript 'windows'), [ref]$null, [ref]$errs)
Check 'windows unpack script: parses as PowerShell' @($errs).Count 0

if ($sh) {
    $home1 = Join-Path $tmp 'uhome'; New-Item -ItemType Directory -Force -Path (Join-Path $home1 '.local/bin') | Out-Null
    $fake = Join-Path $home1 '.local/bin/atrium'
    # the stand-in prints its arguments and the spec it was handed on stdin, then exits with the code in FAKE_RC
    [IO.File]::WriteAllText($fake, "#!/bin/sh`necho `"args=`$*`"`ncat`nexit `${FAKE_RC:-0}`n", [Text.UTF8Encoding]::new($false))
    $shHome = ConvertTo-MsysPath $home1
    function Run-Sh { param([string] $script, [string] $rc = '0', [string] $homeDir = $shHome)
        $f = Join-Path $tmp "run-$([guid]::NewGuid().ToString('N').Substring(0, 6)).sh"
        [IO.File]::WriteAllText($f, (($script -replace "`r", '') + "`n"), [Text.UTF8Encoding]::new($false))
        $o = & $sh -c "chmod +x '$(ConvertTo-MsysPath $fake)' 2>/dev/null; HOME='$homeDir' FAKE_RC=$rc sh '$(ConvertTo-MsysPath $f)'" 2>&1
        [pscustomobject]@{ Out = @($o | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
    }
    $me = (& $sh -c 'id -un').Trim()
    $r = Run-Sh $u 13
    Check 'unix run: the exit code comes back' $r.Code 13
    Check 'unix run: the arguments are room setup --spec - --plan' ($r.Out | Where-Object { $_ -like 'args=*' }) 'args=room setup --spec - --plan'
    Check 'unix run: the account is the login, filled in' @($r.Out | Where-Object { $_ -eq "account: '$me'" }).Count 1
    Check 'unix run: the quote in the work root arrived as it was written' @($r.Out | Where-Object { $_ -eq "work_root: '/srv/it''s work'" }).Count 1
    $r = Run-Sh $u 0 (ConvertTo-MsysPath $tmp)
    Check 'unix run: no binary is 127 and says so' @($r.Code, ($r.Out -join '|')) @(127, 'setup-missing=binary')
    $pd = Join-Path $home1 '.atrium/provision'; New-Item -ItemType Directory -Force -Path (Join-Path $pd 'pack-src') | Out-Null
    [IO.File]::WriteAllText((Join-Path $pd 'pack-src.tgz'), 'x')
    $r = Run-Sh $ua 0
    Check 'unix run: the pack dir is the one in ~/.atrium/provision' ([bool]($r.Out | Where-Object { $_ -like "args=room setup --spec - --apply --pack-dir */.atrium/provision/pack-src" })) $true
    Check 'unix run: the pack source and its tarball are gone afterwards' @((Test-Path -LiteralPath (Join-Path $pd 'pack-src')), (Test-Path -LiteralPath (Join-Path $pd 'pack-src.tgz'))) @($false, $false)
    # the unpack script, with a real tarball
    $src = Join-Path $tmp 'tarsrc'; New-Item -ItemType Directory -Force -Path $src | Out-Null
    [IO.File]::WriteAllText((Join-Path $src 'hello.txt'), 'hi')
    & (Get-PackTar) -czf (Join-Path $pd 'pack-src.tgz') -C $src .
    $r = Run-Sh (Get-PackUnpackScript 'linux') 0
    Check 'unix unpack: the tarball is extracted to pack-src' @($r.Code, ($r.Out -join '|'), (Test-Path -LiteralPath (Join-Path $pd 'pack-src/hello.txt'))) @(0, 'unpack=ok', $true)
    [IO.File]::WriteAllText((Join-Path $pd 'pack-src.tgz'), 'not a tarball')
    $r = Run-Sh (Get-PackUnpackScript 'linux') 0
    Check 'unix unpack: a bad tarball is unpack=fail and exit 1, and leaves nothing behind' @($r.Code, ($r.Out | Where-Object { $_ -eq 'unpack=fail' }).Count, (Test-Path -LiteralPath (Join-Path $pd 'pack-src'))) @(1, 1, $false)
} else {
    Write-Host 'skip unix runs: no sh on this machine'
}

# ── against the real binary ─────────────────────────────────────────────────

if ($bin) {
    $goos = if ($isWin) { 'windows' } elseif ($IsMacOS) { 'darwin' } else { 'linux' }
    $login = if ($isWin) { ([Security.Principal.WindowsIdentity]::GetCurrent().Name -split '\\')[-1] } else { (& id -un).Trim() }
    $wrRoot = if ($isWin) { Join-Path $tmp 'work' } else { (Join-Path $tmp 'work') }
    $spec = (New-RoomSpecYaml -Name 'rt' -Os $goos -WorkRoot $wrRoot -PackRepo 'dovholuknf/dotfiles') -replace '@@ACCOUNT@@', $login
    $o = $spec | & $bin room setup --spec - --plan 2>&1
    $code = $LASTEXITCODE
    $res = ConvertFrom-RoomSetup @($o | ForEach-Object { "$_" }) $code
    Check 'binary: reads the spec this builds (rows came back, not a refusal)' @($res.Kind, [bool](@($res.Rows | Where-Object { $_.Step -eq 'work-root' }).Count)) @('ok', $true)
    Check 'binary: the plan names the agent pack' ([bool](@($res.Rows | Where-Object { $_.Step -eq 'agent-pack' }).Count)) $true
    Check 'binary: a plan wrote nothing (no work root made)' (Test-Path -LiteralPath $wrRoot) $false
    $bad = ((New-RoomSpecYaml -Name 'rt' -Os $goos -WorkRoot $(if ($isWin) { 'C:\' } else { '/' })) -replace '@@ACCOUNT@@', $login)
    $o = $bad | & $bin room setup --spec - --plan 2>&1
    $res = ConvertFrom-RoomSetup @($o | ForEach-Object { "$_" }) $LASTEXITCODE
    Check 'binary: a filesystem root is refused, exit 1, with its reason' @($res.Kind, $res.Code, [bool](($res.Other -join ' ') -match 'filesystem root|top-level')) @('error', 1, $true)
    if ($isWin) {
        # the Windows remote script, run by a child pwsh whose HOME holds the room's binary
        $wh = Join-Path $tmp 'whome'; New-Item -ItemType Directory -Force -Path (Join-Path $wh '.atrium/bin') | Out-Null
        Copy-Item -LiteralPath $bin -Destination (Join-Path $wh '.atrium/bin/atrium.exe')
        $ws = Get-RoomSetupScript -Os 'windows' -Yaml (New-RoomSpecYaml -Name 'rt' -Os 'windows' -WorkRoot (Join-Path $tmp 'wwork') -PackRepo 'o/r') -Mode 'plan'
        $sf = Join-Path $tmp 'win-run.ps1'; [IO.File]::WriteAllText($sf, $ws, [Text.UTF8Encoding]::new($true))
        # HOME is set in THIS process, so the child's $HOME starts as it
        $keep = @($env:HOME, $env:USERPROFILE)
        try {
            $env:HOME = $wh; $env:USERPROFILE = $wh
            $o = & pwsh -NoProfile -File $sf 2>&1
            $res = ConvertFrom-RoomSetup @($o | ForEach-Object { "$_" }) $LASTEXITCODE
            Check 'windows run: the spec reaches the real room setup with the account filled in' @($res.Kind, [bool](@($res.Rows | Where-Object { $_.Step -eq 'account' -and $_.Status -eq 'fail' }).Count)) @('ok', $false)
            $env:HOME = $tmp; $env:USERPROFILE = $tmp
            $o = & pwsh -NoProfile -File $sf 2>&1
            $res = ConvertFrom-RoomSetup @($o | ForEach-Object { "$_" }) $LASTEXITCODE
        } finally { $env:HOME = $keep[0]; $env:USERPROFILE = $keep[1] }
        Check 'windows run: no binary is 127' @($res.Kind, $res.Code) @('nobinary', 127)
    }
} else {
    Write-Host 'skip binary runs: set ATRIUM_TEST_BIN to a built atrium (go build -o build.claude/atrium-roomspec.exe ./cmd/atrium)'
}

# ── a failed pack does not stop a run, and a bad root is refused before ssh ──

$packFail = [pscustomobject]@{ Kind = 'ok'; Code = 3; AdminLines = @(); Other = @(); Rows = @(
        [pscustomobject]@{ Step = 'work-root'; Status = 'ok'; Detail = 'x' }, [pscustomobject]@{ Step = 'agent-pack'; Status = 'fail'; Detail = 'bad tarball' }) }
$lim = Limit-PackFailures $packFail
Check 'pack failure: the row is a warn and the exit code is 0' @($lim.Rows[1].Status, $lim.Code) @('warn', 0)
$both = [pscustomobject]@{ Kind = 'ok'; Code = 3; AdminLines = @(); Other = @(); Rows = @(
        [pscustomobject]@{ Step = 'work-dirs'; Status = 'fail'; Detail = 'x' }, [pscustomobject]@{ Step = 'agent-pack-claude'; Status = 'fail'; Detail = 'y' }) }
$lim = Limit-PackFailures $both
Check 'pack failure: another failing step still fails the run' @($lim.Rows[0].Status, $lim.Rows[1].Status, $lim.Code) @('fail', 'warn', 3)
$human = [pscustomobject]@{ Kind = 'ok'; Code = 13; AdminLines = @('x'); Other = @(); Rows = @([pscustomobject]@{ Step = 'work-root'; Status = 'human'; Detail = 'x' }, [pscustomobject]@{ Step = 'agent-pack'; Status = 'fail'; Detail = 'y' }) }
Check 'pack failure: an administrator is still 13' (Limit-PackFailures $human).Code 13
Check 'pack failure: no binary is left alone' (Limit-PackFailures ([pscustomobject]@{ Kind = 'nobinary'; Code = 127; Rows = @() })).Code 127
Check 'yaml: no work root is a spec of the pack alone' ((New-RoomSpecYaml -Name 'r' -Os 'linux' -PackRepo 'o/r') -like '*work_root*') $false
Check 'local check: no atrium to ask is not a refusal' (Test-WorkRootLocal '' 'linux' 'x' 'al') $null
if ($bin) {
    foreach ($c in @(@('linux', 'relative/dir', 'al', 'not an absolute path'), @('windows', '/srv/localai', 'al', 'not a drive path'), @('linux', '/home/bob/x', 'al', "bob's home"),
            @('darwin', '/private/var/root/x', 'al', "root's home"), @('windows', 'V:\', 'al', 'filesystem root'), @('linux', '//host/share', 'al', 'network path'))) {
        $why = Test-WorkRootLocal $bin $c[0] $c[1] $c[2]
        Check "local check: $($c[0]) $($c[1]) is refused before ssh" ([bool]($why -and $why -like "*$($c[3])*")) $true
    }
    Check 'local check: a good root passes' @((Test-WorkRootLocal $bin 'linux' '/srv/localai' 'al'), (Test-WorkRootLocal $bin 'windows' 'V:/localai' ''), (Test-WorkRootLocal $bin 'darwin' '/Users/al/w' 'al')) @($null, $null, $null)
}

# ── the scripts that use it ─────────────────────────────────────────────────

foreach ($n in 'room-spec.ps1', 'provision-room.ps1', 'room-check.ps1') {
    $errs = $null; [void][Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $n), [ref]$null, [ref]$errs)
    Check "parse: $n" @($errs).Count 0
}
$pr = Join-Path $PSScriptRoot 'provision-room.ps1'
function Run-Pr { param([string[]] $a) $o = & pwsh -NoProfile -File $pr 'x@nowhere' @a 2>&1; [pscustomobject]@{ Out = @($o | ForEach-Object { "$_" }); Code = $LASTEXITCODE } }
foreach ($c in @(
        @('-Remove', @('-WorkRoot', 'V:\localai', '-Remove'), 'belong to a provision run'),
        @('-Restart', @('-WorkRoot', 'V:\localai', '-Restart'), 'belong to a provision run'),
        @('-SmokeOnly', @('-WorkRoot', 'V:\localai', '-SmokeOnly'), 'belong to a provision run'),
        @('-NoAgentPack with -Remove', @('-NoAgentPack', '-Remove'), 'belong to a provision run'),
        @('-NoSharedFolder', @('-WorkRoot', 'V:\localai', '-NoSharedFolder'), 'takes the place of the shared folder'),
        @('an empty folder', @('-WorkRoot', ''), 'empty or holds a control character'),
        @('a bad pack branch', @('-AgentPackBranch', '--upload-pack=x'), 'not a branch name'),
        @('a bad pack repo', @('-AgentPackRepo', 'nope'), 'not owner/name'))) {
    $r = Run-Pr $c[1]
    Check "provision: $($c[0]) is exit 1 and says why" @($r.Code, [bool](@($r.Out | Where-Object { $_ -like "provision args fail*$($c[2])*" }).Count)) @(1, $true)
}
# What a work root's shape is, is the binary's to say (internal/roomspec, `go test ./internal/roomspec`): a relative folder, a
# drive root or a UNC path passes the argument check here and is refused by `room setup` with exit 1, once there is ssh.
$r = Run-Pr @('-WorkRoot', 'V:\localai', '-Check')
Check 'provision: a good -WorkRoot passes the argument checks and fails at ssh (exit 2)' $r.Code 2
$text = Get-Content -LiteralPath $pr -Raw
Check 'provision: setup is wired in, plan before and apply after, exit 13 passed on' @([bool]($text -match "Invoke-RoomSetup 'plan'"), [bool]($text -match "Invoke-RoomSetup 'apply'"), [bool]($text -match 'Finish \$pl\.Code'), [bool]($text -match 'Finish \$ap\.Code'), [bool]($text -match '#  13  ')) @($true, $true, $true, $true, $true)
Check 'provision: nothing of the old scripts is left in it' @([bool]($text -match 'room-work|Invoke-WorkRoot|Get-AgentPack|Test-WorkRootArg|New-AgentPack\b')) @($false)
$rc = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'room-check.ps1') -Raw
Check 'room-check: the plan rows are wired in, with no old helper' @([bool]($rc -match "Get-RoomSetupScript"), [bool]($rc -match "-Mode plan"), [bool]($rc -match 'room-work|Invoke-WorkRoot|Get-AgentPack')) @($true, $true, $false)
Check 'old scripts are gone' @((Test-Path (Join-Path $PSScriptRoot 'room-work.ps1')), (Test-Path (Join-Path $PSScriptRoot 'test-room-work.ps1'))) @($false, $false)

Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
Write-Host ''
if ($script:failed) { Write-Host "$script:failed of $script:ran checks failed."; exit 1 }
Write-Host "all $script:ran checks passed."
