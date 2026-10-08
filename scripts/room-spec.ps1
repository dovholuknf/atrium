# What provision-room.ps1 and room-check.ps1 share to drive `atrium room setup`. DOT-SOURCED, runs nothing by itself:
#
#   . (Join-Path $PSScriptRoot 'room-spec.ps1')
#
# The work root, the tool caches, the room's folder settings and the agent pack are made, read and judged by the room's own
# atrium (internal/roomspec, `atrium room setup --spec - --plan|--apply`), not by anything in these scripts. This file only
#
#   - writes the room.yaml for a room (New-RoomSpecYaml), from what the operator typed or the manifest recorded,
#   - builds the remote script that pipes it to `atrium room setup` (Get-RoomSetupScript),
#   - reads what that printed (ConvertFrom-RoomSetup): `setup <step> <status> <detail>` lines, then, after
#     `an administrator runs:`, the lines an administrator pastes, and the exit code (0 ok, 13 an administrator is needed,
#     3 a step failed, 1 the arguments are wrong), and
#   - gets the agent pack's source onto the room, since the room's /git/hub forwarder is tokenized per card and no card exists
#     at provision time: THIS machine clones the hub's own mirror (New-PackSource) and the room is told `--pack-dir`.
#
# The account in the spec is written as @@ACCOUNT@@ and the remote script puts the ssh login there, because everything runs as
# that login and the binary compares the account with who it runs as. Text in and text out, so scripts/test-room-spec.ps1 runs
# it without ssh.

. (Join-Path $PSScriptRoot 'room-folders.ps1')

# Format-WorkPath is a path as the settings spell it: forward slashes, no trailing slash.
function Format-WorkPath { param([string] $p) ($p -replace '\\', '/').TrimEnd('/') }

# Get-WorkHint says, for a machine given no -WorkRoot, that a second fixed drive is there. Windows only, and only as advice.
# Get-WorkHintScript prints one `drive=<root>|<free GB>` line for each fixed drive that is not the system drive.
function Get-WorkHintScript {
    @'
$sys = $env:SystemDrive + '\'
[IO.DriveInfo]::GetDrives() | Where-Object { $_.DriveType -eq 'Fixed' -and $_.IsReady -and $_.Name -ne $sys } | ForEach-Object { "drive=$($_.Name)|$([math]::Round($_.AvailableFreeSpace / 1GB))" }
'@
}
function Get-WorkHintDetail {
    param($lines, [int] $minGB = 50)
    $c = @(@($lines) | ForEach-Object { "$_" } | Where-Object { $_ -match '^drive=(.+)\|(\d+)$' } | ForEach-Object { [pscustomobject]@{ Root = $Matches[1]; GB = [int]$Matches[2] } } | Where-Object { $_.GB -ge $minGB })
    if (-not $c.Count) { return $null }
    $b = $c | Sort-Object GB -Descending | Select-Object -First 1
    "the machine has another fixed drive, $($b.Root) with $($b.GB) GB free, and no -WorkRoot was given, so agent files land on the system drive. pass -WorkRoot $($b.Root)localai to keep them off it"
}

# ── the spec ────────────────────────────────────────────────────────────────

# YAML single quotes hold anything but a newline, and ' is written ''. A value with a control character is refused here: the
# binary refuses it too, but a newline would otherwise be folded into a space before it could say so.
function ConvertTo-YamlText {
    param([string] $s)
    if ($s -match '[\x00-\x1f\x7f]') { throw "a control character cannot be written into a room.yaml: $($s -replace '[\x00-\x1f\x7f]', '?')" }
    "'" + ($s -replace "'", "''") + "'"
}

# New-RoomSpecYaml is the room.yaml (version 1) for a room: the layout and caches are the binary's defaults, and the pack is the
# claude one unless $PackRepo is empty. Field names are the contract of internal/roomspec/spec.go.
function New-RoomSpecYaml {
    param([string] $Name, [string] $Os, [string] $WorkRoot, [string] $Account = '@@ACCOUNT@@', [string] $PackRepo, [string] $PackBranch = 'main', [string[]] $PackRunners = @('claude'))
    $y = @(
        'version: 1'
        "name: $(ConvertTo-YamlText $Name)"
        "os: $(ConvertTo-YamlText $Os)"
        "account: $(ConvertTo-YamlText $Account)"
    )
    # No work root is a spec of the pack alone (a room provisioned without -WorkRoot still gets its agents and skills)
    if ($WorkRoot) { $y += "work_root: $(ConvertTo-YamlText $WorkRoot)" }
    if ($PackRepo) {
        $y += 'packs:'
        foreach ($r in @($PackRunners | Where-Object { $_ } | Select-Object -Unique)) {
            if ($r -notmatch '^(claude|codex|gemini)$') { throw "'$r' is not a runner with an agent pack (claude, codex, gemini)" }
            $y += "  - runner: $r"
            $y += "    repo: $(ConvertTo-YamlText $PackRepo)"
            $y += "    branch: $(ConvertTo-YamlText $PackBranch)"
        }
    }
    ($y -join "`n") + "`n"
}

# ── the remote script ───────────────────────────────────────────────────────

# Get-RoomSetupScript is the script that runs `atrium room setup` on the room, for Invoke-Remote. Windows gets PowerShell and
# the spec rides as base64, so a path with a non-ASCII letter or a quote arrives as it was. Unix gets sh and a quoted printf.
# $Mode is plan (reads, cannot write) or apply. $UsePackDir takes the pack from ~/.atrium/provision/pack-src (put there by
# Send-PackSource) and removes it, and its tarball, afterwards. It prints what setup prints and exits with its code, or with 127
# and `setup-missing=binary` when atrium is not there.
function Get-RoomSetupScript {
    param([ValidateSet('windows', 'linux', 'darwin', 'unix')] [string] $Os, [string] $Yaml, [ValidateSet('plan', 'apply')] [string] $Mode, [switch] $UsePackDir)
    if ($Os -eq 'windows') {
        $b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($Yaml))
        $pd = if ($UsePackDir) { " --pack-dir `$packdir" } else { '' }
        $clean = if ($UsePackDir) { "`nRemove-Item -LiteralPath `$packdir, `"`$packdir.tgz`" -Recurse -Force -ErrorAction SilentlyContinue" } else { '' }
        return "`$Bin = Join-Path `$HOME '.atrium\bin\atrium.exe'; `$packdir = Join-Path `$HOME '.atrium\provision\pack-src'`n" +
            "`$ErrorActionPreference = 'Continue'`n" +
            "if (-not (Test-Path -LiteralPath `$Bin)) { 'setup-missing=binary'; exit 127 }`n" +
            "`$spec = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$b64')).Replace('@@ACCOUNT@@', ([Security.Principal.WindowsIdentity]::GetCurrent().Name -split '\\')[-1])`n" +
            "`$OutputEncoding = New-Object Text.UTF8Encoding `$false`n" +
            "`$spec | & `$Bin room setup --spec - --$Mode$pd 2>&1 | ForEach-Object { `"`$_`" }`n`$rc = `$LASTEXITCODE$clean`nexit `$rc"
    }
    $pd = if ($UsePackDir) { " --pack-dir `"`$packdir`"" } else { '' }
    $clean = if ($UsePackDir) { "`nrm -rf `"`$packdir`" `"`$packdir.tgz`"" } else { '' }
    "Bin=`"`$HOME/.local/bin/atrium`"; packdir=`"`$HOME/.atrium/provision/pack-src`"`n" +
        "if [ ! -x `"`$Bin`" ]; then echo setup-missing=binary; exit 127; fi`n" +
        "printf %s $(ConvertTo-ShLiteral $Yaml) | sed `"s/@@ACCOUNT@@/`$(id -un)/`" | `"`$Bin`" room setup --spec - --$Mode$pd 2>&1`nrc=`$?$clean`nexit `$rc"
}

# ConvertFrom-RoomSetup reads a setup run: Rows (Step, Status, Detail), AdminLines (the lines under `an administrator runs:`),
# Other (anything else it printed, an error among them) and Code. Kind says what the run was:
#   ok           the binary answered with rows. Code is its exit code
#   nobinary     there is no atrium on the room yet
#   unsupported  the room's atrium has no `room setup` yet (an older build), so it can only be checked by a newer one
#   error        it ran and said no rows: Other says why (a spec it refused is exit 1)
function ConvertFrom-RoomSetup {
    param($Lines, [int] $Code)
    $rows = @(); $admin = @(); $other = @(); $inAdmin = $false
    foreach ($l in @($Lines)) {
        $s = "$l"
        if ($s -match '^setup (\S+) (ok|done|todo|warn|fail|human|skip)(?: (.*))?$') {
            $rows += [pscustomobject]@{ Step = $Matches[1]; Status = $Matches[2]; Detail = "$($Matches[3])".TrimEnd() }
            $inAdmin = $false
        } elseif ($s -match '^an administrator runs:\s*$') { $inAdmin = $true }
        elseif ($inAdmin -and $s -match '^    (\S.*)$') { $admin += $Matches[1].TrimEnd() }
        elseif ($s.Trim() -and $s -notmatch '^#< CLIXML|^<Objs |^</Objs>') { $other += $s.Trim(); $inAdmin = $false }
    }
    $text = (@($Lines) | ForEach-Object { "$_" }) -join "`n"
    $kind = if ($Code -eq 127 -or $text -match 'setup-missing=binary') { 'nobinary' }
            elseif ($rows.Count) { 'ok' }
            elseif ($text -match '(?i)unknown (command|subcommand)|no such command') { 'unsupported' }
            else { 'error' }
    [pscustomobject]@{ Kind = $kind; Rows = $rows; AdminLines = $admin; Other = $other; Code = $Code }
}

# ConvertTo-StepStatus is a setup status in the vocabulary provision-room.ps1 prints: todo (a run would change it) is a warn,
# and human (an administrator must) is a fail. room-check keeps human, which is its own status.
function ConvertTo-StepStatus { param([string] $s) switch ($s) { 'todo' { 'warn' } 'human' { 'fail' } default { $s } } }

# Limit-PackFailures keeps the agent pack from stopping a run. The pack is the operator's extras, so a pack that cannot be
# fetched or installed is a warn here and never an exit code (docs/room-accounts.md). `atrium room setup` exits 3 on any fail
# row, so the code is worked out again without the pack's: 13 if an administrator is needed, 3 if another step failed, else 0.
function Limit-PackFailures {
    param($Res)
    if ($Res.Kind -ne 'ok') { return $Res }
    foreach ($r in @($Res.Rows)) { if ($r.Step -like 'agent-pack*' -and $r.Status -eq 'fail') { $r.Status = 'warn' } }
    if ($Res.Code -eq 3) {
        $Res.Code = if (@($Res.Rows | Where-Object { $_.Status -eq 'human' }).Count) { 13 } elseif (@($Res.Rows | Where-Object { $_.Status -eq 'fail' }).Count) { 3 } else { 0 }
    }
    $Res
}

# Test-WorkRootLocal asks THIS machine's atrium whether a -WorkRoot is one the room's operating system accepts
# (`atrium room setup --validate`: strings only, no disk, no ssh), so a bad one is refused before anything on the room changes.
# Returns $null when it is fine, or when this atrium has no --validate (an older build: the room's own atrium judges it later,
# at the plan or apply), else the reason. $Account is the login the room runs as, or '' when it is not known yet.
function Test-WorkRootLocal {
    param([string] $Exe, [string] $Os, [string] $Root, [string] $Account)
    if (-not $Exe) { return $null }
    $a = @('room', 'setup', '--validate', '--os', $Os, '--work-root', $Root)
    if ($Account) { $a += @('--account', $Account) }
    $o = @(& $Exe @a 2>&1 | ForEach-Object { "$_" })
    if ($LASTEXITCODE -eq 0) { return $null }
    $m = @($o | Where-Object { $_ -match '^atrium room setup: (.+)$' } | ForEach-Object { $Matches[1] })
    if ($LASTEXITCODE -eq 1 -and $m.Count) { return $m[0] }
    $null
}

# Set-PackRowLatest settles the agent-pack row against the commit THIS machine read from the hub's mirror. The room cannot be
# told the hub's address (it is the address as this machine sees it), so the room's row says "could not be asked", and here is
# where a stale pack is found. $latest is $null when the mirror could not be asked either.
function Set-PackRowLatest {
    param($Rows, [string] $Latest)
    foreach ($r in @($Rows)) {
        if ($r.Step -notlike 'agent-pack*' -or $r.Status -ne 'ok' -or -not $Latest) { continue }
        if ($r.Detail -notmatch '^the agent pack at ([0-9a-f]{4,40})[ ,]') { continue }
        $short = $Matches[1]
        if ($Latest.StartsWith($short)) { $r.Detail = $r.Detail -replace ' \(the hub mirror could not be asked[^)]*\)$', '' }
        else {
            $r.Status = 'warn'
            $r.Detail = "the agent pack is stale: $short is installed and the hub's mirror has $($Latest.Substring(0, [Math]::Min(9, $Latest.Length))). " +
                'run `atrium room setup --apply` (a provision run does) to update it'
        }
    }
    $Rows
}

# ── the agent pack's source ─────────────────────────────────────────────────

# Get-HubMirrorUrl is where the hub serves a repository it mirrors. The host is `github`, as in the hub's own git URLs.
function Get-HubMirrorUrl { param([string] $hubAddr, [string] $repo) "http://$hubAddr/git/hub/github/$($repo.Trim('/')).git" }

function Test-PackArg {
    param([string] $repo, [string] $branch)
    if ($repo -notmatch '^[A-Za-z0-9_][A-Za-z0-9_.-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*$') { return "-AgentPackRepo '$repo' is not owner/name" }
    if ($branch -notmatch '^[A-Za-z0-9_][A-Za-z0-9_./-]*$' -or $branch -match '\.\.') { return "-AgentPackBranch '$branch' is not a branch name (it may not start with - or hold ..)" }
    $null
}

# Get-HubMirrorCommit is the commit the hub's mirror has for a branch, or $null when it cannot be asked. Reads refs only.
function Get-HubMirrorCommit {
    param([string] $hubAddr, [string] $repo, [string] $branch)
    if (Test-PackArg $repo $branch) { return $null }
    $was = $env:GIT_TERMINAL_PROMPT; $env:GIT_TERMINAL_PROMPT = '0'
    try { $o = & git ls-remote (Get-HubMirrorUrl $hubAddr $repo) "refs/heads/$branch" 2>$null }
    catch { return $null }
    finally { if ($null -eq $was) { Remove-Item Env:GIT_TERMINAL_PROMPT -ErrorAction SilentlyContinue } else { $env:GIT_TERMINAL_PROMPT = $was } }
    if ($LASTEXITCODE -ne 0) { return $null }
    $l = @($o | ForEach-Object { "$_" } | Where-Object { $_ -match "^[0-9a-f]{40}\s+refs/heads/$([regex]::Escape($branch))$" } | Select-Object -First 1)
    if ($l.Count) { ($l[0] -split '\s+')[0] } else { $null }
}

# Get-PackTar is bsdtar where Windows ships it, since the GNU tar a Windows bash puts first on the PATH reads C:\x as a host.
function Get-PackTar { $w = Join-Path $env:SystemRoot 'System32\tar.exe'; if ($IsWindows -ne $false -and (Test-Path -LiteralPath $w)) { $w } else { 'tar' } }

# New-PackSource clones the hub's mirror of the pack's repository, WHOLE (the hub serves whole fetches only), and tars it WITH
# its .git, because the room's `atrium room setup --pack-dir` asks the checkout for its commit. The checkout on this machine is
# never read. Returns Ok, Why, Commit and Tgz (<OutDir>/pack-src.tgz).
function New-PackSource {
    param([string] $HubAddr, [string] $Repo, [string] $Branch, [string] $OutDir)
    $fail = { param($why) [pscustomobject]@{ Ok = $false; Why = $why; Commit = $null; Tgz = $null } }
    $bad = Test-PackArg $Repo $Branch
    if ($bad) { return (& $fail $bad) }
    $url = Get-HubMirrorUrl $HubAddr $Repo
    $src = Join-Path $OutDir 'pack-src'
    if (Test-Path -LiteralPath $src) { Remove-Item -LiteralPath $src -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
    $was = $env:GIT_TERMINAL_PROMPT; $env:GIT_TERMINAL_PROMPT = '0'
    try { $o = & git clone -q -b $Branch -- $url $src 2>&1 }
    catch { $o = @("$_"); $global:LASTEXITCODE = 1 }
    finally { if ($null -eq $was) { Remove-Item Env:GIT_TERMINAL_PROMPT -ErrorAction SilentlyContinue } else { $env:GIT_TERMINAL_PROMPT = $was } }
    if ($LASTEXITCODE -ne 0) { return (& $fail "could not fetch $Branch of $url from the hub's mirror: $((@($o) | Select-Object -First 1))") }
    $commit = (& git -C $src rev-parse HEAD 2>$null | Select-Object -First 1)
    $tgz = Join-Path $OutDir 'pack-src.tgz'
    if (Test-Path -LiteralPath $tgz) { Remove-Item -LiteralPath $tgz -Force }
    $t = & (Get-PackTar) -czf $tgz -C $src . 2>&1
    if ($LASTEXITCODE -ne 0) { return (& $fail "tar could not pack the checkout: $((@($t) | Select-Object -First 1))") }
    # claude always; codex and gemini only when the repository has a folder of theirs with agents or skills in it
    $runners = @('claude')
    foreach ($rn in 'codex', 'gemini') {
        if ((Test-Path -LiteralPath (Join-Path $src "$rn/agents")) -or (Test-Path -LiteralPath (Join-Path $src "$rn/skills"))) { $runners += $rn }
    }
    [pscustomobject]@{ Ok = $true; Why = $null; Commit = $commit; Tgz = $tgz; Runners = $runners }
}

# Get-PackUnpackScript unpacks the tarball Copy-ToRemote put at ~/.atrium/provision/pack-src.tgz into ~/.atrium/provision/pack-src,
# fresh. Windows needs the tar that ships with it (Windows 10 1803 and later), Unix needs tar. Prints unpack=ok or unpack=fail.
function Get-PackUnpackScript {
    param([string] $Os)
    if ($Os -eq 'windows') {
        return @'
$dir = Join-Path $HOME '.atrium\provision\pack-src'; $tgz = "$dir.tgz"
Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$ErrorActionPreference = 'Continue'
$tarx = Join-Path $env:SystemRoot 'System32\tar.exe'; if (-not (Test-Path -LiteralPath $tarx)) { $tarx = 'tar.exe' }
& $tarx -xzf $tgz -C $dir 2>&1 | ForEach-Object { "tar=$_" }
if ($LASTEXITCODE -eq 0) { 'unpack=ok' } else { 'unpack=fail'; exit 1 }
'@
    }
    @'
dir="$HOME/.atrium/provision/pack-src"; tgz="$dir.tgz"
rm -rf "$dir"; mkdir -p "$dir"
if tar -xzf "$tgz" -C "$dir"; then echo unpack=ok; else echo unpack=fail; rm -rf "$dir" "$tgz"; exit 1; fi
'@
}
