# The work-root and agent-pack helpers that provision-room.ps1 and room-check.ps1 share. DOT-SOURCED, runs nothing by itself:
#
#   . (Join-Path $PSScriptRoot 'room-work.ps1')
#
# THE WORK ROOT (backlog f-room-work-drive). Everything an agent writes goes under one folder on the work drive, such as
# V:\localai on sg3, and nothing of it on the system drive:
#
#   <work>/git                 git.scm_root and git_root: the clones, and with them the <clone>-worktrees folders
#   <work>/reviews             the reviews_root setting
#   <work>/handoff             the context_handoff_dir setting
#   <work>/cache/npm           npm's cache      (~/.npmrc, `cache=`)
#   <work>/cache/go-mod        GOMODCACHE       (`go env -w`, the file under the user config dir)
#   <work>/cache/go-build      GOCACHE          (the same)
#   <work>/cache/pip           pip's cache      (pip.ini or pip.conf, `[global] cache-dir`)
#   <work>/cache/cargo         CARGO_HOME       (a user environment variable, a profile line on Unix)
#
# The caches are set the way each tool persists them for the user the room runs as, by writing that file, so it needs the tool
# NOT to be installed yet (room-toolchain.ps1 comes after provisioning on a bare machine). When the tool IS there it is asked
# for the value afterwards, and a different answer (GOENV, an npm project file) is a warn. Never a symlink or a junction.
#
# EVERY PARENT OF THE WORK ROOT MUST BE EXAMINABLE by the room's account, or Claude Code raises its own prompt, "could not be
# determined", which neither atrium rules nor auto mode can answer. Get-WorkParents lists them, the probe reads each one's
# attributes as the account, and a parent it cannot read is named with the line an administrator runs, always
#
#   icacls V:\ /grant 'SG3\localai:(RA,REA)'          the attributes, not inherited: no listing, no creating
#   icacls V:\localai /grant 'SG3\localai:(OI)(CI)F'  the agent folder only
#
# and never anything that lets the account list or create at a drive root.
#
# THE AGENT PACK (backlog f-room-agent-pack). The operator's Claude agents and skills, claude/agents/*.md and
# claude/skills/<name>/ of the dotfiles repo, installed as real files into the room account's ~/.claude, with the commit they
# came from recorded in ~/.claude/atrium-agent-pack.json. The source is the hub's own git mirror of dotfiles, never the
# checkout on the machine that runs the script and never a push to the room.
#
# Everything here is text in and text out, so scripts/test-room-work.ps1 runs it without ssh. The code that touches a remote
# is built as a script and run through the caller's own Invoke-Remote, passed in as a script block.

. (Join-Path $PSScriptRoot 'room-folders.ps1')

# ── the layout ──────────────────────────────────────────────────────────────

# Format-WorkPath is a path as the settings spell it: forward slashes, no trailing slash.
function Format-WorkPath { param([string] $p) ($p -replace '\\', '/').TrimEnd('/') }

# Test-WorkRootArg is why -WorkRoot cannot be used, or $null. Absolute only (the remote's working directory is whatever ssh
# gave it), no drive root (that is the thing the account is never given), no UNC.
function Test-WorkRootArg {
    param([string] $dir)
    if (-not $dir) { return 'is empty' }
    if ($dir -match '[;\r\n\0]') { return 'holds a semicolon or a newline' }
    if ($dir -match '^(\\\\|//)') { return 'is a network path, which this does not set up' }
    if ($dir -notmatch '^(/|[A-Za-z]:[\\/])') { return 'is not an absolute path (start it with / or a drive letter)' }
    if ($dir -match '^(/+|[A-Za-z]:[\\/]*)$') { return 'is a filesystem root, and an agent must never be able to create a folder there. name a folder on the drive' }
    if ($dir -match '(^|[\\/])\.\.([\\/]|$)') { return 'holds .., so the folder it means is not the one it says' }
    $null
}

# Test-WorkRootOs is why a -WorkRoot that Test-WorkRootArg let through cannot be used on a room of this OS, or $null. The
# argument checks run before the OS is known, so a drive path on a Unix room (which would be made under $HOME) and a Unix path
# on a Windows room (which has no drive) are caught here, once the room says what it is.
function Test-WorkRootOs {
    param([string] $os, [string] $dir)
    $isDrive = $dir -match '^[A-Za-z]:[\\/]'
    if ($os -eq 'windows' -and -not $isDrive) { return 'is a Unix path and the room is a Windows machine. name a folder on a drive, like V:\localai' }
    if ($os -ne 'windows' -and $isDrive) { return "is a drive path and the room is a $os machine. name an absolute folder, like /srv/localai" }
    $null
}

# Test-AgentPackArg is why -AgentPackRepo or -AgentPackBranch cannot be used, or $null. Both end up as git arguments.
function Test-AgentPackArg {
    param([string] $repo, [string] $branch)
    if ($repo -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { return "-AgentPackRepo '$repo' is not owner/name" }
    if ($branch -notmatch '^[A-Za-z0-9_][A-Za-z0-9_./-]*$' -or $branch -match '\.\.') { return "-AgentPackBranch '$branch' is not a branch name (it may not start with - or hold ..)" }
    $null
}

# Get-WorkLayout is every folder the work root holds, as the settings spell them.
function Get-WorkLayout {
    param([string] $root)
    $r = Format-WorkPath $root
    [pscustomobject]@{
        Root = $r; Git = "$r/git"; Reviews = "$r/reviews"; Handoff = "$r/handoff"
        Npm = "$r/cache/npm"; GoMod = "$r/cache/go-mod"; GoBuild = "$r/cache/go-build"; Pip = "$r/cache/pip"; Cargo = "$r/cache/cargo"
    }
}

# Get-WorkDirs is the folders to make, in the order to make them.
function Get-WorkDirs {
    param($layout)
    @($layout.Root, $layout.Git, $layout.Reviews, $layout.Handoff, "$($layout.Root)/cache", $layout.Npm, $layout.GoMod, $layout.GoBuild, $layout.Pip, $layout.Cargo)
}

# Get-WorkParents is every folder ABOVE the work root, drive root first, in the form the OS spells it: V:\ and V:\a on Windows,
# / and /srv on Unix. The root itself is not one of them: it is the account's own.
function Get-WorkParents {
    param([string] $root, [string] $os)
    $p = Format-WorkPath $root
    $out = @()
    if ($os -eq 'windows') {
        if ($p -notmatch '^([A-Za-z]:)(/.*)?$') { return @() }
        $acc = $Matches[1]; $out += "$acc\"
        $segs = @(("$($Matches[2])" -split '/') | Where-Object { $_ })
        for ($i = 0; $i -lt $segs.Count - 1; $i++) { $acc += '\' + $segs[$i]; $out += $acc }
    } else {
        $out += '/'
        $segs = @(($p -split '/') | Where-Object { $_ })
        $acc = ''
        for ($i = 0; $i -lt $segs.Count - 1; $i++) { $acc += '/' + $segs[$i]; $out += $acc }
    }
    $out
}

# ── what an administrator is told to run ────────────────────────────────────

function Format-WorkWord { param([string] $s) if ($s -cmatch '^[A-Za-z0-9_.:\\/-]+$') { $s } else { "'" + ($s -replace "'", "''") + "'" } }
function Format-WorkWordSh { param([string] $s) if ($s -cmatch '^[A-Za-z0-9_.:/=+-]+$') { $s } else { ConvertTo-ShLiteral $s } }

# Get-WorkAdminLines is the commands for an administrator, each one line, for the parents the account cannot examine and a work
# root it cannot make or write. Windows is PowerShell, Unix is sh. The grant on a parent is the attributes alone and is NOT
# inherited, so the account can neither list the drive nor create at its root. The work root gets full control, inherited.
#
# A parent that is not there ($missingParents, a subset of $badParents) is MADE first, top down, and only then are the grants
# given, so the lines work as printed. On Unix a parent is opened to the account alone, by an ACL entry (`setfacl -m
# u:<acct>:x`, `chmod +a` on a Mac), never `chmod o+x`, which would open it to every user. The root of the filesystem is never
# touched: there is no line for `/`.
function Get-WorkAdminLines {
    param([string] $os, [string] $account, [string] $root, [string[]] $badParents = @(), [bool] $rootMissing = $false, [bool] $rootNotWritable = $false, [string[]] $missingParents = @())
    $lines = @()
    if ($os -eq 'windows') {
        $wr = (Format-WorkPath $root) -replace '/', '\'
        foreach ($p in $badParents) { if ($missingParents -contains $p) { $lines += "New-Item -ItemType Directory $(Format-WorkWord $p)" } }
        foreach ($p in $badParents) { $lines += "icacls $(Format-WorkWord $p) /grant $(Format-WorkWord "${account}:(RA,REA)")" }
        if ($rootMissing) { $lines += "New-Item -ItemType Directory $(Format-WorkWord $wr)" }
        if ($rootMissing -or $rootNotWritable) { $lines += "icacls $(Format-WorkWord $wr) /grant $(Format-WorkWord "${account}:(OI)(CI)F")" }
    } else {
        $wr = Format-WorkPath $root
        foreach ($p in $badParents) { if ($p -ne '/' -and $missingParents -contains $p) { $lines += "sudo install -d -m 755 $(Format-WorkWordSh $p)" } }
        foreach ($p in $badParents) {
            if ($p -eq '/') { continue }
            $lines += if ($os -eq 'darwin') { "sudo chmod +a $(Format-WorkWordSh "user:$account allow search") $(Format-WorkWordSh $p)" } else { "sudo setfacl -m $(Format-WorkWordSh "u:${account}:x") $(Format-WorkWordSh $p)" }
        }
        if ($rootMissing) { $lines += "sudo install -d -o $account -m 755 $(Format-WorkWordSh $wr)" }
        elseif ($rootNotWritable) { $lines += "sudo chown -R ${account}: $(Format-WorkWordSh $wr)" }
    }
    $lines
}

# ── the work root on the remote ─────────────────────────────────────────────

# Get-WorkProbeScript is the script that reads, and with $make makes, the work root, as the ssh login. Windows is PowerShell,
# Unix is sh. It prints key=value lines: login, drive (False when the drive is not there), parents and parentN (ok, missing or
# fail: can its attributes be read), exists, made, writable. It writes nothing but the root itself, and only when $make: a
# read only check reads the Windows folder's ACL rather than writing a probe file, and the Unix one asks `test -w`. Examining a parent is Get-Item on Windows and a search permission test on
# Unix, which is what Claude Code asks of each one. It never lists or creates at a parent.
function Get-WorkProbeScript {
    param([string] $os, [string] $root, [bool] $make)
    $parents = @(Get-WorkParents $root $os)
    if ($os -eq 'windows') {
        $wr = (Format-WorkPath $root) -replace '/', '\'
        $ps = ($parents | ForEach-Object { ConvertTo-PsLiteral $_ }) -join ', '
        return "`$root = $(ConvertTo-PsLiteral $wr); `$mk = `$$make`n`$parents = @($ps)`n" + @'
"login=$([Security.Principal.WindowsIdentity]::GetCurrent().Name)"
"drive=$([bool]([IO.DriveInfo]::GetDrives() | Where-Object { $_.Name -eq $parents[0] }))"
"parents=$($parents.Count)"
for ($i = 0; $i -lt $parents.Count; $i++) {
    try { $null = Get-Item -LiteralPath $parents[$i] -Force -ErrorAction Stop; "parent$i=ok" }
    catch { if ($_.Exception -is [System.Management.Automation.ItemNotFoundException]) { "parent$i=missing" } else { "parent$i=fail" } }
}
if (Test-Path -LiteralPath $root -PathType Container) { 'exists=True' }
else {
    'exists=False'
    if ($mk) { try { New-Item -ItemType Directory -Force -Path $root -ErrorAction Stop | Out-Null; 'made=True' } catch { 'made=False' } }
}
if (Test-Path -LiteralPath $root -PathType Container) {
    if ($mk) {
        $t = Join-Path $root ".atrium-probe-$PID"
        try { [IO.File]::WriteAllText($t, 'x'); Remove-Item -LiteralPath $t -Force; 'writable=True' } catch { 'writable=False' }
    } else {
        # -Check writes nothing, so the answer is the folder's ACL read against this login's SIDs: a folder is writable when
        # an Allow entry that applies to the folder itself gives CreateFiles and AppendData and no Deny entry takes either.
        try {
            $sids = @([Security.Principal.WindowsIdentity]::GetCurrent().User.Value) + @([Security.Principal.WindowsIdentity]::GetCurrent().Groups | ForEach-Object { $_.Value })
            $need = 6; $allow = 0; $deny = 0
            foreach ($r in (Get-Acl -LiteralPath $root -ErrorAction Stop).Access) {
                if ($r.PropagationFlags -band [Security.AccessControl.PropagationFlags]::InheritOnly) { continue }
                try { $sid = $r.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value } catch { continue }
                if ($sids -notcontains $sid) { continue }
                if ($r.AccessControlType -eq 'Deny') { $deny = $deny -bor [int]$r.FileSystemRights } else { $allow = $allow -bor [int]$r.FileSystemRights }
            }
            if ((($allow -band $need) -eq $need) -and (($deny -band $need) -eq 0)) { 'writable=True' } else { 'writable=False' }
        } catch { 'writable=False' }
    }
}
'@
    }
    $wr = Format-WorkPath $root
    $ps = ($parents | ForEach-Object { ConvertTo-ShLiteral $_ }) -join ' '
    "root=$(ConvertTo-ShLiteral $wr); mk=$(if ($make) { 1 } else { 0 })`nn=0; for p in $ps; do if [ -x `"`$p`" ]; then echo `"parent`$n=ok`"; elif [ ! -e `"`$p`" ]; then echo `"parent`$n=missing`"; else echo `"parent`$n=fail`"; fi; n=`$((n+1)); done`n" + @'
echo "login=$(id -un)"
echo drive=True
echo "parents=$n"
if [ -d "$root" ]; then echo exists=True
else
  echo exists=False
  if [ "$mk" = 1 ]; then if mkdir -p "$root" 2>/dev/null; then echo made=True; else echo made=False; fi; fi
fi
if [ -d "$root" ]; then if [ -w "$root" ] && [ -x "$root" ]; then echo writable=True; else echo writable=False; fi; fi
'@
}

# ConvertFrom-WorkProbe reads what the probe printed.
function ConvertFrom-WorkProbe {
    param($lines, [int] $parentCount)
    $kv = @{}
    foreach ($l in @($lines)) { $s = "$l"; $i = $s.IndexOf('='); if ($i -gt 0) { $kv[$s.Substring(0, $i).Trim()] = $s.Substring($i + 1).TrimEnd() } }
    $bad = @(); $gone = @(); $seen = 0
    for ($i = 0; $i -lt $parentCount; $i++) {
        if ($kv.ContainsKey("parent$i")) { $seen++; if ($kv["parent$i"] -ne 'ok') { $bad += $i }; if ($kv["parent$i"] -eq 'missing') { $gone += $i } }
    }
    [pscustomobject]@{
        Ok = ($kv.ContainsKey('parents') -and $seen -eq $parentCount)
        Login = $kv.login; DriveMissing = ($kv.drive -eq 'False'); BadIndexes = $bad; MissingIndexes = $gone
        Exists = ($kv.exists -eq 'True'); Made = ($kv.made -eq 'True'); MadeFailed = ($kv.made -eq 'False'); Writable = ($kv.writable -eq 'True')
    }
}

# Get-WorkRootVerdict turns a probe into the step line and the exit code. Code 13 is "an administrator has to do something
# first", with the lines in AdminLines, and nothing was changed beyond what $Make made. $Make false is -Check: what a run would
# do is said and nothing is written.
function Get-WorkRootVerdict {
    param([string] $os, [string] $root, $probe, [bool] $make, [string] $remoteHost)
    $parents = @(Get-WorkParents $root $os)
    $acct = if ($probe.Login) { $probe.Login } else { 'the-room-account' }
    $mk = { param($status, $detail, $code, $admin) [pscustomobject]@{ Status = $status; Detail = $detail; Code = $code; AdminLines = @($admin) } }
    if (-not $probe.Ok) { return (& $mk 'fail' "could not read the work root's parent folders on $remoteHost" 3 @()) }
    if ($probe.DriveMissing) {
        $d = $parents[0]
        return (& $mk 'fail' "the drive $d is not there for $acct on $remoteHost (a mapped drive belongs to one logon session and is not there over ssh). pick a -WorkRoot on a drive that is" 13 @())
    }
    $bad = @($probe.BadIndexes | ForEach-Object { $parents[$_] })
    $gone = @($probe.MissingIndexes | ForEach-Object { $parents[$_] })
    $closed = @($bad | Where-Object { $gone -notcontains $_ })
    $missing = (-not $probe.Exists) -and (-not $probe.Made)
    $noWrite = $probe.Exists -and (-not $probe.Writable) -and (-not $probe.Made)
    if ($bad.Count -or ($make -and ($probe.MadeFailed -or $noWrite))) {
        $admin = @(Get-WorkAdminLines $os $acct $root $bad $missing $noWrite $gone)
        $why = @()
        if ($gone.Count) { $why += "$($gone -join ', ') $(if ($gone.Count -eq 1) { 'is' } else { 'are' }) not there, and $acct may not make $(if ($gone.Count -eq 1) { 'it' } else { 'them' })" }
        if ($closed.Count) { $why += "$acct cannot examine $($closed -join ', '). Claude Code examines every folder on the way to a path it writes and raises its own unanswerable prompt when it cannot" }
        elseif ($gone.Count) { $why += "once made, each is given to $acct as an examine-only entry, because Claude Code examines every folder on the way to a path it writes and raises its own unanswerable prompt when it cannot" }
        if ($missing -and $make) { $why += "$root is missing and $acct may not make it" }
        if ($noWrite) { $why += "$root exists and $acct cannot write to it" }
        return (& $mk 'fail' ("$($why -join '. '). this never needs admin itself. an administrator runs the lines below, which give the attributes of the parent folders and nothing else (no listing, no creating, not inherited)") 13 $admin)
    }
    if (-not $make) {
        if (-not $probe.Exists) { return (& $mk 'warn' "$root is missing on $remoteHost, and every parent folder is examinable by $acct. a run without -Check makes it" 0 @()) }
        if (-not $probe.Writable) { return (& $mk 'warn' "$root exists and $acct cannot write to it" 0 @()) }
    }
    $word = if ($probe.Made) { 'done' } else { 'ok' }
    $what = if ($probe.Made) { "$root made" } else { "$root is there" }
    (& $mk $word "$what, writable by $acct, and $(if ($parents.Count) { "every parent ($($parents -join ', ')) is examinable" } else { 'it has no parent' })" 0 @())
}

# ── the settings and caches ─────────────────────────────────────────────────

# Get-WorkCacheScript sets the tool caches for the ssh login's account, and makes the work folders when $make. It prints
# key=value lines: dirs (made or ok), then npm, gomod, gocache, pip, cargo, each ok, done or todo (a write that $make false did
# not do), and where a tool is installed its own answer, npmtool, gotool, gocachetool, piptool. A file is edited in place: its
# other lines stay, a repeated key is cut to one, and nothing is written when it already says the right thing.
function Get-WorkCacheScript {
    param([string] $os, $layout, [bool] $make)
    $dirs = @(Get-WorkDirs $layout)
    if ($os -eq 'windows') {
        $d = ($dirs | ForEach-Object { ConvertTo-PsLiteral (($_ -replace '/', '\')) }) -join ', '
        return "`$mk = `$$make`n`$dirs = @($d)`n`$npm = $(ConvertTo-PsLiteral $layout.Npm); `$gomod = $(ConvertTo-PsLiteral $layout.GoMod); " +
            "`$gocache = $(ConvertTo-PsLiteral $layout.GoBuild); `$pip = $(ConvertTo-PsLiteral $layout.Pip); `$cargo = $(ConvertTo-PsLiteral $layout.Cargo)`n" + $script:WorkCacheWin
    }
    $d = ($dirs | ForEach-Object { ConvertTo-ShLiteral $_ }) -join ' '
    # POSIX sh, not bash: Invoke-Remote pipes this to `sh -s`, which is dash on Debian and Ubuntu. The folders ride in the
    # positional parameters (`set --`), not an array.
    "mk=$(if ($make) { 1 } else { 0 }); set -- $d`nnpm=$(ConvertTo-ShLiteral $layout.Npm); gomod=$(ConvertTo-ShLiteral $layout.GoMod); " +
        "gocache=$(ConvertTo-ShLiteral $layout.GoBuild); pip=$(ConvertTo-ShLiteral $layout.Pip); cargo=$(ConvertTo-ShLiteral $layout.Cargo)`n" + $script:WorkCacheSh
}

# The Windows body. Set-KeyLine is a key=value file (.npmrc, go's env file), Set-IniKey is the pip.ini one. Both return ok,
# done or todo.
$script:WorkCacheWin = @'
# Windows PowerShell 5.1 reads a file with no BOM as ANSI, and these files are rewritten as UTF-8, so every read says UTF-8.
function Read-Lines { param($file) [IO.File]::ReadAllLines($file, (New-Object Text.UTF8Encoding $false)) }
function Write-Lines { param($file, $lines)
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $file) | Out-Null
    [IO.File]::WriteAllText($file, (($lines -join "`r`n") + "`r`n"), (New-Object Text.UTF8Encoding $false))
}
function Set-KeyLine { param($file, $key, $val)
    $line = "$key=$val"
    $old = if (Test-Path -LiteralPath $file) { @(Read-Lines $file) } else { @() }
    $new = @(); $placed = $false
    foreach ($l in $old) {
        if ($l -match ('^\s*' + [regex]::Escape($key) + '\s*=')) { if (-not $placed) { $new += $line; $placed = $true } }
        else { $new += $l }
    }
    if (-not $placed) { $new += $line }
    if (($new -join "`n") -ceq ($old -join "`n")) { return 'ok' }
    if (-not $mk) { return 'todo' }
    Write-Lines $file $new; 'done'
}
function Set-IniKey { param($file, $sec, $key, $val)
    $line = "$key = $val"
    $old = if (Test-Path -LiteralPath $file) { @(Read-Lines $file) } else { @() }
    $new = @(); $in = $false; $seen = $false
    foreach ($l in $old) {
        if ($l -match '^\s*\[.*\]') {
            $in = ($l -match ('^\s*\[' + [regex]::Escape($sec) + '\]\s*$'))
            $new += $l
            if ($in -and -not $seen) { $new += $line; $seen = $true }
            continue
        }
        if ($in -and $l -match ('^\s*' + [regex]::Escape($key) + '\s*[=:]')) { continue }
        $new += $l
    }
    if (-not $seen) { $new += "[$sec]"; $new += $line }
    if (($new -join "`n") -ceq ($old -join "`n")) { return 'ok' }
    if (-not $mk) { return 'todo' }
    Write-Lines $file $new; 'done'
}
$made = 0
foreach ($d in $dirs) { if (-not (Test-Path -LiteralPath $d -PathType Container)) { if ($mk) { New-Item -ItemType Directory -Force -Path $d | Out-Null; $made++ } else { $made++ } } }
"dirs=$(if ($made) { if ($mk) { "made $made" } else { "todo $made" } } else { 'ok' })"
"npm=$(Set-KeyLine (Join-Path $env:USERPROFILE '.npmrc') 'cache' $npm)"
$goenv = Join-Path $env:APPDATA 'go\env'
"gomod=$(Set-KeyLine $goenv 'GOMODCACHE' $gomod)"
"gocache=$(Set-KeyLine $goenv 'GOCACHE' $gocache)"
"pip=$(Set-IniKey (Join-Path $env:APPDATA 'pip\pip.ini') 'global' 'cache-dir' $pip)"
$cur = [Environment]::GetEnvironmentVariable('CARGO_HOME', 'User')
if ($cur -ceq $cargo) { 'cargo=ok' } elseif (-not $mk) { 'cargo=todo' } else { [Environment]::SetEnvironmentVariable('CARGO_HOME', $cargo, 'User'); 'cargo=done' }
$ErrorActionPreference = 'Continue'
if (Get-Command npm -ErrorAction SilentlyContinue) { "npmtool=$((& npm config get cache 2>$null | Select-Object -First 1))" }
if (Get-Command go -ErrorAction SilentlyContinue) { "gotool=$((& go env GOMODCACHE 2>$null | Select-Object -First 1))"; "gocachetool=$((& go env GOCACHE 2>$null | Select-Object -First 1))" }
if (Get-Command pip -ErrorAction SilentlyContinue) { "piptool=$((& pip config get global.cache-dir 2>$null | Select-Object -First 1))" }
'@

# The Unix body: the same, with awk. A file is rebuilt in a temp file and compared with cmp, so an unchanged one is not written.
$script:WorkCacheSh = @'
apply() { f=$1; t=$2; if cmp -s "$t" "$f" 2>/dev/null; then rm -f "$t"; echo ok; return; fi
  if [ "$mk" = 1 ]; then mkdir -p "$(dirname "$f")"; cat "$t" > "$f"; rm -f "$t"; echo done; else rm -f "$t"; echo todo; fi; }
setkv() { f=$1; k=$2; v=$3; t="${TMPDIR:-/tmp}/atrium-kv.$$"; src="$f"; if [ ! -f "$f" ]; then : > "$t.empty"; src="$t.empty"; fi
  K="$k" L="$k=$v" awk 'BEGIN { k=ENVIRON["K"]; l=ENVIRON["L"]; p=0 } $0 ~ "^[ \t]*" k "[ \t]*=" { if (!p) { print l; p=1 }; next } { print } END { if (!p) print l }' "$src" > "$t"
  rm -f "$t.empty"; apply "$f" "$t"; }
setini() { f=$1; s=$2; k=$3; v=$4; t="${TMPDIR:-/tmp}/atrium-ini.$$"; src="$f"; if [ ! -f "$f" ]; then : > "$t.empty"; src="$t.empty"; fi
  S="$s" K="$k" L="$k = $v" awk 'BEGIN { sec=ENVIRON["S"]; k=ENVIRON["K"]; l=ENVIRON["L"]; inn=0; seen=0 }
    /^[ \t]*\[.*\]/ { inn = ($0 ~ "^[ \t]*\\[" sec "\\][ \t]*$"); print; if (inn && !seen) { print l; seen=1 }; next }
    inn && $0 ~ "^[ \t]*" k "[ \t]*[=:]" { next }
    { print }
    END { if (!seen) { print "[" sec "]"; print l } }' "$src" > "$t"
  rm -f "$t.empty"; apply "$f" "$t"; }
shq() { printf "'%s'" "$(printf %s "$1" | sed "s/'/'\\\\''/g")"; }
made=0
for d in "$@"; do if [ ! -d "$d" ]; then if [ "$mk" = 1 ]; then mkdir -p "$d" && made=$((made+1)); else made=$((made+1)); fi; fi; done
if [ "$made" = 0 ]; then echo dirs=ok; elif [ "$mk" = 1 ]; then echo "dirs=made $made"; else echo "dirs=todo $made"; fi
echo "npm=$(setkv "$HOME/.npmrc" cache "$npm")"
gcfg="${XDG_CONFIG_HOME:-$HOME/.config}"; if [ "$(uname -s)" = Darwin ]; then gcfg="$HOME/Library/Application Support"; fi
echo "gomod=$(setkv "$gcfg/go/env" GOMODCACHE "$gomod")"
echo "gocache=$(setkv "$gcfg/go/env" GOCACHE "$gocache")"
echo "pip=$(setini "${XDG_CONFIG_HOME:-$HOME/.config}/pip/pip.conf" global cache-dir "$pip")"
pf="$HOME/.profile"; t="${TMPDIR:-/tmp}/atrium-prof.$$"
{ if [ -f "$pf" ]; then grep -v '# atrium work root$' "$pf"; fi; echo "export CARGO_HOME=$(shq "$cargo")  # atrium work root"; } > "$t"
echo "cargo=$(apply "$pf" "$t")"
if command -v npm >/dev/null 2>&1; then echo "npmtool=$(npm config get cache 2>/dev/null </dev/null | head -1)"; fi
if command -v go >/dev/null 2>&1; then echo "gotool=$(go env GOMODCACHE 2>/dev/null </dev/null | head -1)"; echo "gocachetool=$(go env GOCACHE 2>/dev/null </dev/null | head -1)"; fi
if command -v pip >/dev/null 2>&1; then echo "piptool=$(pip config get global.cache-dir 2>/dev/null </dev/null | head -1)"; fi
'@

# Get-WorkCacheVerdict turns what the cache script printed into step lines: one for the folders, one for the caches. A tool's own
# answer that differs from what was written is a warn, since something else (GOENV, a project .npmrc) is overriding it.
function Get-WorkCacheVerdict {
    param($lines, $layout, [bool] $make)
    $kv = @{}
    foreach ($l in @($lines)) { $s = "$l"; $i = $s.IndexOf('='); if ($i -gt 0) { $kv[$s.Substring(0, $i).Trim()] = $s.Substring($i + 1).TrimEnd() } }
    $steps = @()
    $names = @('npm', 'gomod', 'gocache', 'pip', 'cargo')
    if (-not $kv.ContainsKey('dirs') -or ($names | Where-Object { -not $kv.ContainsKey($_) })) {
        return @([pscustomobject]@{ Step = 'work-cache'; Status = 'fail'; Detail = 'the remote did not answer for the folders and caches' })
    }
    $steps += [pscustomobject]@{ Step = 'work-dirs'; Status = $(if ($kv.dirs -like 'made*') { 'done' } elseif ($kv.dirs -like 'todo*') { 'warn' } else { 'ok' }); Detail = $(if ($kv.dirs -eq 'ok') { 'the folders under the work root are there' } elseif ($kv.dirs -like 'made*') { "$($kv.dirs) under $($layout.Root)" } else { "$($kv.dirs) under $($layout.Root). a run without -Check makes them" }) }
    $todo = @($names | Where-Object { $kv[$_] -eq 'todo' })
    $done = @($names | Where-Object { $kv[$_] -eq 'done' })
    $want = @{ npm = $layout.Npm; gomod = $layout.GoMod; gocache = $layout.GoBuild; pip = $layout.Pip }
    $tools = @(@('npmtool', 'npm', $layout.Npm), @('gotool', 'go GOMODCACHE', $layout.GoMod), @('gocachetool', 'go GOCACHE', $layout.GoBuild), @('piptool', 'pip', $layout.Pip))
    $diff = @()
    if (-not $todo.Count) {
        foreach ($t in $tools) {
            if ($kv.ContainsKey($t[0]) -and (Format-WorkPath $kv[$t[0]]) -ine (Format-WorkPath $t[2])) { $diff += "$($t[1]) says $($kv[$t[0]])" }
        }
    }
    $set = "npm $($layout.Npm), go $($layout.GoMod) and $($layout.GoBuild), pip $($layout.Pip), cargo $($layout.Cargo)"
    if ($todo.Count) { $steps += [pscustomobject]@{ Step = 'work-cache'; Status = 'warn'; Detail = "$($todo -join ', ') not yet pointed at the work root. a run without -Check does it" } }
    elseif ($diff.Count) { $steps += [pscustomobject]@{ Step = 'work-cache'; Status = 'warn'; Detail = "written, but $($diff -join '. ') and not the work root, so something overrides the file (GOENV, a project .npmrc). $set" } }
    elseif ($done.Count) { $steps += [pscustomobject]@{ Step = 'work-cache'; Status = 'done'; Detail = "$($done -join ', ') set. $set. a process that was already running reads them at its next start" } }
    else { $steps += [pscustomobject]@{ Step = 'work-cache'; Status = 'ok'; Detail = "already pointed at the work root. $set" } }
    $steps
}

# Invoke-WorkRoot runs the whole work-root check and, with $Make, setup, through $Remote (a script block taking a script and
# returning @{ Out; Code }). Returns Steps (Step, Status, Detail), AdminLines and Code (0 or 13, 3 for a remote that did not
# answer). The caches are only touched once the root is sound.
function Invoke-WorkRoot {
    param([scriptblock] $Remote, [string] $Os, [string] $Root, [bool] $Make, [string] $RemoteHost)
    $layout = Get-WorkLayout $Root
    $osWhy = Test-WorkRootOs $Os $Root
    if ($osWhy) {
        return [pscustomobject]@{ Steps = @([pscustomobject]@{ Step = 'work-root'; Status = 'fail'; Detail = "-WorkRoot $Root $osWhy" }); AdminLines = @(); Code = 1; Layout = $layout }
    }
    $parents = @(Get-WorkParents $Root $Os)
    $pr =& $Remote (Get-WorkProbeScript $Os $Root $Make)
    $probe = ConvertFrom-WorkProbe $pr.Out $parents.Count
    $v = Get-WorkRootVerdict $Os $Root $probe $Make $RemoteHost
    $steps = @([pscustomobject]@{ Step = 'work-root'; Status = $v.Status; Detail = $v.Detail })
    if ($v.Status -eq 'fail') { return [pscustomobject]@{ Steps = $steps; AdminLines = $v.AdminLines; Code = $v.Code; Layout = $layout } }
    if (-not $probe.Exists -and -not $probe.Made) { return [pscustomobject]@{ Steps = $steps; AdminLines = @(); Code = 0; Layout = $layout } }
    $cr = & $Remote (Get-WorkCacheScript $Os $layout $Make)
    $steps += @(Get-WorkCacheVerdict $cr.Out $layout $Make)
    $code = if (@($steps | Where-Object { $_.Status -eq 'fail' }).Count) { 3 } else { 0 }
    [pscustomobject]@{ Steps = $steps; AdminLines = @(); Code = $code; Layout = $layout }
}

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

# ── the agent pack ──────────────────────────────────────────────────────────

# The agents a pull-request panel names by default, which room-check says are missing when the pack lacks them. It is the
# agents of DefaultPRPanel in internal/store/prs.go, and scripts/test-room-work.ps1 reads that file to prove they are the same.
$script:PanelAgents = @('c-systems-reviewer', 'go-security-reviewer', 'functional-tester', 'nonfunctional-tester')

# Get-AgentPackFiles is what goes into the pack from a dotfiles checkout: claude/agents/*.md and every regular file under
# claude/skills/<name>/ for a skill that has a SKILL.md. Each is a path relative to claude/, with forward slashes. A symlink is
# not a file of the pack: a skill that is a link into another repository is not fetched, and is returned in Skipped.
function Get-AgentPackFiles {
    param([string] $claudeDir)
    $files = @(); $skipped = @()
    $ad = Join-Path $claudeDir 'agents'
    if (Test-Path -LiteralPath $ad) {
        foreach ($f in @(Get-ChildItem -LiteralPath $ad -Filter '*.md' -File | Sort-Object Name)) {
            if ($f.LinkType) { $skipped += "agents/$($f.Name) (a link)"; continue }
            $files += "agents/$($f.Name)"
        }
    }
    $sd = Join-Path $claudeDir 'skills'
    if (Test-Path -LiteralPath $sd) {
        foreach ($d in @(Get-ChildItem -LiteralPath $sd -Directory | Sort-Object Name)) {
            if ($d.LinkType) { $skipped += "skills/$($d.Name) (a link to $($d.Target -join ', '))"; continue }
            if (-not (Test-Path -LiteralPath (Join-Path $d.FullName 'SKILL.md'))) { continue }
            foreach ($f in @(Get-ChildItem -LiteralPath $d.FullName -Recurse -File -Force | Sort-Object FullName)) {
                if ($f.LinkType) { continue }
                $rel = $f.FullName.Substring($claudeDir.TrimEnd('\', '/').Length + 1) -replace '\\', '/'
                $files += $rel
            }
        }
    }
    $sorted = [string[]]@($files); [Array]::Sort($sorted, [StringComparer]::Ordinal)
    [pscustomobject]@{ Files = @($sorted); Skipped = $skipped }
}

# New-AgentPackMeta is the record that rides in the pack and is written to ~/.claude/atrium-agent-pack.json: the repo, branch and
# commit it came from, and every file with its SHA-256.
function New-AgentPackMeta {
    param([string] $claudeDir, [string[]] $files, [string] $repo, [string] $branch, [string] $commit)
    $h = [ordered]@{}
    foreach ($f in $files) { $h[$f] = (Get-FileHash -LiteralPath (Join-Path $claudeDir $f) -Algorithm SHA256).Hash.ToLower() }
    [pscustomobject]@{
        repo = $repo; branch = $branch; commit = $commit
        agents = @($files | Where-Object { $_ -match '^agents/[^/]+\.md$' } | ForEach-Object { ($_ -replace '^agents/', '') -replace '\.md$', '' })
        skills = @($files | Where-Object { $_ -match '^skills/([^/]+)/' } | ForEach-Object { ($_ -split '/')[1] } | Select-Object -Unique)
        files = [pscustomobject] $h
    }
}

# Get-HubMirrorUrl is where the hub serves a repository it mirrors. The host is `github`, as in the hub's own git URLs.
function Get-HubMirrorUrl { param([string] $hubAddr, [string] $repo) "http://$hubAddr/git/hub/github/$($repo.Trim('/')).git" }

# Get-HubMirrorCommit is the commit the hub's mirror has for a branch, or $null when it cannot be asked. Reads refs only.
function Get-HubMirrorCommit {
    param([string] $hubAddr, [string] $repo, [string] $branch)
    if (Test-AgentPackArg $repo $branch) { return $null }
    $was = $env:GIT_TERMINAL_PROMPT; $env:GIT_TERMINAL_PROMPT = '0'
    try { $o = & git ls-remote (Get-HubMirrorUrl $hubAddr $repo) "refs/heads/$branch" 2>$null }
    finally { if ($null -eq $was) { Remove-Item Env:GIT_TERMINAL_PROMPT -ErrorAction SilentlyContinue } else { $env:GIT_TERMINAL_PROMPT = $was } }
    if ($LASTEXITCODE -ne 0) { return $null }
    $l = @($o | ForEach-Object { "$_" } | Where-Object { $_ -match "^[0-9a-f]{40}\s+refs/heads/$([regex]::Escape($branch))$" } | Select-Object -First 1)
    if ($l.Count) { ($l[0] -split '\s+')[0] } else { $null }
}

# Get-PackTar is bsdtar where Windows ships it, since the GNU tar a Windows bash puts first on the PATH reads C:\x as a host.
function Get-PackTar { $w = Join-Path $env:SystemRoot 'System32\tar.exe'; if ($IsWindows -ne $false -and (Test-Path -LiteralPath $w)) { $w } else { 'tar' } }

# New-AgentPack fetches the repository from the hub's mirror, WHOLE (the hub serves whole fetches only), and packs the agents
# and skills of its claude/ folder into <OutDir>/agent-pack.tgz, with the record that rides in it as .atrium-pack.json. The
# checkout on this machine is never read. Returns Ok, Why, Commit, Tgz, Meta, Skipped.
function New-AgentPack {
    param([string] $HubAddr, [string] $Repo, [string] $Branch, [string] $OutDir)
    $fail = { param($why) [pscustomobject]@{ Ok = $false; Why = $why; Commit = $null; Tgz = $null; Meta = $null; Skipped = @() } }
    $bad = Test-AgentPackArg $Repo $Branch
    if ($bad) { return (& $fail $bad) }
    $url = Get-HubMirrorUrl $HubAddr $Repo
    $src = Join-Path $OutDir 'agent-pack-src'; $stage = Join-Path $OutDir 'agent-pack-stage'
    foreach ($d in $src, $stage) { if (Test-Path -LiteralPath $d) { Remove-Item -LiteralPath $d -Recurse -Force } }
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
    $was = $env:GIT_TERMINAL_PROMPT; $env:GIT_TERMINAL_PROMPT = '0'
    try { $o = & git clone -q -b $Branch -- $url $src 2>&1 }
    finally { if ($null -eq $was) { Remove-Item Env:GIT_TERMINAL_PROMPT -ErrorAction SilentlyContinue } else { $env:GIT_TERMINAL_PROMPT = $was } }
    if ($LASTEXITCODE -ne 0) { return (& $fail "could not fetch $Branch of $url from the hub's mirror: $((@($o) | Select-Object -First 1))") }
    $commit = (& git -C $src rev-parse HEAD 2>$null | Select-Object -First 1)
    $claude = Join-Path $src 'claude'
    $f = Get-AgentPackFiles $claude
    if (-not @($f.Files | Where-Object { $_ -like 'agents/*' }).Count) { return (& $fail "$Repo at $commit has no claude/agents/*.md") }
    $meta = New-AgentPackMeta $claude $f.Files $Repo $Branch $commit
    foreach ($rel in $f.Files) {
        $to = Join-Path $stage $rel
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $to) | Out-Null
        Copy-Item -LiteralPath (Join-Path $claude $rel) -Destination $to
    }
    [IO.File]::WriteAllText((Join-Path $stage '.atrium-pack.json'), ($meta | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
    $tgz = Join-Path $OutDir 'agent-pack.tgz'
    if (Test-Path -LiteralPath $tgz) { Remove-Item -LiteralPath $tgz -Force }
    $t = & (Get-PackTar) -czf $tgz -C $stage . 2>&1
    if ($LASTEXITCODE -ne 0) { return (& $fail "tar could not pack the agents: $((@($t) | Select-Object -First 1))") }
    [pscustomobject]@{ Ok = $true; Why = $null; Commit = $commit; Tgz = $tgz; Meta = $meta; Skipped = $f.Skipped }
}

# Get-AgentPackInstallScript is the script that unpacks the pack the caller copied to <P>/agent-pack.tgz on the remote and puts
# each file of it in ~/.claude, a real file, replacing a link or a changed file and leaving every other file alone. Windows needs
# the tar that ships with it (Windows 10 1803 and later), Unix needs tar. With $make false it only counts what would change.
# It prints commit, files (how many), changed, and ok when the record is already what it should be.
function Get-AgentPackInstallScript {
    param([string] $os, [bool] $make)
    if ($os -eq 'windows') {
        return "`$mk = `$$make`n" + @'
$tgz = Join-Path $P 'agent-pack.tgz'; $tmp = Join-Path $P 'agent-pack'
Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$ErrorActionPreference = 'Continue'
$tarx = Join-Path $env:SystemRoot 'System32\tar.exe'; if (-not (Test-Path -LiteralPath $tarx)) { $tarx = 'tar.exe' }
& $tarx -xzf $tgz -C $tmp 2>&1 | ForEach-Object { "tar=$_" }
if ($LASTEXITCODE -ne 0) { 'unpack=fail'; exit 1 }
$ErrorActionPreference = 'Stop'
$meta = Get-Content -LiteralPath (Join-Path $tmp '.atrium-pack.json') -Raw | ConvertFrom-Json
$dst = Join-Path $env:USERPROFILE '.claude'; $changed = 0; $n = 0; $refused = @(); $edited = @()
$rec = Join-Path $dst 'atrium-agent-pack.json'
$prev = if (Test-Path -LiteralPath $rec) { try { Get-Content -LiteralPath $rec -Raw -Encoding UTF8 | ConvertFrom-Json } catch { $null } } else { $null }
$old = if ($prev) { "$($prev.commit)" } else { '' }
# A folder BELOW ~/.claude that is a link or a junction would send the write to wherever it points, which may be outside the
# account's own folder: that file is refused, and named. ~/.claude itself may be one (a dotfiles layout), it is the account's.
function Get-LinkedParent { param($rel)
    $d = $dst
    foreach ($c in @((Split-Path -Parent $rel) -split '[\\/]' | Where-Object { $_ })) {
        $d = Join-Path $d $c
        $i = Get-Item -LiteralPath $d -Force -ErrorAction SilentlyContinue
        if ($i -and $i.LinkType) { return $d }
    }
    $null
}
foreach ($f in $meta.files.PSObject.Properties) {
    $n++
    $to = Join-Path $dst $f.Name; $src = Join-Path $tmp $f.Name
    $lp = Get-LinkedParent $f.Name
    if ($lp) { $refused += "$($f.Name) ($lp is a link)"; continue }
    $it = Get-Item -LiteralPath $to -Force -ErrorAction SilentlyContinue
    $have = if ($it -and -not $it.LinkType -and -not $it.PSIsContainer) { (Get-FileHash -LiteralPath $to -Algorithm SHA256).Hash.ToLower() } else { $null }
    if ($have -eq $f.Value) { continue }
    # a file that is there, is not a link, and is not what the last pack wrote has been edited here: it is replaced, and said
    if ($have) { $was = if ($prev -and $prev.files) { $prev.files.($f.Name) } else { $null }; if ($have -ne $was) { $edited += $f.Name } }
    $changed++
    if ($mk) {
        if ($it -and ($it.LinkType -or $it.PSIsContainer)) { Remove-Item -LiteralPath $to -Recurse -Force }
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $to) | Out-Null
        Copy-Item -LiteralPath $src -Destination $to -Force
    }
}
"commit=$($meta.commit)"; "files=$n"; "changed=$changed"; "was=$old"
"refused=$($refused.Count)"; if ($refused) { "refused_files=$($refused -join ', ')" }
"edited=$($edited.Count)"; if ($edited) { "edited_files=$($edited -join ', ')" }
if ($mk -and -not $refused.Count -and ($changed -gt 0 -or $old -ne $meta.commit)) {
    $stamp = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    $meta | Add-Member -NotePropertyName installed_at -NotePropertyValue $stamp -Force
    [IO.File]::WriteAllText($rec, ($meta | ConvertTo-Json -Depth 5), (New-Object Text.UTF8Encoding $false))
    'record=written'
}
Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $tgz -Force -ErrorAction SilentlyContinue
'@
    }
    "mk=$(if ($make) { 1 } else { 0 })`n" + @'
tgz="$P/agent-pack.tgz"; tmp="$P/agent-pack"; rm -rf "$tmp"; mkdir -p "$tmp"
if ! tar -xzf "$tgz" -C "$tmp"; then echo unpack=fail; exit 1; fi
dst="$HOME/.claude"; changed=0; n=0
if command -v sha256sum >/dev/null 2>&1; then sha() { sha256sum "$1" | cut -d' ' -f1; }; else sha() { shasum -a 256 "$1" | cut -d' ' -f1; }; fi
commit=$(sed -n 's/.*"commit": *"\([^"]*\)".*/\1/p' "$tmp/.atrium-pack.json" | head -1)
sed -n -E 's/^ *"((agents|skills)\/[^"]*)": *"([0-9a-f]{64})".*/\1 \3/p' "$tmp/.atrium-pack.json" > "$tmp/.list"
rec="$dst/atrium-agent-pack.json"
: > "$tmp/.prev"; if [ -f "$rec" ]; then sed -n -E 's/^ *"((agents|skills)\/[^"]*)": *"([0-9a-f]{64})".*/\1 \3/p' "$rec" > "$tmp/.prev"; fi
refused=0; refusedf=""; edited=0; editedf=""
# a folder BELOW ~/.claude that is a link would send the write to where it points: that file is refused, and named
linked() { d="$dst"; oi=$IFS; IFS=/; set -f; for c in $(dirname "$1"); do d="$d/$c"; if [ -L "$d" ]; then IFS=$oi; set +f; lp=$d; return 0; fi; done; IFS=$oi; set +f; return 1; }
while read -r rel h; do
  n=$((n+1)); to="$dst/$rel"
  if linked "$rel"; then refused=$((refused+1)); refusedf="$refusedf${refusedf:+, }$rel ($lp is a link)"; continue; fi
  have=""; if [ -f "$to" ] && [ ! -L "$to" ]; then have=$(sha "$to"); fi
  if [ "$have" = "$h" ]; then continue; fi
  if [ -n "$have" ]; then prevh=$(REL="$rel" awk '$1 == ENVIRON["REL"] { print $2; exit }' "$tmp/.prev"); if [ "$have" != "$prevh" ]; then edited=$((edited+1)); editedf="$editedf${editedf:+, }$rel"; fi; fi
  changed=$((changed+1))
  if [ "$mk" = 1 ]; then rm -rf "$to"; mkdir -p "$(dirname "$to")"; cp "$tmp/$rel" "$to"; fi
done < "$tmp/.list"
was=$(sed -n 's/.*"commit": *"\([^"]*\)".*/\1/p' "$rec" 2>/dev/null | head -1)
echo "commit=$commit"; echo "files=$n"; echo "changed=$changed"; echo "was=$was"
echo "refused=$refused"; if [ "$refused" -gt 0 ]; then echo "refused_files=$refusedf"; fi
echo "edited=$edited"; if [ "$edited" -gt 0 ]; then echo "edited_files=$editedf"; fi
if [ "$mk" = 1 ] && [ "$refused" = 0 ] && { [ "$changed" -gt 0 ] || [ "$was" != "$commit" ]; }; then cp "$tmp/.atrium-pack.json" "$rec"; echo record=written; fi
rm -rf "$tmp" "$tgz"
'@
}

# Get-AgentPackStateScript reads the pack as installed, read only: the record's commit, and the agent and skill names that are
# there as files (a link is reported too, since the room account may not be able to follow it).
function Get-AgentPackStateScript {
    param([string] $os)
    if ($os -eq 'windows') {
        return @'
$c = Join-Path $env:USERPROFILE '.claude'; $rec = Join-Path $c 'atrium-agent-pack.json'
if (Test-Path -LiteralPath $rec) { try { $j = Get-Content -LiteralPath $rec -Raw | ConvertFrom-Json; "commit=$($j.commit)"; "installed_at=$($j.installed_at)"; "repo=$($j.repo)" } catch { 'record=bad' } } else { 'record=missing' }
"agents=$((@(Get-ChildItem -LiteralPath (Join-Path $c 'agents') -Filter '*.md' -File -ErrorAction SilentlyContinue | ForEach-Object { $_.BaseName }) -join ','))"
"skills=$((@(Get-ChildItem -LiteralPath (Join-Path $c 'skills') -Directory -ErrorAction SilentlyContinue | Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'SKILL.md') } | ForEach-Object { $_.Name }) -join ','))"
'@
    }
    @'
c="$HOME/.claude"; rec="$c/atrium-agent-pack.json"
if [ -f "$rec" ]; then
  echo "commit=$(sed -n 's/.*"commit": *"\([^"]*\)".*/\1/p' "$rec" | head -1)"
  echo "installed_at=$(sed -n 's/.*"installed_at": *"\([^"]*\)".*/\1/p' "$rec" | head -1)"
else echo record=missing; fi
echo "agents=$(for f in "$c"/agents/*.md; do [ -f "$f" ] && basename "$f" .md; done 2>/dev/null | tr '\n' ',' | sed 's/,$//')"
echo "skills=$(for d in "$c"/skills/*/; do [ -f "$d/SKILL.md" ] && basename "$d"; done 2>/dev/null | tr '\n' ',' | sed 's/,$//')"
'@
}

# Get-AgentPackVerdict says whether the pack on a room is current, from Get-AgentPackStateScript's lines. $latest is the commit the
# hub's mirror has now ($null when it could not be asked), $need the agents something relies on. Status is ok or warn, never fail:
# a room without the pack works, it only cannot run the operator's panels and skills.
function Get-AgentPackVerdict {
    param($lines, [string] $latest, [string[]] $need = $script:PanelAgents)
    $kv = @{}
    foreach ($l in @($lines)) { $s = "$l"; $i = $s.IndexOf('='); if ($i -gt 0) { $kv[$s.Substring(0, $i).Trim()] = $s.Substring($i + 1).TrimEnd() } }
    $have = @("$($kv.agents)" -split ',' | Where-Object { $_ })
    $missing = @($need | Where-Object { $have -notcontains $_ })
    $fix = 'run provision-room.ps1 without -NoAgentPack to install it'
    if ($kv.record -eq 'missing' -or $kv.record -eq 'bad' -or -not $kv.commit) {
        $m = if ($missing.Count) { " the agents $($missing -join ', ') that the review panel names are missing" } else { '' }
        return [pscustomobject]@{ Status = 'warn'; Detail = "the agent pack is not installed.$m $fix" }
    }
    $short = $kv.commit.Substring(0, [Math]::Min(9, $kv.commit.Length))
    if ($missing.Count) {
        return [pscustomobject]@{ Status = 'warn'; Detail = "the agents $($missing -join ', ') that the review panel names are missing, though the pack at $short is recorded. $fix" }
    }
    if ($latest -and $latest -ne $kv.commit) {
        return [pscustomobject]@{ Status = 'warn'; Detail = "the agent pack is stale: $short is installed and the hub's mirror has $($latest.Substring(0, [Math]::Min(9, $latest.Length))). $fix" }
    }
    [pscustomobject]@{ Status = 'ok'; Detail = "the agent pack at $short, $($have.Count) agents$(if ($kv.skills) { " and $(@($kv.skills -split ',').Count) skills" })$(if (-not $latest) { ' (the hub mirror could not be asked, so whether it is current is not known)' })" }
}
