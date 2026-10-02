# The toolchain for a room, run from THIS side over ssh: go, node, and on Windows a real git and pwsh 7.
#
#   pwsh -File scripts\room-toolchain.ps1 m1mini -Check
#   pwsh -File scripts\room-toolchain.ps1 user@host
#   pwsh -File scripts\room-toolchain.ps1 user@host -Tools go,node -Prefix ~/tools
#   pwsh -File scripts\room-toolchain.ps1 local -Check          # this machine, no ssh
#   pwsh -File scripts\room-toolchain.ps1 user@host -Profile c -Check   # also the C toolchain, see C PROFILE below
#
# A worker on a room needs go and node to build atrium and run go test, and on Windows a real Git for Windows and pwsh
# 7 (the daemon tests run git and pwsh). This installs them into the REMOTE USER'S HOME. No admin, no sudo, no package
# manager. Run it again and it changes only what is not already right.
#
# WHAT IT DOES, per tool
#   1. Looks for a good-enough version THE WAY THE ROOM WILL SEE IT (see PATH below). If there is one the step is `ok`
#      and nothing is installed or overwritten.
#   2. Otherwise fetches the official archive on the remote, checks its sha256 against the publisher's own list, and
#      unpacks it under -Prefix. The lists are read on THIS side, and the remote computes the hash it downloaded:
#        go     https://go.dev/dl/?mode=json               (sha256 per file)
#        node   https://nodejs.org/dist/<v>/SHASUMS256.txt
#        git    the git-for-windows GitHub release: PortableGit-<v>-64-bit.7z.exe, its asset digest
#        pwsh   the PowerShell GitHub release: PowerShell-<v>-win-<arch>.zip, its asset digest or hashes.sha256
#   3. Records the directories to put in front of PATH, and puts that record where the room's start reads it.
#
# WHAT IS GOOD ENOUGH
#   go    >= what go.mod needs (read from the checkout this script is in), or -GoVersion when there is no go.mod
#   node  the same major as -NodeVersion or newer
#   git   Windows only: Git FOR WINDOWS (`git --version` says .windows.), 2.39 or newer. A Cygwin or MSYS git is not
#         one, and breaks the daemon tests, so it counts as missing
#   pwsh  Windows only: the same major as -PwshVersion or newer
#
# WHERE THINGS GO. -Prefix, default ~/.local/share/atrium-tools, holds <prefix>/go, node, git (Windows), pwsh
# (Windows). Versions are parameters, -GoVersion (default: what go.mod says, else 1.26.2), -NodeVersion v24.21.0,
# -GitVersion 2.56.0, -PwshVersion 7.6.6. A directory that is already at the destination and is NOT a working install
# is never replaced unless -Force is given.
#
# PATH. The room reads its PATH once, when it starts, so what this records only reaches a room that is STARTED after.
# The record is ~/.atrium/toolchain/path.txt, one directory per line, and this script only ever ADDS to it. What the
# room's start reads it through:
#   macOS, Linux  ~/.atrium/toolchain/path.sh is written from the record, and ONE line is added to the login profile
#                 the room's login shell reads (zsh .zprofile, bash .bash_profile or .profile):
#                     [ -r "$HOME/.atrium/toolchain/path.sh" ] && . "$HOME/.atrium/toolchain/path.sh" # added by atrium room-toolchain
#                 The line is added once and never rewritten, path.sh carries the changes. A systemd USER unit reads
#                 no profile. It gets the login shell's PATH from scripts/atrium-service.sh at INSTALL time, so run
#                 this before `provision-room.ps1 -Autostart`, or rerun scripts/atrium-service.sh install afterwards.
#   Windows       a user Path entry can never win: the machine Path comes first, and a machine Path with Cygwin on it
#                 has a git and bash that break tests. So the user and machine Path are NOT touched. The room has to
#                 be STARTED with the record in front, through ~\.atrium\toolchain\room-env.ps1, which this writes:
#                     . (Join-Path $HOME '.atrium\toolchain\room-env.ps1')     # prepends the record to $env:Path
#                     & $Bin room --detach
#                 scripts/atrium-autostart.ps1 does that for its logon task when room-env.ps1 exists. The one-line
#                 hook for provision-room.ps1's start step is in docs/changes/fabric-1-toolchain.md.
#                 A SESSION'S Bash tool is a login bash (Cygwin's, on a room that has it), and that profile puts
#                 Cygwin's git ahead of the record, which cannot read a C:/ worktree path, and sets TMP to Cygwin's own
#                 /tmp. So a block in ~/.bash_profile, between `# >>> atrium toolchain` and `# <<< atrium toolchain <<<`,
#                 puts the record first again and points TMP at the user's own Temp. A login bash reads only the first of
#                 .bash_profile, .bash_login and .profile, so the block goes in whichever of them exists first, and a new
#                 .bash_profile is made only when none does (a new one would hide an existing .profile). It is replaced
#                 in place when this script changes it, and a session reads it at its start, so no room restart. The
#                 block also sets TMP and TEMP to the user's Temp: Cygwin points them at its own /tmp, where a Windows
#                 git cannot make a directory, and go test lives in TMP.
# A room that is running already has the old PATH. This never restarts it. When the room answers on 7781 after a
# change, a `restart warn` line says so.
#
# -CHECK reports every step and writes nothing on the remote. It still asks the publishers, so a missing archive shows.
# `local` as the target runs the SAME remote payloads on this machine, through a local `powershell -EncodedCommand` on
# Windows or `sh -s` elsewhere, instead of ssh.
#
# THE REMOTE runs plain `sh -s` on Unix (no pwsh needed) and Windows PowerShell 5.1 with -EncodedCommand on Windows,
# gzipped inside it so the payload stays under the 7800 characters cmd.exe allows, the way provision-room.ps1 and
# room-git.ps1 do.
#
# ONE LINE PER STEP, the same shape as provision-room.ps1, prefixed room-toolchain:
#
#   room-toolchain <step> <status> <detail>
#
# status is ok (already right), done (changed now), skip, warn or fail. With -Check a tool that is not good enough is a
# `warn` that says what would be installed. The last line is `room-toolchain done ok` or `room-toolchain done fail <code>`.
#
# EXIT CODES
#   0  done, or -Check finished (a tool that would be installed is a warn, not a failure)
#   1  a local problem: bad arguments, or a publisher's list or asset could not be read
#   2  ssh could not reach the target, or its OS or arch is not one this covers
#   3  an install step failed on the remote: download, unpack, or the result does not answer
#   4  the sha256 of a download did not match the publisher's. Nothing was unpacked
#   5  installed, but the PATH record could not be written, or the tool is still not reachable the way the room sees it
#   6  the run finished and nothing failed, but a person has to do something (-Profile c only, see C PROFILE)
#
# C PROFILE. `-Profile c` (default none: with none every call behaves as it did before) adds what a room needs to build
# openziti/ziti-sdk-c with MSYS2 mingw, vcpkg and CMake, all in the install and nothing by hand. The code is in
# scripts/room-toolchain-c.ps1. It is opt-in per room, and the tools above are still installed as they were (git and pwsh
# stay: git is Git for Windows, which the C steps need, so -Tools is widened to hold it). A room that only builds C
# can say `-Tools git,pwsh` to leave go and node out. Steps, in the order they run (Windows):
#   msys2          MSYS2 is looked for at -Msys2Dir (when given, that is the only place), then C:\msys64, <Prefix>\msys64 and
#                  the directory above any mingw64\bin on the PATH record. One that is there is used as is and only its gaps
#                  are filled. Otherwise the official base archive msys2-base-x86_64-<newest>.sfx.exe is fetched from
#                  repo.msys2.org, checked against the sha256 the msys2/msys2-installer release of that date publishes (a
#                  mismatch is exit 4 and nothing is unpacked) and unpacked to -Msys2Dir, default <Prefix>\msys64. A fresh
#                  one gets the first start and `pacman -Syuu` twice. -Msys2Version 20260927 pins the date.
#   gcc cmake ninja pkgconf openssl   mingw-w64-x86_64-toolchain, -cmake, -ninja, -openssl and -pkgconf, added with
#                  `pacman -S --needed` when any of the five is missing. pkgconf is the package with mingw64\bin\pkg-config.exe
#                  and openssl the one with libssl.a and include/openssl/ssl.h, which the preset below points at.
#   msys2-acl      -RunnerAccounts (default claude, plus localai when that local user exists) get RX on the MSYS2 directory
#                  when they cannot read it already. The user running pacman gets Modify ONLY while pacman runs and only when
#                  it cannot write, and it is taken back after, to what it was. Never Everyone or Users. When icacls is
#                  refused the step is NEEDS-HUMAN and prints the command for an admin.
#   path           mingw64\bin is added to the PATH record, after Git for Windows. NOT usr\bin: MSYS2's own git and bash must
#                  not shadow Git for Windows. The record is per user, so for another runner account run this as that user
#                  too (the step says so). The user and machine Path are not touched.
#   git-identity   user.name and user.email come only from -GitUserName and -GitUserEmail, never invented and never copied.
#                  Ones already in the global config are left alone. Missing and not given is NEEDS-HUMAN with the commands.
#   git-credential Git Credential Manager (it ships in the PortableGit this installs) is the helper NAME, set with
#                  `credential.helper manager` when no config has it. This script never takes, stores or prints a token. It
#                  checks access with `git ls-remote` (no prompt, 45 s) on https://github.com/openziti/ziti-sdk-c.git and, when
#                  -CheckRepo owner/repo or an https URL is given, on that too. When access is missing it is NEEDS-HUMAN with
#                  the one command a person runs once, in an interactive session as the room's user (not ssh).
#                  -GitHubOwners (default dovholuknf,openziti) are the accounts -CheckRepo may name.
#   vcpkg          `git clone https://github.com/microsoft/vcpkg` to -VcpkgDir (default <home>\vcpkg), then
#                  `bootstrap-vcpkg.bat -disableMetrics`, then `vcpkg version` and the x64-mingw-static triplet file are checked.
#   triplet        triplets\community\x64-mingw-static.cmake is there
#   sdk-checkout   -SdkDir (default <home>\git\github\openziti\ziti-sdk-c) is cloned from github.com/openziti/ziti-sdk-c when
#                  absent. A git checkout there is left alone (branch and clean or dirty are reported). A directory that is
#                  not a git checkout is never replaced: the step fails with the path. Submodules are initialised if it has any.
#   cmake-preset   CMakeUserPresets.json in the checkout root with the hidden `mingw-vcpkg-base` (inherits ci-windows-x64-mingw,
#                  host and target triplet x64-mingw-static, VCPKG_ROOT, the MSYS2 openssl and pkg-config, mingw64\bin first on
#                  PATH, Debug), `cwdming` (binaryDir build/cwdming) and `cwdming-with-tests`. Presets already in the file are
#                  never changed, missing ones are added and the rest of the file is kept. A file that is not valid JSON is left
#                  alone and is NEEDS-HUMAN. -VcpkgBinaryCache dir (default none) sets VCPKG_BINARY_SOURCES.
# -Check with the profile prints one line per step above with the path and version, or MISSING and what a run would do, and
# writes nothing (the git credential check still asks github.com). On macOS and Linux the profile only reports cc, gcc, clang,
# cmake, ninja, git, the vcpkg directory and the checkout, and a run installs nothing for it.
# A step that needs a person is `needs-human`, and the run ends with a block, one command per line:
#   room-toolchain needs-human <command>
# EXIT CODE 6 is that: the run finished and nothing failed, but an admin, an interactive login or a git identity is needed. It
# is used only when none of 1..5 applies. -Check never ends with 6.
#
# SCOPE. Windows x64 and arm64, macOS arm64 and x86_64, Linux x86_64 and aarch64.

param(
    # The ssh destination, or `local` for this machine.
    [Parameter(Position = 0)] [string] $Target,
    # Report every step, write nothing.
    [switch] $Check,
    # Which tools. git and pwsh apply to Windows only.
    [string[]] $Tools = @('git', 'pwsh', 'go', 'node'),
    # Where new installs go, as the REMOTE sees it. A leading ~ is the remote home.
    [string] $Prefix,
    [string] $GoVersion,
    [string] $NodeVersion = 'v24.21.0',
    [string] $GitVersion = '2.56.0',
    [string] $PwshVersion = '7.6.6',
    # Replace a directory at the destination that is not a working install.
    [switch] $Force,
    [string] $Ssh = 'ssh',
    [string[]] $SshOption = @(),
    # A test hook: corrupt the expected hash, to prove a mismatch fails cleanly.
    [switch] $TestBadHash,
    # A test hook: where the PATH record and room-env.ps1 go. Default ~/.atrium/toolchain.
    [string] $StateDir,
    # none (default) changes nothing. c adds the C build toolchain, see C PROFILE.
    [ValidateSet('none', 'c')] [string] $Profile = 'none',
    [string] $Msys2Dir,
    [string] $Msys2Version,
    [string[]] $RunnerAccounts = @('claude', 'localai'),
    [string] $GitUserName,
    [string] $GitUserEmail,
    [string[]] $GitHubOwners = @('dovholuknf', 'openziti'),
    # owner/repo or an https URL, checked for access when given. Default none: only the public repo is checked.
    [string] $CheckRepo,
    [string] $VcpkgDir,
    [string] $SdkDir,
    [string] $VcpkgBinaryCache,
    # A test hook: print the encoded size of every Windows payload and exit.
    [switch] $PayloadSizes
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
. (Join-Path $PSScriptRoot 'room-toolchain-c.ps1')
# UTF-8 without a BOM for what is piped to ssh. See provision-room.ps1.
$OutputEncoding = [Text.UTF8Encoding]::new($false)
# `pwsh -File` hands `-Tools go,node` and `-SshOption -o,Port=2222` over as one string, so commas split.
function Split-List { param($v) @($v | ForEach-Object { "$_" -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ }) }
$Tools = Split-List $Tools
$SshOption = Split-List $SshOption
$RunnerAccounts = Split-List $RunnerAccounts
$GitHubOwners = Split-List $GitHubOwners
$NodeVersion = if ($NodeVersion.StartsWith('v')) { $NodeVersion } else { "v$NodeVersion" }

function Step {
    param([string] $step, [string] $status, [string] $detail = '')
    $line = "room-toolchain $step $status"
    if ($detail) { $line += " $detail" }
    Write-Host $line
}
$script:needs = @()   # what a person has to do, one command per item (-Profile c)
function Finish {
    param([int] $code)
    foreach ($l in (Format-NeedsHuman $script:needs)) { Write-Host $l }
    if ($code -eq 0) { Step 'done' 'ok' } elseif ($code -eq 6) { Step 'done' 'needs-human' '6' } else { Step 'done' 'fail' "$code" }
    exit $code
}
function Fail {
    param([string] $step, [int] $code, [string] $detail, $output)
    Step $step 'fail' $detail
    if ($output) { $output | ForEach-Object { Write-Host "    $_" } }
    Finish $code
}

if (-not $Target) {
    Write-Host 'usage: room-toolchain.ps1 <user@host|local> [-Check] [-Tools go,node,git,pwsh] [-Prefix dir] [-Force]'
    Write-Host '                          [-GoVersion 1.26.2] [-NodeVersion v24.21.0] [-GitVersion 2.56.0] [-PwshVersion 7.6.6]'
    Write-Host '                          [-Profile c [-Msys2Dir dir] [-GitUserName n -GitUserEmail e] [-VcpkgDir dir] [-SdkDir dir] [-CheckRepo owner/repo]]'
    exit 1
}
$allTools = 'git', 'pwsh', 'go', 'node'
foreach ($t in $Tools) { if ($t -notin $allTools) { Fail 'args' 1 "unknown tool '$t'. the tools are $($allTools -join ', ')" } }
$Tools = @($allTools | Where-Object { $_ -in $Tools })
foreach ($v in @(@('-NodeVersion', $NodeVersion), @('-GitVersion', $GitVersion), @('-PwshVersion', $PwshVersion), @('-GoVersion', $GoVersion))) {
    if ($v[1] -and $v[1] -notmatch '^v?\d+\.\d+(\.\d+)?$') { Fail 'args' 1 "bad $($v[0]) '$($v[1])'" }
}
$isLocal = $Target -eq 'local'

# What go.mod asks for, from the checkout this script sits in. The room has no checkout to read it from yet.
$goMin = $null
$gomod = Join-Path (Split-Path -Parent $PSScriptRoot) 'go.mod'
if (Test-Path -LiteralPath $gomod) {
    $m = Select-String -LiteralPath $gomod -Pattern '^go\s+(\d+\.\d+(\.\d+)?)\s*$' | Select-Object -First 1
    if ($m) { $goMin = $m.Matches[0].Groups[1].Value }
}
if (-not $GoVersion) { $GoVersion = if ($goMin) { $goMin } else { '1.26.2' } }
if (-not $goMin) { $goMin = $GoVersion }

# ── talking to the remote ───────────────────────────────────────────────────

$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $(if ($Profile -eq 'c') { @('-o', 'ServerAliveInterval=30') } else { @() }) + $SshOption

function ConvertFrom-KeyValue {
    param($lines)
    $h = @{}
    foreach ($l in $lines) {
        $s = "$l"
        $i = $s.IndexOf('=')
        if ($i -gt 0) { $h[$s.Substring(0, $i).Trim()] = $s.Substring($i + 1).TrimEnd() }
    }
    $h
}

$script:remoteOS = $null   # windows | unix
$script:kind = $null       # windows | mac | linux

# Invoke-Remote runs one script on the remote, or here for `local`. Windows gets -EncodedCommand, whose payload is the
# script gzipped, so cmd.exe's command line limit is not the script's limit. Unix gets the script on stdin to `sh -s`,
# so nothing in it meets the login shell's quoting.
function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $enc = New-EncodedCommand $script
        if ($enc.Length -gt $script:EncodedLimit) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        if ($isLocal) {
            $out = & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc 2>&1
        } else {
            $out = & $Ssh @sshBase $Target "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
        }
    } else {
        # The PATH a login shell has, so what the room finds is what is found here. A COMMENT LAST, because PowerShell
        # ends what it pipes to a native command with CRLF, and `fi` followed by a carriage return is not `fi`.
        $full = "lp=`$(`"`${SHELL:-/bin/sh}`" -lc 'printf %s `"`$PATH`"' 2>/dev/null </dev/null); [ -n `"`$lp`" ] && PATH=`"`$lp`"`n" + $script
        $full = ($full -replace "`r", '') + "`n#"
        $out = if ($isLocal) { $full | & sh -s 2>&1 } else { $full | & $Ssh @sshBase $Target 'sh -s' 2>&1 }
    }
    $code = $LASTEXITCODE
    # Windows PowerShell writes a CLIXML preamble to stderr for some hosts. It is noise here.
    $lines = @($out | ForEach-Object { "$_" } | Where-Object { $_ -notmatch '^#< CLIXML|^<Objs |^</Objs>' })
    [pscustomobject]@{ Out = $lines; Code = $code }
}

# ── the payloads ────────────────────────────────────────────────────────────
#
# One script per OS, and an $Act that says which part runs. Every result is a key=value line. What is good enough is
# decided on THIS side, so the payloads only report what they find.

# Windows. The room's view of PATH is the record, then the machine Path, then the user Path: what the room's start
# builds. Version probes run with that PATH too, because git finds its children on it.
$winPayload = @'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$T = if ($StateDir) { $StateDir -replace '/', '\' } else { Join-Path $HOME '.atrium\toolchain' }; $PF = Join-Path $T 'path.txt'; $EF = Join-Path $T 'room-env.ps1'
if (-not $Prefix) { $Prefix = Join-Path $HOME '.local\share\atrium-tools' }
elseif ($Prefix -eq '~' -or $Prefix -match '^~[\\/]') { $Prefix = $HOME + $Prefix.Substring(1) }
$Prefix = $Prefix -replace '/', '\'
function Rec { if (Test-Path -LiteralPath $PF) { @(Get-Content -LiteralPath $PF | Where-Object { $_.Trim() }) } else { @() } }
function Dirs { $x = @(Rec); foreach ($s in 'Machine', 'User') { $x += @([Environment]::GetEnvironmentVariable('Path', $s) -split ';') }
    @($x | Where-Object { $_ } | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_.Trim('"')) }) }
function First([string]$n) { foreach ($d in Dirs) { $p = Join-Path $d "$n.exe"; if (Test-Path -LiteralPath $p -PathType Leaf) { return $p } } }
function Bp { foreach ($n in '.bash_profile', '.bash_login', '.profile') { $p = Join-Path $HOME $n; if (Test-Path -LiteralPath $p) { return $p } }; Join-Path $HOME '.bash_profile' }
function Raw($t, $exe) {
    $p = $env:Path; $env:Path = (@(Split-Path -Parent $exe) + (Dirs)) -join ';'
    try { $a = @{ go = 'version'; node = '--version'; git = '--version'; pwsh = '--version' }[$t]
        $o = & $exe $a 2>&1 | Out-String; ($o -split "`n" | Where-Object { $_.Trim() } | Select-Object -First 1).Trim() }
    catch { "error: $($_.Exception.Message)" } finally { $env:Path = $p }
}
function Tell { param($tools)
    foreach ($t in $tools) { $e = First $t; "$t=$(if ($e) { "$e|$(Raw $t $e)" } else { '|' })"; "dest.$t=$(Test-Path -LiteralPath (Join-Path $Prefix $t))"
        $a = Join-Path $Prefix (@{ go = 'go\bin'; node = 'node'; git = 'git\cmd'; pwsh = 'pwsh' }[$t]); $a = Join-Path $a "$t.exe"; if (Test-Path -LiteralPath $a) { "at.$t=$a|$(Raw $t $a)" } } }
#@start
#@probe
    "arch=$(if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE })"
    "prefix=$Prefix"; "rec=$((Rec) -join ';')"; "hook=$(Test-Path -LiteralPath $EF)"
    $BP = Bp; "bashhook=$([bool]((Test-Path -LiteralPath $BP) -and (Select-String -LiteralPath $BP -SimpleMatch '>>> atrium toolchain v2' -Quiet)))"
    Tell ($Names -split ',')
    try { $null = Invoke-WebRequest 'http://127.0.0.1:7781/v1/health' -UseBasicParsing -TimeoutSec 3; 'room=up' } catch { 'room=down' }
#@install
    $Dest = Join-Path $Prefix $Tool
    if (Test-Path -LiteralPath $Dest) {
        if (-not $Force) { "err=$Dest is already there and is not a working $Tool. look at it, or rerun with -Force to replace it"; "rc=3"; exit 3 }
    }
    $dl = Join-Path $Prefix '.downloads'; New-Item -ItemType Directory -Force -Path $dl | Out-Null
    $f = Join-Path $dl $File
    try { Invoke-WebRequest -Uri $Url -OutFile $f -UseBasicParsing } catch { "err=download of $Url failed: $($_.Exception.Message)"; "rc=3"; exit 3 }
    $got = (Get-FileHash -LiteralPath $f -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $Sha) { Remove-Item -LiteralPath $f -Force; "err=sha256 of $File is $got, the publisher says $Sha. nothing was unpacked"; "rc=4"; exit 4 }
    "sha=$got"
    if (Test-Path -LiteralPath $Dest) { Remove-Item -LiteralPath $Dest -Recurse -Force }
    $tmp = "$Dest.tmp"; if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force }
    try {
        if ($Kind -eq 'sfx') {
            $p = Start-Process -FilePath $f -ArgumentList "-o`"$tmp`"", '-y' -Wait -PassThru -WindowStyle Hidden
            if ($p.ExitCode -ne 0) { throw "the self-extractor exited $($p.ExitCode)" }
        } else {
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            [IO.Compression.ZipFile]::ExtractToDirectory($f, $tmp)
        }
        $src = $tmp
        if ($Strip -eq '1') { $kids = @(Get-ChildItem -LiteralPath $tmp); if ($kids.Count -eq 1 -and $kids[0].PSIsContainer) { $src = $kids[0].FullName } }
        Move-Item -LiteralPath $src -Destination $Dest
    } catch { "err=could not unpack $File`: $($_.Exception.Message)"; "rc=3"; exit 3 }
    finally { if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }; Remove-Item -LiteralPath $f -Force -ErrorAction SilentlyContinue }
    "installed=$Dest"
#@record
    New-Item -ItemType Directory -Force -Path $T | Out-Null
    $new = @($NewDirs -split ';' | Where-Object { $_ }); $old = @(Rec)
    $all = @(@($new) + @($old | Where-Object { $_ -notin $new }))
    $body = ($all -join "`r`n") + "`r`n"
    if (-not (Test-Path -LiteralPath $PF) -or (Get-Content -LiteralPath $PF -Raw) -ne $body) { [IO.File]::WriteAllText($PF, $body); 'pathfile=changed' } else { 'pathfile=same' }
    $env1 = "# written by room-toolchain.ps1. Dot-source this before `atrium room --detach`: it puts what that script installed in FRONT of PATH,`r`n" +
        "# which a user Path entry cannot do, because the machine Path comes first.`r`n" +
        "`$f = Join-Path `$PSScriptRoot 'path.txt'`r`n" +
        "if (Test-Path -LiteralPath `$f) { `$d = @(Get-Content -LiteralPath `$f | Where-Object { `$_.Trim() }); if (`$d) { `$env:Path = (`$d -join ';') + ';' + `$env:Path } }`r`n"
    if (-not (Test-Path -LiteralPath $EF) -or (Get-Content -LiteralPath $EF -Raw) -ne $env1) { [IO.File]::WriteAllText($EF, $env1); 'hook=changed' } else { 'hook=same' }
    "envfile=$EF"
    $BP = Bp
    $blk = (@('# >>> atrium toolchain v2 (room-toolchain.ps1) >>>',
        ('f=$(cygpath -u ''' + ($PF -replace "'", "'\''") + ''' 2>/dev/null)'),
        'if [ -r "$f" ]; then',
        '  pre=''''',
        '  while IFS= read -r d; do d=$(cygpath -u "${d%$''\r''}" 2>/dev/null); [ -n "$d" ] && pre="$pre$d:"; done < "$f"',
        '  PATH="$pre$PATH"',
        'fi',
        't=$(cygpath -u "$LOCALAPPDATA\\Temp" 2>/dev/null); [ -d "$t" ] && export TMP="$t" TEMP="$t"',
        'unset f pre d t',
        '# <<< atrium toolchain <<<') -join "`n")
    $cur = if (Test-Path -LiteralPath $BP) { (Get-Content -LiteralPath $BP -Raw) -replace "`r`n", "`n" } else { '' }
    $rx = '(?s)# >>> atrium toolchain.*?# <<< atrium toolchain <<<\n?'
    $next = if ($cur -match $rx) { [regex]::Replace($cur, $rx, [Text.RegularExpressions.MatchEvaluator]{ param($m) $blk + "`n" }) } else { $cur + $(if ($cur -and -not $cur.EndsWith("`n")) { "`n" }) + $blk + "`n" }
    if ($next -ne $cur) { [IO.File]::WriteAllText($BP, $next); 'bashprofile=changed' } else { 'bashprofile=same' }
#@verify
Tell ($Names -split ',')
'@

# Unix. The room's view of PATH is the login shell's, which the wrapper in Invoke-Remote already put in PATH.
$unixPayload = @'
if [ -n "$STATEDIR" ]; then T="$STATEDIR"; else T="$HOME/.atrium/toolchain"; fi; PF="$T/path.txt"; PS="$T/path.sh"
[ -n "$PREFIX" ] || PREFIX="$HOME/.local/share/atrium-tools"
case "$PREFIX" in '~') PREFIX="$HOME";; '~/'*) PREFIX="$HOME${PREFIX#\~}";; esac
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1; else openssl dgst -sha256 "$1" | sed 's/.*= *//'; fi; }
profile() { case "${SHELL##*/}" in zsh) echo "$HOME/.zprofile";; bash) if [ -f "$HOME/.bash_profile" ]; then echo "$HOME/.bash_profile"; else echo "$HOME/.profile"; fi;; *) echo "$HOME/.profile";; esac; }
tell() {
  for t in $(echo "$1" | tr ',' ' '); do
    p=$(command -v "$t" 2>/dev/null) || p=; v=
    if [ -n "$p" ]; then
      case $t in go) v=$("$p" version 2>&1);; node) v=$("$p" --version 2>&1);; esac
      v=$(printf '%s\n' "$v" | head -1)
    fi
    echo "$t=$p|$v"
    case $t in go) sub=go/bin;; *) sub=node/bin;; esac
    if [ -x "$PREFIX/$sub/$t" ]; then a=$(PATH="$PREFIX/$sub:$PATH" "$PREFIX/$sub/$t" $(if [ $t = go ]; then echo version; else echo --version; fi) 2>&1 | head -1); echo "at.$t=$PREFIX/$sub/$t|$a"; fi
    if [ -e "$PREFIX/$t" ]; then echo "dest.$t=True"; else echo "dest.$t=False"; fi
  done
}
case "$ACT" in
probe)
  echo "prefix=$PREFIX"; echo "rec=$([ -r "$PF" ] && tr '\n' ';' < "$PF")"
  f=$(profile); echo "profile=$f"
  if grep -qs 'atrium/toolchain/path.sh' "$f"; then echo hook=True; else echo hook=False; fi
  tell "$NAMES"
  if command -v curl >/dev/null 2>&1 && curl -fsS -m 3 http://127.0.0.1:7781/v1/health >/dev/null 2>&1; then echo room=up; else echo room=down; fi;;
install)
  D="$PREFIX/$TOOL"
  if [ -e "$D" ]; then
    if [ "$FORCE" = 1 ]; then :; else echo "err=$D is already there and is not a working $TOOL. look at it, or rerun with -Force to replace it"; echo rc=3; exit 3; fi
  fi
  mkdir -p "$PREFIX/.downloads" || { echo rc=3; exit 3; }
  F="$PREFIX/.downloads/$FILE"
  if command -v curl >/dev/null 2>&1; then curl -fsSL --connect-timeout 20 --max-time 600 -o "$F" "$URL" || { echo "err=download of $URL failed"; rm -f "$F"; echo rc=3; exit 3; }
  elif command -v wget >/dev/null 2>&1; then wget -q -O "$F" "$URL" || { echo "err=download of $URL failed"; rm -f "$F"; echo rc=3; exit 3; }
  else echo "err=neither curl nor wget is here to download with"; echo rc=3; exit 3; fi
  got=$(sha "$F")
  if [ "$got" != "$SHA" ]; then rm -f "$F"; echo "err=sha256 of $FILE is $got, the publisher says $SHA. nothing was unpacked"; echo rc=4; exit 4; fi
  echo "sha=$got"
  rm -rf "$D"
  tmp="$D.tmp"; rm -rf "$tmp"; mkdir -p "$tmp" || { echo rc=3; exit 3; }
  if tar -xzf "$F" -C "$tmp"; then
    set -- "$tmp"/*
    if [ $# -eq 1 ] && [ -d "$1" ] && mv "$1" "$D"; then :; else echo "err=$FILE did not unpack to one folder"; rm -rf "$tmp" "$F"; echo rc=3; exit 3; fi
  else echo "err=could not unpack $FILE"; rm -rf "$tmp" "$F"; echo rc=3; exit 3; fi
  rm -rf "$tmp" "$F"
  echo "installed=$D";;
record)
  mkdir -p "$T" || { echo rc=5; exit 5; }
  { printf '%s\n' "$NEWDIRS" | tr ';' '\n'; cat "$PF" 2>/dev/null; } | awk 'NF && !seen[$0]++' > "$PF.tmp" || { echo rc=5; exit 5; }
  if cmp -s "$PF.tmp" "$PF" 2>/dev/null; then rm -f "$PF.tmp"; echo pathfile=same; else mv "$PF.tmp" "$PF"; echo pathfile=changed; fi
  { echo '# written by room-toolchain.ps1: what it installed, in front of PATH. Read by the login profile.'
    printf 'export PATH="%s$PATH"\n' "$(awk '{printf "%s:", $0}' "$PF")"; } > "$PS.tmp"
  if cmp -s "$PS.tmp" "$PS" 2>/dev/null; then rm -f "$PS.tmp"; else mv "$PS.tmp" "$PS"; fi
  f=$(profile)
  if grep -qs 'atrium/toolchain/path.sh' "$f"; then echo hook=same; else
    case "$T" in "$HOME"/*) tl="\$HOME${T#"$HOME"}";; *) tl="$T";; esac
    printf '\n%s\n' "[ -r \"$tl/path.sh\" ] && . \"$tl/path.sh\" # added by atrium room-toolchain" >> "$f" || { echo rc=5; exit 5; }
    echo hook=changed; fi
  echo "profile=$f";;
verify) tell "$NAMES";;
__CUNIX__esac
'@
$unixPayload = $unixPayload.Replace('__CUNIX__', $script:CUnixAct)

# Get-Payload injects this call's variables in front of the payload.
function Get-Payload {
    param([string] $act, [hashtable] $vars = @{})
    $vars['StateDir'] = $StateDir; $vars['Act'] = $act; $vars['Prefix'] = $Prefix; $vars['Names'] = ($Tools -join ',')
    $vars['Force'] = if ($Force) { '1' } else { '0' }
    if ($script:remoteOS -eq 'windows') {
        $head = ($vars.Keys | Sort-Object | ForEach-Object { "`$$_ = $(Quote-Ps "$($vars[$_])")" }) -join "`n"
        $head = $head -replace '\$Force = ''1''', '$Force = $true' -replace '\$Force = ''0''', '$Force = $false'
        # ONLY THE SECTION FOR THIS ACT, to keep the payload short. Sections start at a #@name line.
        $parts = [regex]::Split($winPayload, '(?m)^#@(\w+)\r?\n')
        $common = $parts[0]; $sec = ''
        for ($i = 1; $i -lt $parts.Count; $i += 2) { if ($parts[$i] -eq $act) { $sec = $parts[$i + 1] } }
        $head + "`n" + $common + $sec
    } else {
        $head = ($vars.Keys | Sort-Object | ForEach-Object { $n = $_.ToUpper(); "$n=$(Quote-Sh "$($vars[$_])")" }) -join "`n"
        $head + "`n" + $unixPayload
    }
}

# A test hook: the encoded size of every Windows payload with values as long as a real call makes them, then exit.
if ($PayloadSizes) {
    $script:remoteOS = 'windows'
    $longDir = 'C:\Users\a-long-user-name\' + ('abcdef0123' * 12)
    $gv = @{ Tool = 'git'; Url = 'https://github.com/git-for-windows/git/releases/download/v2.56.0.windows.1/PortableGit-2.56.0-64-bit.7z.exe'
        File = 'PortableGit-2.56.0-64-bit.7z.exe'; Sha = ('0123456789abcdef' * 4); Kind = 'sfx'; Strip = '0'; NewDirs = ((1..4 | ForEach-Object { "$longDir$_" }) -join ';') }
    foreach ($a in 'probe', 'install', 'record', 'verify') { Write-Host "payload $a $((New-EncodedCommand (Get-Payload $a $gv.Clone())).Length)" }
    foreach ($a in ($script:CActs.Keys | Sort-Object)) { Write-Host "payload $a $((New-EncodedCommand (Get-CPayload $a (Get-CWorstVars $a))).Length)" }
    exit 0
}

# ── 1. reach the target and learn what it is ────────────────────────────────

if ($isLocal) {
    if ($IsWindows -or $env:OS -eq 'Windows_NT') { $script:remoteOS = 'windows'; $script:kind = 'windows' }
    else { $script:remoteOS = 'unix'; $script:kind = if ($IsMacOS) { 'mac' } else { 'linux' } }
    $where = 'this machine'
} else {
    $probe = & $Ssh @sshBase $Target 'uname -sm' 2>&1
    $code = $LASTEXITCODE
    if ($code -eq 255) { Fail 'ssh' 2 "cannot reach $Target over ssh" $probe }
    $text = ($probe | ForEach-Object { "$_" }) -join ' '
    if ($code -eq 0 -and $text -match '^(Linux|Darwin)\s+(\S+)') {
        $script:remoteOS = 'unix'
        $script:kind = if ($Matches[1] -eq 'Linux') { 'linux' } else { 'mac' }
    } else {
        $script:remoteOS = 'windows'; $script:kind = 'windows'
    }
    $where = $Target
}
$osArch = $null
if ($script:remoteOS -eq 'unix') {
    $r = if ($isLocal) { (& uname -m) } else { (& $Ssh @sshBase $Target 'uname -m' 2>&1) }
    $osArch = ("$r" -split "`n")[0].Trim()
}

# -Profile c: what the caller typed is checked before anything runs. The C steps need Git for Windows, so git is added to -Tools.
$cProfile = $Profile -eq 'c'
if ($cProfile -and $script:remoteOS -eq 'windows') {
    foreach ($pa in @(@('-Msys2Dir', $Msys2Dir), @('-VcpkgDir', $VcpkgDir), @('-SdkDir', $SdkDir), @('-VcpkgBinaryCache', $VcpkgBinaryCache))) {
        if ($pa[1]) { $why = Test-WinPathArg $pa[1] $pa[0]; if ($why) { Fail 'args' 1 $why } }
    }
    # a trailing separator would end a quoted argument with \" so it is dropped (a drive root is refused above)
    foreach ($n in 'Msys2Dir', 'VcpkgDir', 'SdkDir', 'VcpkgBinaryCache') { $cur = (Get-Variable $n).Value; if ($cur) { Set-Variable $n $cur.TrimEnd('\', '/') } }
    foreach ($a in $RunnerAccounts) { $why = Test-AccountArg $a; if ($why) { Fail 'args' 1 "-RunnerAccounts: $why" } }
    if ($GitUserName) { $why = Test-GitIdentityArg $GitUserName '-GitUserName'; if ($why) { Fail 'args' 1 $why } }
    if ($GitUserEmail) { $why = Test-GitIdentityArg $GitUserEmail '-GitUserEmail'; if ($why) { Fail 'args' 1 $why } }
    if ($Msys2Version -and $Msys2Version -notmatch '^\d{8}$') { Fail 'args' 1 "bad -Msys2Version '$Msys2Version', it is the archive's date like 20260927" }
    if ($CheckRepo) {
        $u = ConvertTo-RepoUrl $CheckRepo
        if (-not $u) { Fail 'args' 1 "bad -CheckRepo '$CheckRepo', it is owner/repo or an https URL with no user name or token in it" }
        if ($u -match '^https://github\.com/([^/]+)/' -and $Matches[1] -notin $GitHubOwners) { Fail 'args' 1 "-CheckRepo '$CheckRepo' is not under -GitHubOwners ($($GitHubOwners -join ', '))" }
    }
    if ('git' -notin $Tools) { $Tools = @('git') + $Tools; Step 'tools' 'ok' '-Profile c needs Git for Windows, so git is added to -Tools' }
}

# The first probe: for Windows it also says the arch.
$Tools = @($Tools | Where-Object { $script:remoteOS -eq 'windows' -or $_ -in 'go', 'node' })
if ($script:remoteOS -eq 'unix' -and -not $Tools -and -not $cProfile) { Step 'tools' 'skip' 'git and pwsh are installed on Windows only, and nothing else was asked for'; Finish 0 }
$r = Invoke-Remote (Get-Payload 'probe')
if ($r.Code -ne 0) { Fail 'ssh' 2 "$where is not Linux, macOS or Windows PowerShell, or the probe failed" $r.Out }
$kv = ConvertFrom-KeyValue $r.Out
if ($script:remoteOS -eq 'windows') { $osArch = $kv.arch }
$arch = switch -Regex ($osArch) { '^(x86_64|amd64|AMD64)$' { 'x64' } '^(aarch64|arm64|ARM64)$' { 'arm64' } default { $null } }
if (-not $arch) { Fail 'os' 2 "$($script:kind) on '$osArch', which this does not cover" }
Step 'ssh' 'ok' "$where ($($script:kind) $arch)"
$prefixR = $kv.prefix
$sep = if ($script:remoteOS -eq 'windows') { '\' } else { '/' }
Step 'prefix' 'ok' "$prefixR$(if ($Check) { ' (-Check, nothing is written)' })"

# ── 2. the publishers' lists, read here ─────────────────────────────────────

$ghHeaders = @{ 'User-Agent' = 'atrium-room-toolchain' }
if ($env:GITHUB_TOKEN) { $ghHeaders['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }

function Get-Release {
    param([string] $repo, [string] $tag)
    try { Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/tags/$tag" -Headers $ghHeaders -TimeoutSec 30 }
    catch { throw "no release $tag in $repo on GitHub ($($_.Exception.Message))" }
}

# Get-Asset is what to fetch and its expected sha256: url, file, sha, kind, strip.
function Get-Asset {
    param([string] $tool)
    $win = $script:kind -eq 'windows'
    switch ($tool) {
        'go' {
            $goos = @{ windows = 'windows'; mac = 'darwin'; linux = 'linux' }[$script:kind]
            $goarch = if ($arch -eq 'x64') { 'amd64' } else { 'arm64' }
            $list = Invoke-RestMethod -Uri 'https://go.dev/dl/?mode=json&include=all' -TimeoutSec 60
            $want = $GoVersion
            # 1.26 alone means the newest 1.26.x the publisher lists.
            if ($want -match '^\d+\.\d+$') {
                $want = @($list | Where-Object { $_.stable -and $_.version -match "^go$([regex]::Escape($want))(\.\d+)?$" } |
                    ForEach-Object { $_.version.Substring(2) } | Sort-Object { [version]$_ } -Descending)[0]
            }
            $rel = $list | Where-Object { $_.version -eq "go$want" } | Select-Object -First 1
            if (-not $rel) { throw "go.dev lists no go$want" }
            $ext = if ($win) { 'zip' } else { 'tar.gz' }
            $f = $rel.files | Where-Object { $_.os -eq $goos -and $_.arch -eq $goarch -and $_.kind -eq 'archive' -and $_.filename.EndsWith(".$ext") } | Select-Object -First 1
            if (-not $f) { throw "go$want has no $goos/$goarch .$ext" }
            [pscustomobject]@{ Url = "https://go.dev/dl/$($f.filename)"; File = $f.filename; Sha = $f.sha256; Kind = 'archive'; Strip = '1'; Version = $want }
        }
        'node' {
            $nos = @{ windows = 'win'; mac = 'darwin'; linux = 'linux' }[$script:kind]
            $file = "node-$NodeVersion-$nos-$arch." + $(if ($win) { 'zip' } else { 'tar.gz' })
            $sums = (Invoke-WebRequest -Uri "https://nodejs.org/dist/$NodeVersion/SHASUMS256.txt" -UseBasicParsing -TimeoutSec 60).Content
            $line = ($sums -split "`n" | Where-Object { $_ -match "^\s*([0-9a-f]{64})\s+\*?$([regex]::Escape($file))\s*$" } | Select-Object -First 1)
            if (-not $line) { throw "SHASUMS256.txt for $NodeVersion has no $file" }
            [pscustomobject]@{ Url = "https://nodejs.org/dist/$NodeVersion/$file"; File = $file; Sha = $Matches[1]; Kind = 'archive'; Strip = '1'; Version = $NodeVersion.TrimStart('v') }
        }
        'git' {
            $tag = if ($GitVersion -match 'windows') { "v$GitVersion" } else { "v$GitVersion.windows.1" }
            $rel = Get-Release 'git-for-windows/git' $tag
            $bare = $GitVersion -replace '\.windows\.\d+$', ''
            $file = if ($arch -eq 'x64') { "PortableGit-$bare-64-bit.7z.exe" } else { "PortableGit-$bare-arm64.7z.exe" }
            $a = $rel.assets | Where-Object { $_.name -eq $file } | Select-Object -First 1
            if (-not $a) { throw "release $tag has no $file" }
            $sha = if ($a.digest -match '^sha256:([0-9a-f]{64})$') { $Matches[1] }
                   elseif ("$($rel.body)" -match "$([regex]::Escape($file))\s*\|?\s*([0-9a-f]{64})") { $Matches[1] }
            if (-not $sha) { throw "release $tag publishes no sha256 for $file" }
            [pscustomobject]@{ Url = $a.browser_download_url; File = $file; Sha = $sha; Kind = 'sfx'; Strip = '0'; Version = $bare }
        }
        'msys2' {
            $rels = Invoke-RestMethod -Uri 'https://api.github.com/repos/msys2/msys2-installer/releases?per_page=12' -Headers $ghHeaders -TimeoutSec 30
            $sel = Select-Msys2Release $rels $Msys2Version
            if (-not $sel) { throw "the msys2/msys2-installer releases have no msys2-base-x86_64 .sfx.exe with a .sha256$(if ($Msys2Version) { " for $Msys2Version" })" }
            $r = Invoke-WebRequest -Uri $sel.ShaUrl -UseBasicParsing -Headers $ghHeaders -TimeoutSec 30
            $txt = if ($r.Content -is [byte[]]) { [Text.Encoding]::UTF8.GetString($r.Content) } else { "$($r.Content)" }
            $sha = Read-Sha256File $txt $sel.File
            if (-not $sha) { throw "$($sel.ShaUrl) holds no sha256 for $($sel.File)" }
            if ($sel.Digest -match '^sha256:([0-9a-f]{64})$' -and $Matches[1] -ne $sha) { throw "the .sha256 file and the release's own digest for $($sel.File) disagree" }
            [pscustomobject]@{ Url = $sel.Url; File = $sel.File; Sha = $sha; Kind = 'sfx'; Strip = '0'; Version = $sel.Date }
        }
        'pwsh' {
            $rel = Get-Release 'PowerShell/PowerShell' "v$PwshVersion"
            $file = "PowerShell-$PwshVersion-win-$arch.zip"
            $a = $rel.assets | Where-Object { $_.name -eq $file } | Select-Object -First 1
            if (-not $a) { throw "release v$PwshVersion has no $file" }
            $sha = if ($a.digest -match '^sha256:([0-9a-f]{64})$') { $Matches[1] }
            if (-not $sha) {
                $h = $rel.assets | Where-Object { $_.name -eq 'hashes.sha256' } | Select-Object -First 1
                if ($h) {
                    $t = (Invoke-WebRequest -Uri $h.browser_download_url -UseBasicParsing -Headers $ghHeaders).Content
                    if ("$t" -match "(?im)^\s*([0-9a-f]{64})\s+\*?$([regex]::Escape($file))\s*$") { $sha = $Matches[1].ToLower() }
                }
            }
            if (-not $sha) { throw "release v$PwshVersion publishes no sha256 for $file" }
            [pscustomobject]@{ Url = $a.browser_download_url; File = $file; Sha = $sha; Kind = 'archive'; Strip = '0'; Version = $PwshVersion }
        }
    }
}

# ── 3. is a good-enough one already there, the way the room sees it ─────────

# Test-Tool judges what the probe found: good ($true or $false), its version, and a reason when it is not.
function Test-Tool {
    param([string] $tool, [string] $path, [string] $raw)
    if (-not $path) { return [pscustomobject]@{ Good = $false; Ver = ''; Why = 'not found on the PATH the room sees' } }
    $need = switch ($tool) {
        'go' { $goMin }
        'node' { ($NodeVersion.TrimStart('v') -split '\.')[0] }
        'git' { '2.39' }
        'pwsh' { ($PwshVersion -split '\.')[0] }
    }
    $ver = switch ($tool) {
        'go' { if ($raw -match 'go(\d+\.\d+(\.\d+)?)') { $Matches[1] } }
        'node' { if ($raw -match 'v(\d+\.\d+\.\d+)') { $Matches[1] } }
        'git' { if ($raw -match 'git version (\d+\.\d+\.\d+)') { $Matches[1] } }
        'pwsh' { if ($raw -match 'PowerShell (\d+\.\d+\.\d+)') { $Matches[1] } }
    }
    if (-not $ver) { return [pscustomobject]@{ Good = $false; Ver = ''; Why = "$path does not say a version ($raw)" } }
    if ([version]($ver + $(if ($ver -notmatch '\.\d+\.\d+') { '.0' } else { '' })) -lt [version]($need + $(if ($need -notmatch '\.') { '.0' } else { '' }))) {
        return [pscustomobject]@{ Good = $false; Ver = $ver; Why = "$path is $ver and $need or newer is needed" }
    }
    if ($tool -eq 'git' -and $raw -notmatch '\.windows\.') {
        return [pscustomobject]@{ Good = $false; Ver = $ver; Why = "$path is not Git for Windows ($raw), so its git and bash are not the ones the tests need" }
    }
    [pscustomobject]@{ Good = $true; Ver = $ver; Why = '' }
}

function Read-Tool {
    param($kvs, [string] $tool, [string] $key = $tool)
    $v = "$($kvs[$key])" -split '\|', 2
    Test-Tool $tool $v[0] $v[1] | Add-Member -NotePropertyName Path -NotePropertyValue $v[0] -PassThru
}

$script:rc = 0
function Note-Fail { param([int] $code) if ($script:rc -eq 0) { $script:rc = $code } }

$found = @{}
foreach ($t in $Tools) { $found[$t] = Read-Tool $kv $t }
# A good copy in the prefix that the room's PATH does not reach yet is ADOPTED: its folder is recorded, nothing is installed.
$adopt = @{}
foreach ($t in $Tools | Where-Object { -not $found[$_].Good }) {
    if ($kv.ContainsKey("at.$t")) { $a = Read-Tool $kv $t "at.$t"; if ($a.Good) { $adopt[$t] = $a } }
}
$todo = @($Tools | Where-Object { -not $found[$_].Good -and -not $adopt.ContainsKey($_) })
foreach ($t in $Tools | Where-Object { $found[$_].Good }) {
    Step $t 'ok' "$($found[$t].Ver) at $($found[$t].Path)"
}

# ── the C profile ───────────────────────────────────────────────────────────

$script:cMsysDir = $null; $script:cHome = $null; $script:cUser = $null; $script:pathPre = ''

function Get-PathPre {
    $d = @($newDirs) + @("$($kv.rec)" -split ';')
    if ($found.ContainsKey('git') -and $found['git'].Good) { $d += (Split-Path -Parent $found['git'].Path) }
    ($d | Where-Object { $_ } | Select-Object -Unique) -join ';'
}
# One C act on the remote. rc is the act's own `rc=` line when it has one, because the exit code over ssh is not always the one
# the script exited with.
function CCall {
    param([string] $act, [hashtable] $vars = @{})
    $vars['PathPre'] = $script:pathPre
    $r = Invoke-Remote (Get-CPayload $act $vars)
    $k = ConvertFrom-KeyValue $r.Out
    $rc = if ($k.rc) { [int]$k.rc } elseif ($r.Code -ne 0) { $r.Code } else { 0 }
    [pscustomobject]@{ Kv = $k; Out = $r.Out; Rc = $rc; Err = "$($k.err)"; Tail = @(Get-Lines $r.Out 'tail') }
}
function Get-Lines { param($out, [string] $key) @($out | Where-Object { "$_" -like "$key=*" } | ForEach-Object { "$_".Substring($key.Length + 1) }) }
function Show-Tail { param($c) $c.Tail | Where-Object { "$_".Trim() } | ForEach-Object { Write-Host "    $_" } }
# A step that needs a person: the status is needs-human and the commands go in the summary at the end.
# $cmds is a list of commands, each one a list of words. Format-AdminCommand makes the line, so a command is quoted in one place.
function Need {
    param([string] $step, [string] $detail, [object[]] $cmds)
    Step $step 'needs-human' $detail
    $lines = @(foreach ($c in $cmds) { if ($c -is [string]) { throw "Need wants a command as a list of words, not '$c'" }; Format-AdminCommand ([string[]]@($c)) })
    foreach ($l in $lines) { Write-Host "    $l" }
    $script:needs += $lines
}
function Write-CTools {
    param($k, [string] $dir, [string[]] $gaps, [bool] $check)
    $would = if ($check) { "would run: pacman -S --needed --noconfirm $($script:CPackages -join ' ')" } else { 'it is still not there' }
    $one = {
        param([string] $step, $t, [string] $extra)
        if (-not $t.Path) { Step $step 'warn' "MISSING. $would"; return }
        Step $step 'ok' "$($t.Ver) at $($t.Path)$extra"
    }
    $gcc = Split-Tool $k 'bin.gcc'; $gxx = Split-Tool $k 'bin.g++'
    if ($gcc.Path -and $gxx.Path) { Step 'gcc' 'ok' "$($gcc.Ver) at $($gcc.Path), g++ $($gxx.Ver)" } else { Step 'gcc' 'warn' "MISSING$(if ($gcc.Path) { ' g++' }). $would" }
    $cm = Split-Tool $k 'bin.cmake'
    if ($cm.Path -and -not (Test-VersionAtLeast $cm.Ver $script:CmakeMin)) { Step 'cmake' 'warn' "$($cm.Ver) at $($cm.Path) is older than $($script:CmakeMin), which CMakeUserPresets.json version 4 needs. run pacman -Syu in MSYS2 to update it" }
    else { & $one 'cmake' $cm '' }
    & $one 'ninja' (Split-Tool $k 'bin.ninja') ''
    & $one 'pkgconf' (Split-Tool $k 'bin.pkg-config') ' (pkg-config.exe)'
    if ($k['ssl'] -eq 'True') { Step 'openssl' 'ok' "libssl.a and include/openssl/ssl.h under $dir\mingw64" } else { Step 'openssl' 'warn' "MISSING (libssl.a or include/openssl/ssl.h under $dir\mingw64). $would" }
}

# The MSYS2 probe: where it is and its tools (cmsys), then the accounts, the ACL, writability and the grants that were left
# behind (cacls), as one result.
function Get-CMsys {
    param([hashtable] $cv)
    $p = CCall 'cmsys' $cv
    if ($p.Rc -ne 0 -or -not $p.Kv.ContainsKey('msys2.dir')) { return $p }
    $a = CCall 'cacls' @{ Msys2Dir = $p.Kv['msys2.dir']; Accts = $cv.Accts; StateDir = "$StateDir" }
    foreach ($key in $a.Kv.Keys) { $p.Kv[$key] = $a.Kv[$key] }
    $p.Out = @($p.Out) + @($a.Out)
    $p
}

# Stage A: MSYS2, its packages, its ACL. Returns nothing, and adds mingw64\bin to the PATH record through $newDirs.
function Invoke-CMsys2 {
    $script:pathPre = Get-PathPre
    $cv = @{ Prefix = $prefixR; Rec = "$($kv.rec)"; Msys2Dir = $Msys2Dir; Accts = ($RunnerAccounts -join ',') }
    $p = Get-CMsys $cv
    if ($p.Rc -ne 0 -or -not $p.Kv.ContainsKey('msys2.dir')) {
        Step 'msys2' 'fail' "could not look for MSYS2 on $where"; $p.Out | ForEach-Object { Write-Host "    $_" }; Note-Fail 3; return
    }
    $script:cHome = $p.Kv.home; $script:cUser = $p.Kv.user
    $dir = $p.Kv['msys2.dir']; $script:cMsysDir = $dir
    $fresh = $false
    if ($p.Kv['msys2.found'] -eq 'True') {
        Step 'msys2' 'ok' "$dir (found, used as it is)"
    } else {
        try { $asset = Get-Asset 'msys2' }
        catch { Step 'msys2' 'fail' "MISSING at $dir. and the publisher's list could not be read: $($_.Exception.Message)"; Note-Fail 1; return }
        if ($Check) {
            Step 'msys2' 'warn' "MISSING (not at $dir). would install $($asset.Version) from $($asset.Url) (sha256 $($asset.Sha.Substring(0, 12))...) into $dir, run pacman -Syuu twice, then pacman -S --needed $($script:CPackages -join ' ')"
        } else {
            $sha = if ($TestBadHash) { ($(if ($asset.Sha[0] -eq '0') { '1' } else { '0' }) + $asset.Sha.Substring(1)) } else { $asset.Sha }
            $ir = CCall 'cinstall' @{ Msys2Dir = $dir; Prefix = $prefixR; Url = $asset.Url; File = $asset.File; Sha = $sha; Force = $(if ($Force) { '1' } else { '' }) }
            if ($ir.Rc -ne 0) {
                Step 'msys2' 'fail' $(if ($ir.Err) { $ir.Err } else { "the install on $where exited $($ir.Rc)" })
                if (-not $ir.Err) { $ir.Out | ForEach-Object { Write-Host "    $_" } }
                Note-Fail $(if ($ir.Rc -eq 4) { 4 } else { 3 }); return
            }
            Step 'msys2' 'done' "$($asset.Version) from $($asset.Url), sha256 $($ir.Kv.sha) matches, unpacked to $($ir.Kv.installed)"
            $fresh = $true
            $p = Get-CMsys $cv
        }
    }
    # A Modify grant an earlier run made and was cut off before it took back (ssh dropped, process killed) is still on the
    # directory, and the probe now says it is writable, so nothing would ever plan to take it back. The room remembers it.
    $rec = if ($p.Kv['msys2.found'] -eq 'True') { ConvertFrom-GrantRecord (Get-Lines $p.Out 'grant') $dir $p.Kv['user'] } else { [pscustomobject]@{ Grants = @(); Bad = @() } }
    $stale = @($rec.Grants)
    if ($rec.Bad.Count) { Step 'msys2-acl' 'warn' "acl-grants.txt has $($rec.Bad.Count) line(s) this script did not write and they were not acted on: $($rec.Bad -join ', '). look at the file in the toolchain folder next to the PATH record" }
    if ($stale.Count -and $Check) {
        Step 'msys2-acl' 'warn' "$(($stale | ForEach-Object { $_.Account }) -join ', ') still has the Modify grant on $dir that an earlier run was cut off before taking back (the room remembers it in acl-grants.txt next to its PATH record). a run takes it back"
    } elseif ($stale.Count) {
        foreach ($g in $stale) {
            $rops = @(New-RevertOps $g.Dir $g.Account $g.Before)
            $rv = CCall 'cacl' @{ Ops = (ConvertTo-AclOps $rops) }
            if ($rv.Rc -eq 0) { $null = CCall 'cstate' @{ StateDir = "$StateDir"; Mode = 'remove'; Key = "$($g.Dir)|$($g.Account)" }; Step 'msys2-acl' 'done' "took back the Modify grant for $($g.Account) on $($g.Dir) that an earlier run was cut off before taking back" }
            else { Need 'msys2-acl' "could not take back the Modify grant for $($g.Account) that an earlier run left on $($g.Dir) ($($rv.Err)). an admin runs this" @($rops | ForEach-Object { , (@('icacls') + $_.Args) }) }
        }
        $p = Get-CMsys $cv
    }
    $have = $p.Kv['msys2.found'] -eq 'True'
    $k = $p.Kv
    $gaps = if ($have) { @(Get-CGaps $k) } else { @('gcc', 'cmake', 'ninja', 'pkg-config', 'openssl') }
    # the other runner accounts that exist here, and the ACL
    $user = $p.Kv.user
    $others = @($RunnerAccounts | Where-Object { $k["acct.$_"] -eq 'True' -and -not (Test-SameAccount $_ $user) })
    $failedModify = $false; $plan = $null
    if ($have) {
        $aces = @(ConvertFrom-Icacls (Get-Lines $p.Out 'acl') $dir)
        $plan = New-AclPlan $dir $aces $user ($k.writable -eq 'True') $others ($gaps.Count -gt 0 -or $fresh)
        if ($Check) {
            if ($plan.Grant.Count) {
                Step 'msys2-acl' 'warn' ((@($plan.Grant | ForEach-Object { if ($_.Why -eq 'rx') { "would grant $($_.Account) read and execute" } else { "would grant $($_.Account) Modify while pacman runs and take it back after" } })) -join '. ')
            } else { Step 'msys2-acl' 'ok' "$(if ($others) { "$($others -join ', ') can read it" } else { 'no other runner account to grant' }), and $user can write it" }
        } else {
            foreach ($op in $plan.Grant) {
                # the room is told BEFORE the Modify grant, so a cut connection cannot leave one nobody knows about
                if ($op.Why -eq 'modify') { $null = CCall 'cstate' @{ StateDir = "$StateDir"; Mode = 'add'; Key = "$dir|$($op.Account)"; Before = $plan.Before } }
                $c = CCall 'cacl' @{ Ops = (ConvertTo-AclOps @($op)) }
                if ($c.Rc -ne 0 -and $op.Why -eq 'modify') { $null = CCall 'cstate' @{ StateDir = "$StateDir"; Mode = 'remove'; Key = "$dir|$($op.Account)" } }
                if ($c.Rc -eq 0) { Step 'msys2-acl' 'done' $(if ($op.Why -eq 'rx') { "granted $($op.Account) read and execute on $dir" } else { "granted $user Modify on $dir while pacman runs" }) }
                else {
                    Need 'msys2-acl' "icacls was refused for $($op.Account) on $dir ($($c.Err)). an admin runs this" @(, (@('icacls') + $op.Args))
                    if ($op.Why -eq 'modify') { $failedModify = $true }
                }
            }
            if (-not $plan.Grant.Count) { Step 'msys2-acl' 'ok' "$(if ($others) { "$($others -join ', ') can read it" } else { 'no other runner account to grant' }), and $user can write it" }
        }
    }
    if ($have -and $gaps.Count -and -not $Check) {
        if ($failedModify) {
            Need 'pacman' "$user cannot write $dir and the Modify grant was refused, so pacman was not run. an admin, or the owner of $dir, runs it" @(, @("$dir\usr\bin\bash.exe", '-lc', "pacman -S --needed --noconfirm $($script:CPackages -join ' ')"))
        } else {
            $pc = CCall 'cpacman' @{ Msys2Dir = $dir; Init = $(if ($fresh) { '1' } else { '' }); Pkgs = ($script:CPackages -join ' ') }
            if ($pc.Rc -ne 0) { Step 'pacman' 'fail' $(if ($pc.Err) { $pc.Err } else { "pacman on $where exited $($pc.Rc)" }); Show-Tail $pc; Note-Fail 3 }
            else { Step 'pacman' 'done' "$(if ($fresh) { 'first start and two core updates, then ' })pacman -S --needed $($script:CPackages -join ' ')" }
        }
        if ($plan.Modify -and -not $failedModify) {
            $rv = CCall 'cacl' @{ Ops = (ConvertTo-AclOps $plan.Revert) }
            if ($rv.Rc -eq 0) { $null = CCall 'cstate' @{ StateDir = "$StateDir"; Mode = 'remove'; Key = "$dir|$user" }; Step 'msys2-acl' 'done' "took $user's Modify on $dir back" }
            else { Need 'msys2-acl' "could not take $user's Modify back ($($rv.Err)). an admin runs this" @($plan.Revert | ForEach-Object { , (@('icacls') + $_.Args) }) }
        }
        $p = Get-CMsys $cv; $k = $p.Kv
        $gaps = @(Get-CGaps $k)
    }
    if ($have -or -not $Check) {
        Write-CTools $k $dir $gaps ([bool]$Check)
        if ($gaps.Count -and -not $Check -and -not $failedModify -and $script:rc -eq 0) { Note-Fail 5 }
    } else {
        Write-CTools @{} $dir $gaps $true
    }
    if ($others) {
        $cmds = @($others | ForEach-Object { , @('pwsh', '-File', 'scripts\room-toolchain.ps1', $(if ($Target -match '@') { $Target -replace '^[^@]*@', "$_@" } else { "$_@$Target" }), '-Profile', 'c', '-Msys2Dir', $dir) })
        Need 'runner-path' "the PATH record is per user and this run wrote it for $user only. run this as each other runner account too" $cmds
    }
    $bin = "$dir\mingw64\bin"
    if (@("$($kv.rec)" -split ';') -notcontains $bin) { $script:newDirs += $bin }
}

# Stage B: git, vcpkg, the checkout and the presets.
function Invoke-CRest {
    $script:pathPre = Get-PathPre
    if (-not $script:cHome) { Step 'vcpkg' 'warn' 'skipped: the MSYS2 probe did not answer, so the home directory is not known'; return }
    $home_ = $script:cHome
    $vdir = if ($VcpkgDir) { $VcpkgDir } else { "$home_\vcpkg" }
    $sdir = if ($SdkDir) { $SdkDir } else { "$home_\git\github\openziti\ziti-sdk-c" }
    $dry = if ($Check) { '1' } else { '0' }

    # git: identity and the credential helper
    $g = CCall 'cgit' @{}
    $gk = $g.Kv
    if (-not $gk['git.path']) {
        Step 'git-identity' 'warn' 'MISSING: git is not on the room yet, so no identity can be checked. the git step above installs it'
        Step 'git-credential' 'warn' 'MISSING: git is not on the room yet'
    } else {
        $hasName = [bool]$gk['git.name']; $hasEmail = [bool]$gk['git.email']
        $setName = if (-not $hasName -and $GitUserName) { $GitUserName } else { '' }
        $setEmail = if (-not $hasEmail -and $GitUserEmail) { $GitUserEmail } else { '' }
        $lackName = -not $hasName -and -not $GitUserName; $lackEmail = -not $hasEmail -and -not $GitUserEmail
        $helpers = @("$($gk['git.helper'])|$($gk['git.syshelper'])" -split '\|' | Where-Object { $_ })
        $hasHelper = [bool]($helpers | Where-Object { $_ -match '^(manager|manager-core)$' -or $_ -match 'git-credential-manager' })
        $setHelper = if ($hasHelper) { '' } else { 'manager' }
        if (($setName -or $setEmail -or $setHelper) -and -not $Check) {
            $sc = CCall 'cgitset' @{ Name = $setName; Email = $setEmail; Helper = $setHelper }
            if ($sc.Rc -ne 0) { Step 'git-identity' 'fail' "git config --global failed on $where"; $sc.Out | ForEach-Object { Write-Host "    $_" }; Note-Fail 3 }
        }
        # identity
        if ($hasName -and $hasEmail) { Step 'git-identity' 'ok' "$($gk['git.name']) <$($gk['git.email'])> (already in the global config, left alone)" }
        elseif ($lackName -or $lackEmail) {
            $cmds = @(); if ($lackName) { $cmds += , @('git', 'config', '--global', 'user.name', 'Your Name') }; if ($lackEmail) { $cmds += , @('git', 'config', '--global', 'user.email', 'you@example.com') }
            Need 'git-identity' "no $(if ($lackName) { 'user.name' })$(if ($lackName -and $lackEmail) { ' and ' })$(if ($lackEmail) { 'user.email' }) in the global config of $($script:cUser) and none was given (-GitUserName, -GitUserEmail). this script never invents one. as that user" $cmds
        }
        else { Step 'git-identity' $(if ($Check) { 'warn' } else { 'done' }) "$(if ($Check) { 'would set' } else { 'set' }) $(@(if ($setName) { "user.name '$setName'" }; if ($setEmail) { "user.email '$setEmail'" }) -join ' and ') in the global config" }
        # the helper and the access
        $gcmOk = [bool](Get-ToolVersion "$($gk.gcm)")
        $repos = @('https://github.com/openziti/ziti-sdk-c.git'); $checkUrl = $null
        if ($CheckRepo) { $checkUrl = ConvertTo-RepoUrl $CheckRepo; $repos += $checkUrl }
        $au = CCall 'cauth' @{ Repos = ($repos -join "`n") }
        $res = @($au.Out | Where-Object { "$_" -match '^auth\.\d+=' } | ForEach-Object { $_ -replace '^auth\.\d+=', '' } | ForEach-Object { $f = $_ -split '\|', 4; [pscustomobject]@{ Ok = ($f[0] -eq 'ok'); Url = $f[1]; Why = $f[3] } })
        $pub = $res | Select-Object -First 1
        $helperText = if ($hasHelper) { "credential.helper $(($helpers | Where-Object { $_ -match 'manager' } | Select-Object -First 1)) already set" } elseif ($Check) { 'would set credential.helper manager' } else { 'set credential.helper manager' }
        $gcmText = if ($gcmOk) { "git credential-manager $($gk.gcm) answers" } else { 'git credential-manager does not answer, so this git is not Git for Windows with its Credential Manager' }
        if (-not $pub -or -not $pub.Ok) {
            Step 'git-credential' 'fail' "$helperText, but git ls-remote $($repos[0]) failed without a prompt ($($pub.Why)). the room cannot reach github.com, or git is broken"; Note-Fail 3
        } elseif ($checkUrl -and $res.Count -gt 1 -and -not $res[1].Ok) {
            Need 'git-credential' "$helperText. public access works, but $checkUrl needs a login ($($res[1].Why)). a person runs this once in an interactive session AS $($script:cUser) on the room (a console, not ssh). it opens a browser or a device code prompt and asks for the GitHub account, and nothing is typed into this script" @(, @('git', 'credential-manager', 'github', 'login'))
        } elseif (-not $gcmOk) {
            Step 'git-credential' 'warn' "$helperText. public access works. $gcmText"
        } else {
            Step 'git-credential' $(if ($Check -and -not $hasHelper) { 'warn' } else { 'ok' }) "$helperText. public access works$(if ($checkUrl) { ", and so does $checkUrl" } else { ' (no -CheckRepo, so only the public repo was checked)' }). $gcmText"
        }
    }
    $gitUsable = [bool]$gk['git.path']

    # vcpkg
    $vc = CCall 'cvcpkg' @{ VcpkgDir = $vdir; Url = 'https://github.com/microsoft/vcpkg'; Dry = $dry }
    $vk = $vc.Kv
    if ($vk['vcpkg.inway'] -eq 'True') { Step 'vcpkg' 'fail' "$vdir is there and is not a git checkout of vcpkg. nothing was replaced. look at it"; Note-Fail 3 }
    elseif ($vc.Rc -ne 0) { Step 'vcpkg' 'fail' $(if ($vc.Err) { $vc.Err } else { "vcpkg on $where exited $($vc.Rc)" }); Show-Tail $vc; Note-Fail 3 }
    else {
        $vv = "$($vk['vcpkg.ver'])" -split '\|', 2
        # vcpkg says `vcpkg package management program version 2026-08-27-<hash>`, a date and not a dotted number
        $vver = if ("$($vv[1])" -match 'version (\S+)') { $Matches[1] } else { "$($vv[1])" }
        if ($vv[0]) { Step 'vcpkg' $(if ($vk.cloned -or $vk.bootstrapped) { 'done' } else { 'ok' }) "$vver at $($vv[0])$(if ($vk.cloned) { ', cloned' })$(if ($vk.bootstrapped) { ', bootstrapped' })" }
        elseif ($Check) { Step 'vcpkg' 'warn' "MISSING at $vdir. would $(if ($vk['vcpkg.git'] -ne 'True') { 'git clone https://github.com/microsoft/vcpkg there, then ' })run bootstrap-vcpkg.bat -disableMetrics$(if (-not $gitUsable) { ' (git is not on the room yet)' })" }
        else { Step 'vcpkg' 'fail' "vcpkg.exe is not at $vdir after the bootstrap"; Note-Fail 5 }
        if ($vk['vcpkg.triplet'] -eq 'True') { Step 'triplet' 'ok' "x64-mingw-static is in $vdir\triplets\community" }
        else { Step 'triplet' $(if ($Check) { 'warn' } else { 'fail' }) "MISSING: $vdir\triplets\community\x64-mingw-static.cmake$(if ($Check) { '. it comes with the vcpkg clone' })"; if (-not $Check) { Note-Fail 5 } }
    }

    # the checkout
    $sc2 = CCall 'csdk' @{ SdkDir = $sdir; Url = 'https://github.com/openziti/ziti-sdk-c'; Dry = $dry }
    $sk = $sc2.Kv
    if ($sk['sdk.inway'] -eq 'True') { Step 'sdk-checkout' 'fail' "$sdir is there and is not a git checkout. nothing was replaced. look at it"; Note-Fail 3 }
    elseif ($sc2.Rc -ne 0) { Step 'sdk-checkout' 'fail' $(if ($sc2.Err) { $sc2.Err } else { "the checkout on $where exited $($sc2.Rc)" }); Show-Tail $sc2; Note-Fail 3 }
    elseif ($sk['sdk.git'] -eq 'True' -or $sk.cloned) {
        $ign = if ($sk['sdk.ignored'] -eq 'True') { '' } else { ". NOTE its .gitignore does not list CMakeUserPresets.json, so the generated file would show as untracked" }
        Step 'sdk-checkout' $(if ($sk.cloned) { 'done' } else { 'ok' }) "$sdir, branch $($sk['sdk.branch']), $(if ($sk['sdk.dirty'] -eq 'True') { 'dirty' } else { 'clean' })$(if ($sk.cloned) { ', cloned now' } else { ', left alone' })$(if ($sk.submodules) { ', submodules initialised' })$ign"
    } else {
        Step 'sdk-checkout' $(if ($Check) { 'warn' } else { 'fail' }) "MISSING at $sdir.$(if ($Check) { " would git clone https://github.com/openziti/ziti-sdk-c there$(if (-not $gitUsable) { ' (git is not on the room yet)' })" })"
        if (-not $Check) { Note-Fail 5 }
    }

    # the preset
    $mdir = $script:cMsysDir
    $json = ConvertTo-PresetsJson (Get-CwdmingPresets $vdir $mdir $VcpkgBinaryCache)
    if ($Check) {
        $rd = CCall 'cpread' @{ SdkDir = $sdir }
        $cur = if ($rd.Kv.b64) { [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($rd.Kv.b64)) } else { '' }
        Invoke-Expression $script:CPresetMerge
        $m = if ($rd.Kv.dir -ne 'True') { $null } else { Merge-Presets $cur $json }
        $pk = if ($m) { @{ preset = $m.Action; added = ($m.Added -join ','); kept = ($m.Kept -join ','); why = $m.Why } } else { @{ preset = 'nodir' } }
    } else {
        $sj = CCall 'cpjson' @{ Json = $json }
        if ($sj.Rc -ne 0) { Step 'cmake-preset' 'fail' "could not stage the preset text on $where"; $sj.Out | ForEach-Object { Write-Host "    $_" }; Note-Fail 3; return }
        $pc = CCall 'cpresets' @{ SdkDir = $sdir }
        $pk = $pc.Kv
    }
    $file = "$sdir\CMakeUserPresets.json"
    switch ($pk.preset) {
        'nodir' { Step 'cmake-preset' 'warn' "would write $file with $(@((Get-CwdmingPresets $vdir $mdir $VcpkgBinaryCache) | ForEach-Object { $_.name }) -join ', ') once $sdir is cloned" }
        'created' { Step 'cmake-preset' $(if ($Check) { 'warn' } else { 'done' }) "$(if ($Check) { 'would create' } else { 'created' }) $file with $($pk.added). cmake --preset cwdming" }
        'merged' { Step 'cmake-preset' $(if ($Check) { 'warn' } else { 'done' }) "$(if ($Check) { 'would add' } else { 'added' }) $($pk.added) to the existing $file$(if ($pk.kept) { ", kept $($pk.kept) as they are" }). the rest of the file is kept$(if (-not $Check) { ', the old one is next to it as CMakeUserPresets.json.atrium-bak' })" }
        'ok' { Step 'cmake-preset' 'ok' "$file already has $($pk.kept), left alone" }
        'invalid' { Need 'cmake-preset' "$file was left alone because $($pk.why). move it aside or fix it, then run this again" @(, @('move', $file, "$file.old")) }
        default { Step 'cmake-preset' 'fail' "the preset step on $where answered '$($pk.preset)'"; Note-Fail 3 }
    }
}

# macOS and Linux: report only.
function Invoke-CUnix {
    $r = Invoke-Remote (Get-Payload 'cprobe' @{ VcpkgDir = $VcpkgDir; SdkDir = $SdkDir })
    $k = ConvertFrom-KeyValue $r.Out
    if ($r.Code -ne 0) { Step 'c-profile' 'fail' "the probe on $where exited $($r.Code)"; $r.Out | ForEach-Object { Write-Host "    $_" }; Note-Fail 3; return }
    foreach ($t in 'cc', 'gcc', 'cmake', 'ninja', 'git') {
        $v = "$($k[$t])" -split '\|', 2
        if ($v[0]) { Step $t 'ok' "$(Get-ToolVersion $v[1]) at $($v[0])" } else { Step $t 'warn' 'MISSING. install it with the system package manager' }
    }
    Step 'vcpkg' $(if ($k['vcpkg.here'] -eq 'True') { 'ok' } else { 'warn' }) "$($k['vcpkg.dir'])$(if ($k['vcpkg.here'] -ne 'True') { ' MISSING' })"
    Step 'sdk-checkout' $(if ($k['sdk.here'] -eq 'True') { 'ok' } else { 'warn' }) "$($k['sdk.dir'])$(if ($k['sdk.here'] -ne 'True') { ' MISSING' })"
    if (-not $Check) { Step 'c-profile' 'skip' 'install cmake, ninja and gcc with the system package manager. this script installs nothing for -Profile c here' }
}

# ── 4. install what is not ──────────────────────────────────────────────────

$newDirs = @()
$changed = $false
# The directory each tool puts on PATH, under its own folder in the prefix.
$binDir = @{ go = 'go\bin'; node = 'node'; git = 'git\cmd'; pwsh = 'pwsh' }
function Get-BinDir { param([string] $t) if ($script:remoteOS -eq 'windows') { "$prefixR\$($binDir[$t])" } else { "$prefixR/" + $(if ($t -eq 'go') { 'go/bin' } else { 'node/bin' }) } }

foreach ($t in $Tools | Where-Object { $adopt.ContainsKey($_) }) {
    $a = $adopt[$t]
    Step $t $(if ($Check) { 'warn' } else { 'ok' }) "$($a.Ver) is already at $($a.Path), but the room does not see it ($($found[$t].Why)). $(if ($Check) { 'would record' } else { 'recording' }) its folder, nothing is installed"
    $newDirs += Get-BinDir $t
}
foreach ($t in $todo) {
    $why = $found[$t].Why
    try { $asset = Get-Asset $t }
    catch { Step $t 'fail' "$why. and the publisher's list could not be read: $($_.Exception.Message)"; Note-Fail 1; continue }
    $sha = if ($TestBadHash) { ($(if ($asset.Sha[0] -eq '0') { '1' } else { '0' }) + $asset.Sha.Substring(1)) } else { $asset.Sha }
    $destDir = "$prefixR$sep$t"
    if ($Check) {
        $note = if ($kv["dest.$t"] -eq 'True') { ". $destDir is already there, so an install needs -Force" } else { '' }
        Step $t 'warn' "$why. would install $($asset.Version) from $($asset.Url) (sha256 $($asset.Sha.Substring(0, 12))...) into $destDir$note"
        $newDirs += Get-BinDir $t
        continue
    }
    $vars = @{ Tool = $t; Url = $asset.Url; File = $asset.File; Sha = $sha; Kind = $asset.Kind; Strip = $asset.Strip }
    $ir = Invoke-Remote (Get-Payload 'install' $vars)
    $ik = ConvertFrom-KeyValue $ir.Out
    # THE payload's own `rc=` LINE, because over ssh the exit code that arrives is not always the one the script exited with
    # (measured on claudevm: a 4 arrived as something else).
    $irc = if ($ik.rc) { [int]$ik.rc } else { $ir.Code }
    if ($ir.Code -ne 0 -or $irc -ne 0) {
        $code = if ($irc -eq 4) { 4 } else { 3 }
        Step $t 'fail' $(if ($ik.err) { $ik.err } else { "the install on $where exited $($ir.Code)" })
        if (-not $ik.err) { $ir.Out | ForEach-Object { Write-Host "    $_" } }
        Note-Fail $code
        continue
    }
    Step "$t" 'done' "$($asset.Version) from $($asset.Url), sha256 $($ik.sha) matches, unpacked to $($ik.installed)"
    $newDirs += Get-BinDir $t
    $changed = $true
}
if ($script:rc -ne 0 -and -not $Check) {
    # What did install is still recorded below, so a rerun only has the rest to do.
}
if ($cProfile -and $script:remoteOS -eq 'windows') { Invoke-CMsys2 }

# ── 5. the PATH record, and where the room's start reads it ─────────────────

$installedDirs = @($newDirs | Where-Object { $_ })
$hookNow = $kv.hook -eq 'True'
$recNow = @("$($kv.rec)" -split ';' | Where-Object { $_ })
# Windows only: the login bash of a session needs the record too, or Cygwin's git comes first (see the record payload).
$bashMissing = $script:remoteOS -eq 'windows' -and $kv.bashhook -ne 'True'
if ($Check) {
    if ($recNow -and $bashMissing -and $hookNow -and -not $installedDirs) {
        Step 'path' 'warn' "the record lists $($recNow -join ', '), but ~/.bash_profile does not put it first, so a session's Bash tool gets Cygwin's git. a run without -Check writes it"
    } elseif ($installedDirs) {
        Step 'path' 'warn' "would record $($installedDirs -join ', ') in ~/.atrium/toolchain/path.txt$(if ($script:remoteOS -eq 'windows') { ' and write room-env.ps1, which the room start dot-sources. the user and machine Path are not touched' } else { " and add one line to $($kv.profile)" })"
    } elseif ($recNow -and -not $hookNow) {
        Step 'path' 'warn' "the record lists $($recNow -join ', '), but $(if ($script:remoteOS -eq 'windows') { 'room-env.ps1 is missing' } else { "$($kv.profile) has no line reading it" }). a run without -Check writes it"
    } elseif ($recNow) {
        Step 'path' 'ok' "the record lists $($recNow -join ', ')$(if ($script:remoteOS -eq 'windows') { '. the room start must dot-source ~\.atrium\toolchain\room-env.ps1' } else { ", and $($kv.profile) reads it" })"
    } else {
        Step 'path' 'skip' 'this script installed nothing here, so there is nothing to record'
    }
} elseif ($installedDirs -or ($recNow -and (-not $hookNow -or $bashMissing))) {
    $rr = Invoke-Remote (Get-Payload 'record' @{ NewDirs = ($installedDirs -join ';') })
    $rk = ConvertFrom-KeyValue $rr.Out
    if ($rr.Code -ne 0) {
        Step 'path' 'fail' 'could not write the PATH record on the remote'
        $rr.Out | ForEach-Object { Write-Host "    $_" }
        Note-Fail 5
    } else {
        $did = ($rk.pathfile -eq 'changed' -or $rk.hook -eq 'changed')
        $changed = $changed -or $did
        # ~/.bash_profile is read by the next session, not the room, so it does not make the room's PATH stale.
        $didBash = $rk.bashprofile -eq 'changed'
        $via = if ($script:remoteOS -eq 'windows') { "written for the room start: dot-source $($rk.envfile). the user and machine Path are not touched$(if ($didBash) { '. ~/.bash_profile now puts it first for a session''s login bash' })" } else { "$($rk.profile) reads ~/.atrium/toolchain/path.sh" }
        Step 'path' $(if ($did -or $didBash) { 'done' } else { 'ok' }) "$(if ($installedDirs) { $installedDirs -join ', ' } else { $recNow -join ', ' }) in ~/.atrium/toolchain/path.txt, $via"
    }
} elseif ($recNow) {
    Step 'path' 'ok' "the record lists $($recNow -join ', ')"
} else {
    Step 'path' 'skip' 'this script installed nothing here, so there is nothing to record'
}

# ── 6. look again, the way the room will see it ─────────────────────────────

if (-not $Check -and $todo -and $script:rc -ne 3 -and $script:rc -ne 4 -and $script:rc -ne 1) {
    $vr = Invoke-Remote (Get-Payload 'verify')
    $vk = ConvertFrom-KeyValue $vr.Out
    foreach ($t in ($todo | Where-Object { $_ -in ($Tools) })) {
        $n = Read-Tool $vk $t
        if ($n.Good) { Step "$t.verify" 'ok' "$($n.Ver) at $($n.Path) is what the room sees now" }
        else { Step "$t.verify" 'fail' "still not good after the install: $($n.Why)"; Note-Fail 5 }
    }
}

# ── 6b. the rest of the C profile: git, vcpkg, the checkout, the presets ────

if ($cProfile) { if ($script:remoteOS -eq 'windows') { Invoke-CRest } else { Invoke-CUnix } }

# ── 7. a room that is running has the old PATH ──────────────────────────────

if ($changed -and $kv.room -eq 'up') {
    $how = if ($script:remoteOS -eq 'windows') { "start it with room-env.ps1 dot-sourced first (docs/changes/fabric-1-toolchain.md)" } else { 'stop it and start it again through a login shell, which is how provision-room.ps1 starts it' }
    Step 'restart' 'warn' "the room on $where answers on 7781 and read its PATH when it started, so it does not see this yet. this script does not restart it. $how"
}

# 6 is a finished run that needs a person. -Check never ends with it: nothing ran.
Finish (Get-CExitCode $script:rc $(if ($Check) { 0 } else { $script:needs.Count }))
