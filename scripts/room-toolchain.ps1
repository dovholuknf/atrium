# The toolchain for a room, run from THIS side over ssh: go, node, and on Windows a real git and pwsh 7.
#
#   pwsh -File scripts\room-toolchain.ps1 m1mini -Check
#   pwsh -File scripts\room-toolchain.ps1 user@host
#   pwsh -File scripts\room-toolchain.ps1 user@host -Tools go,node -Prefix ~/tools
#   pwsh -File scripts\room-toolchain.ps1 local -Check          # this machine, no ssh
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
    [string] $StateDir
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
# `pwsh -File` hands `-Tools go,node` and `-SshOption -o,Port=2222` over as one string, so commas split.
function Split-List { param($v) @($v | ForEach-Object { "$_" -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ }) }
$Tools = Split-List $Tools
$SshOption = Split-List $SshOption
$NodeVersion = if ($NodeVersion.StartsWith('v')) { $NodeVersion } else { "v$NodeVersion" }

function Step {
    param([string] $step, [string] $status, [string] $detail = '')
    $line = "room-toolchain $step $status"
    if ($detail) { $line += " $detail" }
    Write-Host $line
}
function Finish {
    param([int] $code)
    if ($code -eq 0) { Step 'done' 'ok' } else { Step 'done' 'fail' "$code" }
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

$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $SshOption
function Quote-Ps { param([string] $s) "'" + ($s -replace "'", "''") + "'" }
function Quote-Sh { param([string] $s) "'" + ($s -replace "'", "'\''") + "'" }

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

function Compress-Text {
    param([string] $s)
    $ms = [IO.MemoryStream]::new()
    $gz = [IO.Compression.GZipStream]::new($ms, [IO.Compression.CompressionMode]::Compress)
    $b = [Text.Encoding]::UTF8.GetBytes($s)
    $gz.Write($b, 0, $b.Length); $gz.Close()
    [Convert]::ToBase64String($ms.ToArray())
}

# Invoke-Remote runs one script on the remote, or here for `local`. Windows gets -EncodedCommand, whose payload is the
# script gzipped, so cmd.exe's command line limit is not the script's limit. Unix gets the script on stdin to `sh -s`,
# so nothing in it meets the login shell's quoting.
function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $boot = "`$ErrorActionPreference='Continue';`$ProgressPreference='SilentlyContinue';" +
            "iex (([IO.StreamReader]::new([IO.Compression.GZipStream]::new([IO.MemoryStream]::new(" +
            "[Convert]::FromBase64String('$(Compress-Text $script)')),[IO.Compression.CompressionMode]::Decompress))).ReadToEnd())"
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($boot))
        if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
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
    Tell ($Names -split ',')
    try { $null = Invoke-WebRequest 'http://127.0.0.1:7781/v1/health' -UseBasicParsing -TimeoutSec 3; 'room=up' } catch { 'room=down' }
#@install
    $Dest = Join-Path $Prefix $Tool
    if (Test-Path -LiteralPath $Dest) {
        if (-not $Force) { "err=$Dest is already there and is not a working $Tool. look at it, or rerun with -Force to replace it"; exit 3 }
    }
    $dl = Join-Path $Prefix '.downloads'; New-Item -ItemType Directory -Force -Path $dl | Out-Null
    $f = Join-Path $dl $File
    try { Invoke-WebRequest -Uri $Url -OutFile $f -UseBasicParsing } catch { "err=download of $Url failed: $($_.Exception.Message)"; exit 3 }
    $got = (Get-FileHash -LiteralPath $f -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $Sha) { Remove-Item -LiteralPath $f -Force; "err=sha256 of $File is $got, the publisher says $Sha. nothing was unpacked"; exit 4 }
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
    } catch { "err=could not unpack $File`: $($_.Exception.Message)"; exit 3 }
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
    if [ "$FORCE" = 1 ]; then :; else echo "err=$D is already there and is not a working $TOOL. look at it, or rerun with -Force to replace it"; exit 3; fi
  fi
  mkdir -p "$PREFIX/.downloads" || exit 3
  F="$PREFIX/.downloads/$FILE"
  if command -v curl >/dev/null 2>&1; then curl -fsSL --connect-timeout 20 --max-time 600 -o "$F" "$URL" || { echo "err=download of $URL failed"; rm -f "$F"; exit 3; }
  elif command -v wget >/dev/null 2>&1; then wget -q -O "$F" "$URL" || { echo "err=download of $URL failed"; rm -f "$F"; exit 3; }
  else echo "err=neither curl nor wget is here to download with"; exit 3; fi
  got=$(sha "$F")
  if [ "$got" != "$SHA" ]; then rm -f "$F"; echo "err=sha256 of $FILE is $got, the publisher says $SHA. nothing was unpacked"; exit 4; fi
  echo "sha=$got"
  rm -rf "$D"
  tmp="$D.tmp"; rm -rf "$tmp"; mkdir -p "$tmp" || exit 3
  if tar -xzf "$F" -C "$tmp"; then
    set -- "$tmp"/*
    if [ $# -eq 1 ] && [ -d "$1" ] && mv "$1" "$D"; then :; else echo "err=$FILE did not unpack to one folder"; rm -rf "$tmp" "$F"; exit 3; fi
  else echo "err=could not unpack $FILE"; rm -rf "$tmp" "$F"; exit 3; fi
  rm -rf "$tmp" "$F"
  echo "installed=$D";;
record)
  mkdir -p "$T" || exit 5
  { printf '%s\n' "$NEWDIRS" | tr ';' '\n'; cat "$PF" 2>/dev/null; } | awk 'NF && !seen[$0]++' > "$PF.tmp" || exit 5
  if cmp -s "$PF.tmp" "$PF" 2>/dev/null; then rm -f "$PF.tmp"; echo pathfile=same; else mv "$PF.tmp" "$PF"; echo pathfile=changed; fi
  { echo '# written by room-toolchain.ps1: what it installed, in front of PATH. Read by the login profile.'
    printf 'export PATH="%s$PATH"\n' "$(awk '{printf "%s:", $0}' "$PF")"; } > "$PS.tmp"
  if cmp -s "$PS.tmp" "$PS" 2>/dev/null; then rm -f "$PS.tmp"; else mv "$PS.tmp" "$PS"; fi
  f=$(profile)
  if grep -qs 'atrium/toolchain/path.sh' "$f"; then echo hook=same; else
    case "$T" in "$HOME"/*) tl="\$HOME${T#"$HOME"}";; *) tl="$T";; esac
    printf '\n%s\n' "[ -r \"$tl/path.sh\" ] && . \"$tl/path.sh\" # added by atrium room-toolchain" >> "$f" || exit 5
    echo hook=changed; fi
  echo "profile=$f";;
verify) tell "$NAMES";;
esac
'@

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

# The first probe: for Windows it also says the arch.
$Tools = @($Tools | Where-Object { $script:remoteOS -eq 'windows' -or $_ -in 'go', 'node' })
if ($script:remoteOS -eq 'unix' -and -not $Tools) { Step 'tools' 'skip' 'git and pwsh are installed on Windows only, and nothing else was asked for'; Finish 0 }
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
    if ($ir.Code -ne 0) {
        $code = if ($ir.Code -eq 4) { 4 } else { 3 }
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

# ── 5. the PATH record, and where the room's start reads it ─────────────────

$installedDirs = @($newDirs | Where-Object { $_ })
$hookNow = $kv.hook -eq 'True'
$recNow = @("$($kv.rec)" -split ';' | Where-Object { $_ })
if ($Check) {
    if ($installedDirs) {
        Step 'path' 'warn' "would record $($installedDirs -join ', ') in ~/.atrium/toolchain/path.txt$(if ($script:remoteOS -eq 'windows') { ' and write room-env.ps1, which the room start dot-sources. the user and machine Path are not touched' } else { " and add one line to $($kv.profile)" })"
    } elseif ($recNow -and -not $hookNow) {
        Step 'path' 'warn' "the record lists $($recNow -join ', '), but $(if ($script:remoteOS -eq 'windows') { 'room-env.ps1 is missing' } else { "$($kv.profile) has no line reading it" }). a run without -Check writes it"
    } elseif ($recNow) {
        Step 'path' 'ok' "the record lists $($recNow -join ', ')$(if ($script:remoteOS -eq 'windows') { '. the room start must dot-source ~\.atrium\toolchain\room-env.ps1' } else { ", and $($kv.profile) reads it" })"
    } else {
        Step 'path' 'skip' 'this script installed nothing here, so there is nothing to record'
    }
} elseif ($installedDirs -or ($recNow -and -not $hookNow)) {
    $rr = Invoke-Remote (Get-Payload 'record' @{ NewDirs = ($installedDirs -join ';') })
    $rk = ConvertFrom-KeyValue $rr.Out
    if ($rr.Code -ne 0) {
        Step 'path' 'fail' 'could not write the PATH record on the remote'
        $rr.Out | ForEach-Object { Write-Host "    $_" }
        Note-Fail 5
    } else {
        $did = ($rk.pathfile -eq 'changed' -or $rk.hook -eq 'changed')
        $changed = $changed -or $did
        $via = if ($script:remoteOS -eq 'windows') { "written for the room start: dot-source $($rk.envfile). the user and machine Path are not touched" } else { "$($rk.profile) reads ~/.atrium/toolchain/path.sh" }
        Step 'path' $(if ($did) { 'done' } else { 'ok' }) "$($installedDirs -join ', ') in ~/.atrium/toolchain/path.txt, $via"
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

# ── 7. a room that is running has the old PATH ──────────────────────────────

if ($changed -and $kv.room -eq 'up') {
    $how = if ($script:remoteOS -eq 'windows') { "start it with room-env.ps1 dot-sourced first (docs/changes/fabric-1-toolchain.md)" } else { 'stop it and start it again through a login shell, which is how provision-room.ps1 starts it' }
    Step 'restart' 'warn' "the room on $where answers on 7781 and read its PATH when it started, so it does not see this yet. this script does not restart it. $how"
}

Finish $script:rc
