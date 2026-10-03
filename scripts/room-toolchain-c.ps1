# The C profile of scripts/room-toolchain.ps1 (`-Profile c`): what a room needs to build openziti/ziti-sdk-c with MSYS2 mingw,
# vcpkg and the `cwdming` CMake preset. Dot-sourced by room-toolchain.ps1 and by scripts/test-room-toolchain-c.ps1, and
# it runs nothing when sourced.
#
# Two kinds of thing live here.
#   1. Pure functions on THIS side (quoting, the CMakeUserPresets.json content, the MSYS2 release choice, icacls parsing and
#      plans, exit codes, the needs-human summary). They are tested on any machine.
#   2. The remote payloads for a Windows room, as Windows PowerShell 5.1 text. Each ACT is a small script of its own, because
#      ssh -EncodedCommand has to stay under the 7800 characters cmd.exe allows AFTER gzip, base64 and UTF-16. Get-CPayload
#      builds one act: the variables of the call, the common helpers, the snippets the act uses, then the act.
#
# THE PRESETS. Get-CwdmingPresets is the ONE place the preset text is decided. A change to it is a change to this file only.
# It was read from openziti/ziti-sdk-c CMakePresets.json (ci-windows-x64-mingw and what it inherits) and from the shared
# preset file hot-loop uses on sg4, see docs/changes/f-c-toolchain.md.

# ── quoting ─────────────────────────────────────────────────────────────────

# A PowerShell single-quoted literal. U+2018..U+201B end a single-quoted string too, so they are doubled like '.
function Quote-Ps { param([string] $s) "'" + ($s -replace "['\u2018\u2019\u201A\u201B]", '$0$0') + "'" }
function Quote-Sh { param([string] $s) "'" + ($s -replace "'", "'\''") + "'" }

function Compress-Text {
    param([string] $s)
    $ms = [IO.MemoryStream]::new()
    $gz = [IO.Compression.GZipStream]::new($ms, [IO.Compression.CompressionMode]::Compress)
    $b = [Text.Encoding]::UTF8.GetBytes($s)
    $gz.Write($b, 0, $b.Length); $gz.Close()
    [Convert]::ToBase64String($ms.ToArray())
}

# The -EncodedCommand argument for a Windows remote: the script gzipped inside a short loader.
function New-EncodedCommand {
    param([string] $script)
    $boot = "`$ErrorActionPreference='Continue';`$ProgressPreference='SilentlyContinue';" +
        "iex (([IO.StreamReader]::new([IO.Compression.GZipStream]::new([IO.MemoryStream]::new(" +
        "[Convert]::FromBase64String('$(Compress-Text $script)')),[IO.Compression.CompressionMode]::Decompress))).ReadToEnd())"
    [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($boot))
}

# cmd.exe takes 8191 characters. The command line is `powershell -NoProfile ... -EncodedCommand ` plus this, so 7800.
$script:EncodedLimit = 7800

# ── values that go into a remote script ─────────────────────────────────────

# A Windows path as the caller typed it. Refused: relative paths, UNC, and the characters NTFS and cmd.exe cannot carry
# (" < > | * ? and a control character, newline included). A quote `'` or a curly quote is fine, Quote-Ps handles it.
function Test-WinPathArg {
    param([string] $p, [string] $name)
    if ($p -notmatch '^[A-Za-z]:[\\/]') { return "$name '$p' is not an absolute drive path like C:\work" }
    if ($p -match '["<>|*?\x00-\x1f]') { return "$name '$p' has a character a Windows path cannot hold" }
    if ($p -match '^[A-Za-z]:[\\/]*$') { return "$name '$p' is a drive root" }
    $null
}
function Test-AccountArg {
    param([string] $a)
    if ($a -notmatch '^[A-Za-z0-9_.$-]+(\\[A-Za-z0-9_.$-]+)?$') { return "bad account name '$a'" }
    $null
}
# user.name and user.email become a `git config` argument, which Windows PowerShell 5.1 passes badly when it holds a quote
# or a backslash. Refuse those rather than quote them.
function Test-GitIdentityArg {
    param([string] $v, [string] $name)
    if (-not $v.Trim()) { return "$name is empty" }
    if ($v -match '["\\\x00-\x1f`$]') { return "$name '$v' has a quote, backslash, backtick, dollar or control character" }
    if ($name -eq '-GitUserEmail' -and $v -notmatch '^[^\s@<>]+@[^\s@<>]+$') { return "$name '$v' is not an email address" }
    $null
}
# A repo for the credential check: an https URL with no user info (never a token), or owner/repo on github.com.
function ConvertTo-RepoUrl {
    param([string] $r)
    if ($r -match '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { return "https://github.com/$r.git" }
    if ($r -match '^https://[A-Za-z0-9.-]+/[A-Za-z0-9_./-]+$') { return $r }
    $null
}

# ── the C tools and the package set ─────────────────────────────────────────

# mingw-w64-x86_64-toolchain is gcc, g++, make, gdb and binutils. cmake and ninja are the build. openssl is what the preset
# points OPENSSL_ROOT_DIR at (libssl.a, libcrypto.a, include/openssl/ssl.h). pkgconf is the package that ships
# mingw64/bin/pkg-config.exe (and pkgconf.exe), which the preset points PKG_CONFIG_EXECUTABLE at. Both file lists were read from
# the packages on repo.msys2.org/mingw/mingw64.
$script:CPackages = @('mingw-w64-x86_64-toolchain', 'mingw-w64-x86_64-cmake', 'mingw-w64-x86_64-ninja', 'mingw-w64-x86_64-openssl', 'mingw-w64-x86_64-pkgconf')
# The files whose absence means the matching package is not there, in mingw64\bin unless said otherwise.
$script:CTools = @('gcc', 'g++', 'cmake', 'ninja', 'pkg-config')
# CMakeUserPresets.json version 4 needs cmake 3.23.
$script:CmakeMin = '3.23'

# The first dotted number in a tool's `--version` text: "cmake version 4.1.1", "gcc.exe (Rev8, Built by MSYS2 project) 15.2.0",
# "1.13.1", "pkgconf 3.0.7". An empty string when there is none.
function Get-ToolVersion {
    param([string] $raw)
    if ("$raw" -match '(?<!\d)(\d+\.\d+(?:\.\d+)?)') { $Matches[1] } else { '' }
}
function Test-VersionAtLeast {
    param([string] $have, [string] $need)
    if (-not $have) { return $false }
    $a = [version]((@($have -split '\.') + @('0', '0'))[0..2] -join '.')
    $b = [version]((@($need -split '\.') + @('0', '0'))[0..2] -join '.')
    $a -ge $b
}

# What a probe says about a tool: its path and the version in its --version text, from a `path|text` value.
function Split-Tool { param($kvs, [string] $key) $v = "$($kvs[$key])" -split '\|', 2; [pscustomobject]@{ Path = $v[0]; Raw = "$($v[1])"; Ver = (Get-ToolVersion "$($v[1])") } }

# What the cmsys probe says is missing, as the names the steps use.
function Get-CGaps {
    param($k)
    $g = @()
    foreach ($t in 'gcc', 'g++', 'cmake', 'ninja', 'pkg-config') { if (-not (Split-Tool $k "bin.$t").Path) { $g += $t } }
    if ($k['ssl'] -ne 'True') { $g += 'openssl' }
    $g
}
# ── the CMakeUserPresets.json content ───────────────────────────────────────

# The ONE function that says what the generated presets are. Returns the presets in file order.
#   mingw-vcpkg-base  hidden. Inherits upstream's ci-windows-x64-mingw (Ninja, the vcpkg toolchain file, gcc and g++, the static
#                     link flags, developer mode) and adds what a room needs: the host triplet (x64-mingw-static, so vcpkg builds
#                     its host tools such as pkgconf with gcc and no Visual Studio is needed), VCPKG_ROOT, the MSYS2 openssl and
#                     pkg-config, MSYS2's mingw64\bin first on PATH, Debug.
#   cwdming           what hot-loop builds with: binaryDir ${sourceDir}/build/cwdming.
#   cwdming-with-tests  the same with the tests and samples (ziti_DEVELOPER_MODE, the dev-features manifest feature).
# Paths are written with forward slashes, which CMake takes on Windows.
function Get-CwdmingPresets {
    param([string] $VcpkgRoot, [string] $Msys2Dir, [string] $BinaryCache)
    $v = $VcpkgRoot -replace '\\', '/'
    $m = ($Msys2Dir -replace '\\', '/').TrimEnd('/')
    $env = [ordered]@{
        VCPKG_ROOT       = $v
        OPENSSL_ROOT_DIR = "$m/mingw64"
        PATH             = "$m/mingw64/bin;`$penv{PATH}"
    }
    if ($BinaryCache) { $env['VCPKG_BINARY_SOURCES'] = 'clear;files,' + ($BinaryCache -replace '\\', '/') + ',readwrite' }
    @(
        [ordered]@{
            name           = 'mingw-vcpkg-base'
            hidden         = $true
            inherits       = @('ci-windows-x64-mingw')
            generator      = 'Ninja'
            environment    = $env
            cacheVariables = [ordered]@{
                CMAKE_TOOLCHAIN_FILE  = '$env{VCPKG_ROOT}/scripts/buildsystems/vcpkg.cmake'
                VCPKG_TARGET_TRIPLET  = 'x64-mingw-static'
                VCPKG_HOST_TRIPLET    = 'x64-mingw-static'
                CMAKE_C_COMPILER      = 'gcc'
                CMAKE_CXX_COMPILER    = 'g++'
                PKG_CONFIG_EXECUTABLE = "$m/mingw64/bin/pkg-config.exe"
                CMAKE_BUILD_TYPE      = 'Debug'
            }
        }
        [ordered]@{
            name        = 'cwdming'
            displayName = 'MinGW, Debug'
            inherits    = @('mingw-vcpkg-base')
            binaryDir   = '${sourceDir}/build/cwdming'
        }
        [ordered]@{
            name           = 'cwdming-with-tests'
            displayName    = 'MinGW, Debug, tests and samples'
            inherits       = @('cwdming')
            cacheVariables = [ordered]@{
                ziti_DEVELOPER_MODE    = 'ON'
                VCPKG_MANIFEST_FEATURES = 'dev-features'
            }
        }
    )
}
# What the remote merges: {"configurePresets":[...]}, compact.
function ConvertTo-PresetsJson {
    param($Presets)
    ConvertTo-Json -InputObject ([ordered]@{ configurePresets = @($Presets) }) -Depth 12 -Compress
}

# THE MERGE, as the text that runs on the remote (Windows PowerShell 5.1) and here (the test dot-sources it). It returns
# Action: created (no file), ok (every preset is there, the file is left alone), merged (the missing ones were added, the
# rest kept), invalid (the file is not something it can add to: it is left alone). Never overwrites a preset that is there.
$script:CPresetMerge = @'
function Merge-Presets($cur, $json) {
    $new = ConvertFrom-Json $json; $np = @($new.configurePresets); $names = @($np | ForEach-Object { $_.name })
    if (-not "$cur".Trim()) {
        $o = [pscustomobject]@{ version = 4; configurePresets = $np }
        return @{ Action = 'created'; Text = (ConvertTo-Json -InputObject $o -Depth 64); Added = $names; Kept = @(); Why = '' }
    }
    $bad = { param($w) @{ Action = 'invalid'; Text = ''; Added = @(); Kept = @(); Why = $w } }
    try { $o = ConvertFrom-Json $cur } catch { return (& $bad "it is not valid JSON ($($_.Exception.Message))") }
    if ($o -isnot [pscustomobject]) { return (& $bad 'its top level is not an object') }
    $ver = 0; try { $ver = [int]$o.version } catch {}
    if ($ver -lt 2) { return (& $bad 'it has no version of 2 or more') }
    $p = $o.PSObject.Properties['configurePresets']
    if (-not $p) { Add-Member -InputObject $o -NotePropertyName configurePresets -NotePropertyValue @(); $p = $o.PSObject.Properties['configurePresets'] }
    if ($null -ne $p.Value -and $p.Value -isnot [array]) { return (& $bad 'its configurePresets is not a list') }
    $have = @($p.Value | ForEach-Object { if ($_ -is [pscustomobject]) { $_.name } })
    $add = @($np | Where-Object { $_.name -notin $have }); $kept = @($names | Where-Object { $_ -in $have })
    if (-not $add.Count) { return @{ Action = 'ok'; Text = ''; Added = @(); Kept = $kept; Why = '' } }
    $keep = @(); if ($null -ne $p.Value) { $keep = @($p.Value) }
    $o.configurePresets = @($keep) + @($add)
    $text = ConvertTo-Json -InputObject $o -Depth 64 -WarningVariable jw -WarningAction SilentlyContinue
    if ($jw) { return (& $bad 'it nests deeper than can be rewritten') }
    $back = $null; try { $back = ConvertFrom-Json $text } catch {}
    if (-not $back -or @($back.configurePresets).Count -ne @($o.configurePresets).Count -or (@($back.PSObject.Properties.Name) -join ',') -ne (@($o.PSObject.Properties.Name) -join ',')) { return (& $bad 'it cannot be rewritten without losing something') }
    @{ Action = 'merged'; Text = $text; Added = @($add | ForEach-Object { $_.name }); Kept = $kept; Why = '' }
}
'@

# ── MSYS2: which release, and where it is ───────────────────────────────────

# The official self-extracting base archive. repo.msys2.org/distrib/x86_64 publishes msys2-base-x86_64-<date>.sfx.exe and a .sig
# but NO .sha256, which the publisher puts on the msys2/msys2-installer GitHub release of the same date, next to the same file
# (the bytes were compared). So the file comes from repo.msys2.org and its hash from that release. $Releases is the GitHub list.
function Select-Msys2Release {
    param($Releases, [string] $Version)
    $best = $null
    foreach ($r in @($Releases)) {
        if ($r.draft -or $r.prerelease) { continue }
        foreach ($a in @($r.assets)) {
            if ($a.name -notmatch '^msys2-base-x86_64-(\d{8})\.sfx\.exe$') { continue }
            $date = $Matches[1]
            if ($Version -and $date -ne $Version) { continue }
            $sha = @($r.assets | Where-Object { $_.name -eq "$($a.name).sha256" })[0]
            if (-not $sha) { continue }
            if (-not $best -or $date -gt $best.Date) {
                $best = [pscustomobject]@{
                    Date = $date; File = $a.name; Url = "https://repo.msys2.org/distrib/x86_64/$($a.name)"
                    ShaUrl = $sha.browser_download_url; Digest = "$($a.digest)"
                }
            }
        }
    }
    $best
}
# The 64 hex characters in the publisher's .sha256 file, or $null.
function Read-Sha256File {
    param([string] $text, [string] $file)
    foreach ($l in ("$text" -split "`n")) {
        if ($l -match '^\s*([0-9a-fA-F]{64})(\s+\*?(.+?))?\s*$') {
            if (-not $Matches[3] -or $Matches[3].Trim() -eq $file) { return $Matches[1].ToLower() }
        }
    }
    $null
}

# THE DISCOVERY, as remote text and tested here over fake directories. J joins any number of single path segments, because
# Windows PowerShell 5.1 Join-Path takes two, and [IO.Path]::Combine does not look at drives. On a Unix disk (the tests) a backslash becomes a slash. With an explicit directory that is the answer, found or not. Otherwise the
# first of C:\msys64, <prefix>\msys64 and the directory two above any mingw64\bin on the PATH record that has usr\bin\pacman.exe.
$script:CFind = @'
function Msys2Cands($ex, $pre, $rec) {
    if ($ex) { return @($ex) }
    $c = @('C:\msys64', (J $pre 'msys64'))
    foreach ($d in $rec) { if ($d -match '[\\/]mingw64[\\/]bin$') { $c += ($d -replace '[\\/]mingw64[\\/]bin$', '') } }
    @($c)
}
function Msys2Find($ex, $pre, $rec) {
    foreach ($d in (Msys2Cands $ex $pre $rec)) { if (Test-Path -LiteralPath (J $d 'usr' 'bin' 'pacman.exe')) { return $d } }
}
'@

# ── icacls ──────────────────────────────────────────────────────────────────

# `icacls <dir>` lines to ACEs. The first line carries the directory in front of the first ACE.
function ConvertFrom-Icacls {
    param([string[]] $Lines, [string] $Dir)
    $out = @()
    foreach ($raw in @($Lines)) {
        $l = "$raw"
        if ($Dir -and $l.StartsWith($Dir, [StringComparison]::OrdinalIgnoreCase)) { $l = $l.Substring($Dir.Length) }
        if ($l -notmatch '^\s*(?<acct>\S.*?):(?<aces>(\([^)]*\))+)\s*$') { continue }
        $toks = @([regex]::Matches($Matches['aces'], '\(([^)]*)\)') | ForEach-Object { $_.Groups[1].Value })
        $flags = @($toks | Where-Object { $_ -in 'I', 'OI', 'CI', 'IO', 'NP', 'DENY' })
        $out += [pscustomobject]@{
            Account = $Matches['acct'].Trim(); Rights = @($toks | Where-Object { $_ -notin 'I', 'OI', 'CI', 'IO', 'NP', 'DENY' })
            Inherited = ('I' -in $flags); InheritOnly = ('IO' -in $flags); Deny = ('DENY' -in $flags); Raw = ($Matches['aces'])
        }
    }
    $out
}
function Test-SameAccount {
    param([string] $a, [string] $b)
    ($a -split '\\')[-1] -ieq ($b -split '\\')[-1]
}
# Can this account read the directory by what the ACL says: an allow ACE for it, or for a group everyone is in.
function Test-AclReadable {
    param($Aces, [string] $Account)
    $broad = 'Everyone', 'BUILTIN\Users', 'NT AUTHORITY\Authenticated Users'
    # a deny for the account itself beats any allow
    if (@($Aces | Where-Object { $_.Deny -and -not $_.InheritOnly -and (Test-SameAccount $_.Account $Account) }).Count) { return $false }
    foreach ($e in @($Aces)) {
        if ($e.Deny -or $e.InheritOnly) { continue }
        if (-not (@($e.Rights | Where-Object { $_ -in 'F', 'M', 'RX', 'R' }).Count)) { continue }
        if ((Test-SameAccount $e.Account $Account) -or $e.Account -in $broad) { return $true }
    }
    $false
}
# What icacls prints for one ACE after the account, flags then rights: (OI)(CI)(RX), (CI)(IO)(W), (F). The one grammar for it, used
# on what New-IcaclsArgs is given and on what is read back from the room's grant record. Anything else is not an ACE.
function Test-AceRaw { param([string] $s) $s -cmatch '^(\((OI|CI|IO|NP|I)\))*\((?!(OI|CI|IO|NP|I)\))[A-Z]{1,4}(,[A-Z]{1,4})*\)\z' }
# What the room's grant record may say an account HAD before: an ACE (Test-AceRaw) whose rights are all of a kind this script could
# have seen on an account that could not write, and none above Modify. Full control (F), write DAC (WDAC), write owner (WO) and
# generic all (GA) are refused, and so is any token not listed. A record that says more than that was not written by this script.
function Test-AceBelowModify {
    param([string] $s)
    if (-not (Test-AceRaw $s)) { return $false }
    $rights = ($s -replace '^(\((OI|CI|IO|NP|I)\))*', '').Trim('(', ')')
    foreach ($t in ($rights -split ',')) { if ($t -cnotin 'M', 'RX', 'R', 'W', 'D', 'N', 'RD', 'WD', 'AD', 'REA', 'WEA', 'X', 'DC', 'RC', 'S', 'DE', 'GR', 'GW', 'GE') { return $false } }
    $true
}
# The arguments to icacls.exe for one change. Never Everyone or Users: that is refused here, not left to the caller. A right that is
# not an ACE is refused too.
function New-IcaclsArgs {
    param([string] $Dir, [string] $Account, [ValidateSet('grant', 'remove', 'restore')] [string] $Mode, [string[]] $Rights = @('RX'))
    if ($Account -match '^(Everyone|BUILTIN\\Users|Users|Authenticated Users|NT AUTHORITY\\Authenticated Users)$') { throw "a grant to '$Account' is not allowed" }
    if ($Mode -eq 'grant' -and "$($Rights[0])" -cnotmatch '^(F|M|RX|R|W|D)\z') { throw "'$($Rights[0])' is not a right to grant" }
    if ($Mode -eq 'restore') { foreach ($r in @($Rights)) { if (-not (Test-AceRaw $r)) { throw "'$r' is not an ACE" } } }
    switch ($Mode) {
        'grant'   { [string[]]@($Dir, '/grant', "${Account}:(OI)(CI)$($Rights[0])") }
        # /grant:r replaces what the account has, so the FIRST of the ACEs it had is put back with it and the rest are added
        # with /grant, all in one icacls call.
        'restore' { $a = @($Dir); $i = 0; foreach ($r in @($Rights)) { $a += $(if ($i++ -eq 0) { '/grant:r' } else { '/grant' }); $a += "${Account}:$r" }; [string[]]$a }
        'remove'  { [string[]]@($Dir, '/remove:g', $Account) }
    }
}
# THE one place a command for a person to paste is made (git config, icacls, pacman, the login, every needs-human line). Each word
# outside [A-Za-z0-9_.:\/-] is a PowerShell single-quoted literal (Quote-Ps), so a $( ), a ;, a & or a backtick in a value is text and
# runs nothing. A program that had to be quoted is called with &. Need (room-toolchain.ps1) takes words and calls this.
function Format-AdminCommand {
    param([string[]] $Words, [string] $Note)
    $q = @($Words | ForEach-Object { if ("$_" -cmatch '^[A-Za-z0-9_.:\\/-]+\z') { "$_" } else { Quote-Ps "$_" } })
    if ($q.Count -and $q[0].StartsWith("'")) { $q[0] = '& ' + $q[0] }
    $line = $q -join ' '
    if ($Note) { $line += ' # ' + ($Note -replace '[^A-Za-z0-9 ,.-]', '') }
    $line
}
# The ops that take a Modify grant back: remove what was granted, then put back what the account had of its own ($Before, the
# Raw text of each explicit ACE, may be empty).
function New-RevertOps {
    param([string] $Dir, [string] $Account, [string[]] $Before = @())
    $ops = @(, @{ Args = (New-IcaclsArgs $Dir $Account 'remove'); Account = $Account; Why = 'unmodify' })
    $b = @($Before | Where-Object { $_ })
    if ($b.Count) { $ops += , @{ Args = (New-IcaclsArgs $Dir $Account 'restore' $b); Account = $Account; Why = 'restore' } }
    $ops
}
# The grants this script made and has not taken back, as the room remembers them: one `dir|account|before` line each, `before`
# being the Raw text of the ACEs the account had, joined by a space. Each line is `n:text` as cacls prints it, n being its line
# number in the file. The file is writable by anything that runs as the room user, so a line is only believed when it is what this
# script writes: Dir the MSYS2 directory in question, Account exactly $User (only the user's own Modify is ever recorded), every
# `before` token an ACE no higher than Modify (Test-AceBelowModify). A line for another directory is not ours to touch and is skipped. Any other line is Bad:
# never acted on, never printed as a command, and named by its number with a short, control-character-free excerpt only.
# Returns @{ Grants = <Dir, Account, Before, Line>; Bad = <line numbers and excerpts as text> }.
function ConvertFrom-GrantRecord {
    param([string[]] $Lines, [string] $Dir, [string] $User)
    $grants = @(); $bad = @()
    foreach ($l in @($Lines)) {
        if (-not "$l".Trim()) { continue }
        $n = '?'; $t = "$l"
        if ($t -match '^(\d{1,6}):(.*)$') { $n = $Matches[1]; $t = $Matches[2] }
        $f = $t -split '\|', 3
        $ok = $f.Count -eq 3 -and $f[0].Trim() -and $f[1].Trim()
        if ($ok -and $Dir -and $f[0].TrimEnd('\', '/') -ine $Dir.TrimEnd('\', '/')) { continue }
        if ($ok) { $ok = $f[0] -ieq $f[0].Trim() -and $f[1] -ieq $User }
        $before = @()
        if ($ok) { $before = @(@($f[2] -split ' ') | Where-Object { $_ }); foreach ($b in $before) { if (-not (Test-AceBelowModify $b)) { $ok = $false } } }
        if ($ok) { $grants += [pscustomobject]@{ Dir = $(if ($Dir) { $Dir } else { $f[0] }); Account = $f[1]; Before = $before; Line = $n } }
        else { $bad += "line $n ($((($t -replace '[\x00-\x1f\x7f-\x9f\u2028\u2029]', ' ')).Substring(0, [Math]::Min(24, $t.Length))))" }
    }
    [pscustomobject]@{ Grants = $grants; Bad = $bad }
}
# What to do to the ACL of an MSYS2 directory. $Aces from ConvertFrom-Icacls, $User the account running this, $Writable what the
# probe measured, $Others the runner accounts that exist and are not $User, $NeedWrite whether pacman is going to run.
#   Grant   the ops that go first: RX for each other account that cannot read, then Modify for $User when pacman must write
#           and cannot. Each op is a pair of the icacls arguments and the account.
#   Revert  the ops that take Modify back after pacman, to what $User had before (only when Grant gave it).
function New-AclPlan {
    param([string] $Dir, $Aces, [string] $User, [bool] $Writable, [string[]] $Others, [bool] $NeedWrite)
    $grant = @(); $revert = @(); $rx = @(); $beforeText = ''
    foreach ($o in @($Others)) {
        if (Test-AclReadable $Aces $o) { continue }
        $rx += $o; $grant += , @{ Args = (New-IcaclsArgs $Dir $o 'grant' 'RX'); Account = $o; Why = 'rx' }
    }
    $modify = $false
    if ($NeedWrite -and -not $Writable) {
        $modify = $true
        $grant += , @{ Args = (New-IcaclsArgs $Dir $User 'grant' 'M'); Account = $User; Why = 'modify' }
        # What the user had of their own before is put back as it was: every explicit ACE with its flags and rights. An
        # inherited one comes back by itself, a deny is not touched.
        $before = @($Aces | Where-Object { (Test-SameAccount $_.Account $User) -and -not $_.Inherited -and -not $_.Deny } | ForEach-Object { $_.Raw })
        $revert = @(New-RevertOps $Dir $User $before)
        $beforeText = $before -join ' '
    }
    [pscustomobject]@{ Grant = $grant; Revert = $revert; Rx = $rx; Modify = $modify; Before = $beforeText }
}
# The ops as the line format the cacl act reads: one op per line, its arguments joined with |.
function ConvertTo-AclOps {
    param($Ops)
    (@($Ops) | ForEach-Object { ($_.Args | ForEach-Object { "$_" }) -join '|' }) -join "`n"
}

# ── can the MSYS2 target be installed into, and the git identity ─────────────

# What an MSYS2 install needs free on the target's drive: about 1.5 GB for the unpacked base, about 3 GB for what pacman adds
# (gcc, cmake, ninja, openssl and what they pull in), and the unpack goes to <dir>.tmp first. 6 GB covers that. The figure is this
# script's own estimate, not the publisher's; it is the one place to change it.
$script:Msys2NeedBytes = 6GB

# The verdict on the -Msys2Dir target from what the cdisk act measured: drive.root, drive.exists, drive.readable, anc (the
# nearest ancestor of the target that exists), anc.writable, free (bytes, -1 when the drive cannot say). Nothing is installed
# unless Ok. Otherwise Why says what is wrong and Cmds are the commands for a person, each a list of words for Format-AdminCommand.
function Test-Msys2Target {
    param($Kv, [string] $Dir, [string] $User, [long] $NeedBytes = $script:Msys2NeedBytes)
    $root = "$($Kv['drive.root'])"; $anc = "$($Kv['anc'])"
    $bad = { param($why, $cmds) [pscustomobject]@{ Ok = $false; Why = $why; Cmds = @($cmds) } }
    if ($Kv['drive.exists'] -ne 'True') {
        return (& $bad "the drive $root for $Dir does not exist for $User on the room (a mapped drive belongs to one logon session and is not there over ssh). pick a -Msys2Dir on a drive this list shows" @(, @('Get-PSDrive', '-PSProvider', 'FileSystem')))
    }
    if ($Kv['drive.readable'] -ne 'True') {
        return (& $bad "$User cannot read $root, the drive of $Dir. if it is a permission, an admin runs this (a drive that is not ready or locked is not fixed by it)" @(, (@('icacls') + (New-IcaclsArgs $root $User 'grant' 'RX'))))
    }
    if ($Kv['anc.writable'] -ne 'True') {
        return (& $bad "$User cannot write $anc, the nearest folder of $Dir that exists, so MSYS2 cannot be unpacked there. an admin, or the owner of $anc, runs this" @(, (@('icacls') + (New-IcaclsArgs $anc $User 'grant' 'M'))))
    }
    $free = [long]"$($Kv['free'])"
    if ($free -ge 0 -and $free -lt $NeedBytes) {
        return (& $bad "$root has $([Math]::Round($free / 1GB, 1)) GB free and MSYS2 with its packages needs $([Math]::Round($NeedBytes / 1GB, 1)) GB. free space, or pick a -Msys2Dir on another drive. this lists the drives" @(, @('Get-PSDrive', '-PSProvider', 'FileSystem')))
    }
    [pscustomobject]@{ Ok = $true; Why = ''; Cmds = @() }
}

# What to do about -GitUserName and -GitUserEmail against the global config. A value is set when it was given and the config does
# not say exactly that, or is empty; Lack is a value that is neither configured nor given (left for a person, never invented).
# Nothing given leaves the config alone.
function Get-GitIdentityPlan {
    param([string] $CfgName, [string] $CfgEmail, [string] $GivenName, [string] $GivenEmail)
    [pscustomobject]@{
        SetName = $(if ($GivenName -and $GivenName -cne $CfgName) { $GivenName } else { '' })
        SetEmail = $(if ($GivenEmail -and $GivenEmail -cne $CfgEmail) { $GivenEmail } else { '' })
        LackName = (-not $CfgName -and -not $GivenName); LackEmail = (-not $CfgEmail -and -not $GivenEmail)
    }
}

# ── exit code and the needs-human summary ───────────────────────────────────

# 1..5 are the existing failures. 6 is only for a run that finished and needs a person.
function Get-CExitCode {
    param([int] $Rc, [int] $NeedsHuman)
    if ($Rc -ne 0) { $Rc } elseif ($NeedsHuman -gt 0) { 6 } else { 0 }
}
function Format-NeedsHuman {
    param([string[]] $Items)
    @($Items | Where-Object { $_ } | Select-Object -Unique | ForEach-Object { "room-toolchain needs-human $_" })
}

# ── the Windows payloads ────────────────────────────────────────────────────

# Common to every C act. J is Join-Path over any number of segments. PathPre is the room's own PATH record, in front.
$script:CCommon = @'
function J { $p = [string]$args[0]; foreach ($s in $args[1..($args.Count - 1)]) { $p = [IO.Path]::Combine($p, [string]$s) }; if ([IO.Path]::DirectorySeparatorChar -eq '/') { $p = $p.Replace('\', '/') }; $p }
if ($PathPre) { $env:Path = $PathPre + ';' + $env:Path }
function Ver($e, $a = @('--version')) { try { $o = (& $e @a 2>&1 | Out-String); ($o -split "`n" | Where-Object { $_.Trim() } | Select-Object -First 1).Trim() } catch { '' } }
'@
# A native program with a time limit, in $wd (the home directory unless said). Its output tail is left in $T.
$script:CRun = @'
function Q($s) { '"' + $s + '"' }
function Run($exe, $a, $ms, $wd = $HOME) {
    $o = [IO.Path]::GetTempFileName(); $e = [IO.Path]::GetTempFileName()
    $p = Start-Process -FilePath $exe -ArgumentList $a -NoNewWindow -PassThru -RedirectStandardOutput $o -RedirectStandardError $e -WorkingDirectory $wd
    $null = $p.Handle; $rc = 124; if ($p.WaitForExit($ms)) { $rc = $p.ExitCode } else { try { $p.Kill() } catch {} }
    $script:T = @(Get-Content -LiteralPath $o -Tail 12) + @(Get-Content -LiteralPath $e -Tail 12)
    Remove-Item -LiteralPath $o, $e -Force -ErrorAction SilentlyContinue; $rc
}
'@

# act name -> @{ Uses = snippets; Body = the act }. An act reports key=value lines and, for a change, `rc=` (3 an install step
# failed, 4 a sha256 mismatch, 1 anything else), the way the install act of room-toolchain.ps1 does.
$script:CActs = @{}

# Probe MSYS2 and its tools. Writes nothing.
$script:CActs['cmsys'] = @{ Vars = @('PathPre', 'Rec', 'Msys2Dir', 'Prefix'); Uses = @('Find'); Body = @'
$R = @($Rec -split ';' | Where-Object { $_ }); $M = Msys2Find $Msys2Dir $Prefix $R
$u = try { [Security.Principal.WindowsIdentity]::GetCurrent().Name } catch { "$env:USERDOMAIN\$env:USERNAME" }; "user=$u"; "home=$HOME"
"msys2.dir=$(if ($M) { $M } elseif ($Msys2Dir) { $Msys2Dir } else { J $Prefix 'msys64' })"; "msys2.found=$([bool]$M)"
if ($M) {
    $B = J $M 'mingw64' 'bin'; $env:Path = "$B;$env:Path"
    foreach ($n in 'gcc', 'g++', 'cmake', 'ninja', 'pkg-config') { $x = J $B "$n.exe"; "bin.$n=$(if (Test-Path -LiteralPath $x) { "$x|$(Ver $x)" } else { '|' })" }
    "ssl=$((Test-Path -LiteralPath (J $M 'mingw64' 'lib' 'libssl.a')) -and (Test-Path -LiteralPath (J $M 'mingw64' 'include' 'openssl' 'ssl.h')))"
}
'@ }

# The ACL side of the same probe, for a directory cmsys found or chose: the runner accounts that exist, whether this user can
# write, the ACL, and the Modify grants an earlier run made and did not take back (see cstate). Writes nothing.
$script:CActs['cacls'] = @{ Vars = @('Msys2Dir', 'Accts', 'StateDir'); Uses = @(); Body = @'
$M = $Msys2Dir
foreach ($a in @($Accts -split ',' | Where-Object { $_ })) { $k = 'False'; try { $null = ([Security.Principal.NTAccount]$a).Translate([Security.Principal.SecurityIdentifier]); $k = 'True' } catch {}; "acct.$a=$k" }
$sf = J $(if ($StateDir) { $StateDir } else { J $HOME '.atrium' 'toolchain' }) 'acl-grants.txt'
if (Test-Path -LiteralPath $sf) { $n = 0; Get-Content -LiteralPath $sf | ForEach-Object { $n++; if ($_.Trim()) { "grant=${n}:$_" } } }
if (Test-Path -LiteralPath (J $M 'usr' 'bin' 'pacman.exe')) {
    $w = J $M 'var' 'lib' 'pacman' 'local' 'ALPM_DB_VERSION'; if (-not (Test-Path -LiteralPath $w)) { $w = J $M 'etc' 'fstab' }
    $ok = $false; try { $s = [IO.File]::Open($w, 'Open', 'Write', 'ReadWrite'); $s.Close(); $ok = $true } catch {}; "writable=$ok"
    & icacls.exe $M 2>&1 | ForEach-Object { "acl=$_" }
}
'@ }

# Can the -Msys2Dir target take an install: its drive, the nearest folder that exists, the free space. Writes nothing (the write
# probe makes one file that is deleted on close).
$script:CActs['cdisk'] = @{ Vars = @('Msys2Dir'); Uses = @(); Body = @'
$d = try { $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Msys2Dir) } catch { $Msys2Dir }
$root = [IO.Path]::GetPathRoot($d); "drive.root=$root"
$ex = $false; $rd = $false
try { $ex = Test-Path -LiteralPath $root; if ($ex) { $null = @(Get-ChildItem -LiteralPath $root -Force -ErrorAction Stop | Select-Object -First 1); $rd = $true } } catch {}
"drive.exists=$ex"; "drive.readable=$rd"
$a = $d; while ($a -and -not (Test-Path -LiteralPath $a)) { $a = Split-Path -Parent $a }
"anc=$a"; $w = $false
if ($a -and $rd) { try { $s = New-Object IO.FileStream((J $a ('.atrium-probe-' + [guid]::NewGuid().ToString('N'))), 'CreateNew', 'Write', 'None', 4096, 'DeleteOnClose'); $n = $s.Name; $s.Close(); Remove-Item -LiteralPath $n -Force -ErrorAction SilentlyContinue; $w = $true } catch {} }
"anc.writable=$w"; $f = -1; try { $f = (New-Object IO.DriveInfo $root).AvailableFreeSpace } catch {}; "free=$f"
'@ }

# Unpack the verified MSYS2 base archive. Windows PowerShell 5.1 offers only TLS 1.0 and 1.1 unless told and repo.msys2.org wants
# 1.2, so the first line asks for it, as the older payloads in room-toolchain.ps1 do. The other acts download nothing themselves.
# The sha256 is computed here and a mismatch unpacks nothing (rc=4).
$script:CActs['cinstall'] = @{ Vars = @('PathPre', 'Msys2Dir', 'Prefix', 'Url', 'File', 'Sha', 'Force'); Uses = @(); Body = @'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$tmp = "$Msys2Dir.tmp"; $dl = J $Prefix '.downloads'; $f = J $dl $File
if ((Test-Path -LiteralPath $Msys2Dir) -and @(Get-ChildItem -LiteralPath $Msys2Dir -Force).Count -and $Force -ne '1') { "err=$Msys2Dir is there and is not an MSYS2. look at it, or rerun with -Force to replace it"; 'rc=3'; exit 3 }
New-Item -ItemType Directory -Force -Path $dl | Out-Null
try { Invoke-WebRequest -Uri $Url -OutFile $f -UseBasicParsing } catch { "err=download of $Url failed: $($_.Exception.Message)"; 'rc=3'; exit 3 }
$got = (Get-FileHash -LiteralPath $f -Algorithm SHA256).Hash.ToLower()
if ($got -ne $Sha) { Remove-Item -LiteralPath $f -Force; "err=sha256 of $File is $got, the publisher says $Sha. nothing was unpacked"; 'rc=4'; exit 4 }
"sha=$got"
try {
    if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force }
    $p = Start-Process -FilePath $f -ArgumentList "-o`"$tmp`"", '-y' -Wait -PassThru -WindowStyle Hidden
    if ($p.ExitCode -ne 0) { throw "the self-extractor exited $($p.ExitCode)" }
    $src = J $tmp 'msys64'; if (-not (Test-Path -LiteralPath (J $src 'usr' 'bin' 'pacman.exe'))) { throw 'there is no msys64 in the archive' }
    if (Test-Path -LiteralPath $Msys2Dir) { Remove-Item -LiteralPath $Msys2Dir -Recurse -Force }
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Msys2Dir) | Out-Null
    Move-Item -LiteralPath $src -Destination $Msys2Dir
} catch { "err=could not unpack $File`: $($_.Exception.Message)"; 'rc=3'; exit 3 }
finally { if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }; Remove-Item -LiteralPath $f -Force -ErrorAction SilentlyContinue }
"installed=$Msys2Dir"
'@ }

# What this script has granted and not yet taken back, kept on the room so a dropped ssh or a killed run cannot leave a Modify grant
# behind unseen. A line is `dir|account|before`. Mode add (replacing a line with the same key) or remove.
$script:CActs['cstate'] = @{ Vars = @('StateDir', 'Mode', 'Key', 'Before'); Uses = @(); Body = @'
$sf = J $(if ($StateDir) { $StateDir } else { J $HOME '.atrium' 'toolchain' }) 'acl-grants.txt'
$cur = @(); if (Test-Path -LiteralPath $sf) { $cur = @(Get-Content -LiteralPath $sf | Where-Object { $_.Trim() -and -not $_.StartsWith("$Key|", 'OrdinalIgnoreCase') }) }
if ($Mode -eq 'add') { $cur += "$Key|$Before" }
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $sf) | Out-Null
if ($cur.Count) { [IO.File]::WriteAllText($sf, (($cur -join "`n") + "`n"), (New-Object Text.UTF8Encoding $false)) } elseif (Test-Path -LiteralPath $sf) { Remove-Item -LiteralPath $sf -Force }
"state=$($cur.Count)"
'@ }

# The MSYS2 dance: a first start, two core updates (the first ends the shell), then the packages with --needed.
$script:CActs['cpacman'] = @{ Vars = @('PathPre', 'Msys2Dir', 'Init', 'Pkgs'); Uses = @('Run'); Body = @'
$bash = J $Msys2Dir 'usr' 'bin' 'bash.exe'; $env:MSYSTEM = 'MSYS'; $env:CHERE_INVOKING = '1'
$steps = @(); if ($Init -eq '1') { $steps += 'exit', 'pacman -Syuu --noconfirm', 'pacman -Syuu --noconfirm' }
$steps += "pacman -S --needed --noconfirm $Pkgs"
$i = 0; $rc = 0
foreach ($c in $steps) { $i++; $rc = Run $bash @('-lc', (Q $c)) 1500000; "step.$i=$c|$rc" }
if ($rc -ne 0) { "err=pacman exited $rc"; $T | ForEach-Object { "tail=$_" }; 'rc=3'; exit 3 }
'@ }

# Run icacls ops: one per line, the arguments joined by |.
$script:CActs['cacl'] = @{ Vars = @('PathPre', 'Ops'); Uses = @(); Body = @'
$n = 0
foreach ($line in @($Ops -split "`n" | Where-Object { $_ })) {
    $a = @($line -split '\|'); $o = (& icacls.exe @a 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { "err=icacls $($a -join ' ') said: $o"; 'rc=1'; exit 1 }
    $n++
}
"ok=$n"
'@ }

# What git is configured with, for this user. Writes nothing.
$script:CActs['cgit'] = @{ Vars = @('PathPre'); Uses = @(); Body = @'
$g = Get-Command git.exe -ErrorAction SilentlyContinue | Select-Object -First 1
"git.path=$(if ($g) { $g.Source })"
if ($g) {
    "git.name=$(& git config --global user.name)"; "git.email=$(& git config --global user.email)"
    "git.helper=$(@(& git config --global --get-all credential.helper) -join '|')"; "git.syshelper=$(@(& git config --system --get-all credential.helper) -join '|')"
    "gcm=$(Ver 'git' @('credential-manager', '--version'))"
}
'@ }

# Set what is missing, and only that. Name, Email and Helper are empty when they are not to be set.
$script:CActs['cgitset'] = @{ Vars = @('PathPre', 'Name', 'Email', 'Helper'); Uses = @(); Body = @'
if ($Name) { & git config --global user.name $Name; 'set=user.name' }
if ($Email) { & git config --global user.email $Email; 'set=user.email' }
if ($Helper) { & git config --global --add credential.helper $Helper; 'set=credential.helper' }
'@ }

# Can this user reach each repo with what git has now, without a prompt. Reads nothing but the remote's refs.
$script:CActs['cauth'] = @{ Vars = @('PathPre', 'Repos'); Uses = @('Run'); Body = @'
$env:GIT_TERMINAL_PROMPT = '0'; $env:GCM_INTERACTIVE = 'never'; $i = 0
foreach ($u in @($Repos -split "`n" | Where-Object { $_ })) { $i++; $rc = Run 'git' @('ls-remote', '--exit-code', (Q $u), 'HEAD') 45000
    "auth.$i=$(if ($rc -eq 0) { 'ok' } else { 'fail' })|$u|$rc|$(($T | Where-Object { $_ } | Select-Object -First 1))" }
'@ }

# vcpkg: clone, bootstrap, and answer. With Dry=1 it only reports.
$script:CActs['cvcpkg'] = @{ Vars = @('PathPre', 'VcpkgDir', 'Url', 'Dry'); Uses = @('Run'); Body = @'
$g = J $VcpkgDir '.git'; $exe = J $VcpkgDir 'vcpkg.exe'
"vcpkg.dir=$VcpkgDir"; "vcpkg.dirhere=$(Test-Path -LiteralPath $VcpkgDir)"; "vcpkg.git=$(Test-Path -LiteralPath $g)"
"vcpkg.inway=$((Test-Path -LiteralPath $VcpkgDir) -and -not (Test-Path -LiteralPath $g) -and [bool]@(Get-ChildItem -LiteralPath $VcpkgDir -Force).Count)"
if ($Dry -ne '1') {
    if ((Test-Path -LiteralPath $VcpkgDir) -and -not (Test-Path -LiteralPath $g) -and @(Get-ChildItem -LiteralPath $VcpkgDir -Force).Count) { "err=$VcpkgDir is there and is not a git checkout. look at it, nothing was replaced"; 'rc=3'; exit 3 }
    if (-not (Test-Path -LiteralPath $g)) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $VcpkgDir) | Out-Null
        if ((Run 'git' @('clone', $Url, (Q $VcpkgDir)) 1800000) -ne 0) { "err=git clone of $Url failed"; $T | ForEach-Object { "tail=$_" }; 'rc=3'; exit 3 }; 'cloned=1'
    }
    if (-not (Test-Path -LiteralPath $exe)) {
        $env:VCPKG_DISABLE_METRICS = '1'
        if ((Run (J $VcpkgDir 'bootstrap-vcpkg.bat') @('-disableMetrics') 1800000) -ne 0) { 'err=bootstrap-vcpkg.bat failed'; $T | ForEach-Object { "tail=$_" }; 'rc=3'; exit 3 }; 'bootstrapped=1'
    }
}
"vcpkg.ver=$(if (Test-Path -LiteralPath $exe) { "$exe|$(Ver $exe @('version'))" } else { '|' })"
"vcpkg.triplet=$(Test-Path -LiteralPath (J $VcpkgDir 'triplets' 'community' 'x64-mingw-static.cmake'))"
'@ }

# The ziti-sdk-c checkout: clone when absent, leave a git checkout alone, init submodules if the repo has any.
$script:CActs['csdk'] = @{ Vars = @('PathPre', 'SdkDir', 'Url', 'Dry'); Uses = @('Run'); Body = @'
$g = J $SdkDir '.git'
"sdk.dir=$SdkDir"; "sdk.dirhere=$(Test-Path -LiteralPath $SdkDir)"; "sdk.git=$(Test-Path -LiteralPath $g)"
"sdk.inway=$((Test-Path -LiteralPath $SdkDir) -and -not (Test-Path -LiteralPath $g) -and [bool]@(Get-ChildItem -LiteralPath $SdkDir -Force).Count)"
if ($Dry -ne '1') {
    if ((Test-Path -LiteralPath $SdkDir) -and -not (Test-Path -LiteralPath $g) -and @(Get-ChildItem -LiteralPath $SdkDir -Force).Count) { "err=$SdkDir is there and is not a git checkout. look at it, nothing was replaced"; 'rc=3'; exit 3 }
    if (-not (Test-Path -LiteralPath $g)) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $SdkDir) | Out-Null
        if ((Run 'git' @('clone', $Url, (Q $SdkDir)) 1800000) -ne 0) { "err=git clone of $Url failed"; $T | ForEach-Object { "tail=$_" }; 'rc=3'; exit 3 }; 'cloned=1'
    }
    if (Test-Path -LiteralPath (J $SdkDir '.gitmodules')) {
        $rc = Run 'git' @('submodule', 'update', '--init', '--recursive') 1800000 $SdkDir
        if ($rc -ne 0) { 'err=git submodule update failed'; $T | ForEach-Object { "tail=$_" }; 'rc=3'; exit 3 }; 'submodules=1'
    }
}
if (Test-Path -LiteralPath $g) {
    Push-Location $SdkDir
    "sdk.branch=$(& git rev-parse --abbrev-ref HEAD 2>$null)"; "sdk.dirty=$([bool](& git --no-optional-locks status --porcelain 2>$null))"
    Pop-Location
    $gi = J $SdkDir '.gitignore'; "sdk.ignored=$((Test-Path -LiteralPath $gi) -and [bool](Select-String -LiteralPath $gi -SimpleMatch 'CMakeUserPresets.json' -Quiet))"
}
'@ }

# The preset JSON is too long to ride in the same payload as the merge, so it goes first, to a file of the user's Temp.
$script:CActs['cpjson'] = @{ Vars = @('Json'); Uses = @(); Body = @'
[IO.File]::WriteAllText((J $env:TEMP 'atrium-presets.json'), $Json, (New-Object Text.UTF8Encoding $false)); 'saved=1'
'@ }

# CMakeUserPresets.json in the checkout root, from the JSON cpjson left: created, or the missing presets added.
$script:CActs['cpresets'] = @{ Vars = @('PathPre', 'SdkDir'); Uses = @('Merge'); Body = @'
if (-not (Test-Path -LiteralPath $SdkDir)) { 'preset=nodir'; exit 0 }
$f = J $SdkDir 'CMakeUserPresets.json'; $jf = J $env:TEMP 'atrium-presets.json'; $Json = Get-Content -LiteralPath $jf -Raw
$cur = if (Test-Path -LiteralPath $f) { Get-Content -LiteralPath $f -Raw } else { '' }
$r = Merge-Presets $cur $Json
"preset=$($r.Action)"; "added=$($r.Added -join ',')"; "kept=$($r.Kept -join ',')"; "why=$($r.Why)"
if ($r.Action -eq 'created' -or $r.Action -eq 'merged') {
    if ("$cur".Trim() -and -not (Test-Path -LiteralPath "$f.atrium-bak")) { Copy-Item -LiteralPath $f -Destination "$f.atrium-bak" }
    [IO.File]::WriteAllText($f, $r.Text, (New-Object Text.UTF8Encoding $false)); 'wrote=1'
}
Remove-Item -LiteralPath $jf -Force -ErrorAction SilentlyContinue
'@ }

# -Check: the file as it is, read only, so the merge can be run on this side and say what a run would do.
$script:CActs['cpread'] = @{ Vars = @('SdkDir'); Uses = @(); Body = @'
$f = J $SdkDir 'CMakeUserPresets.json'
"dir=$(Test-Path -LiteralPath $SdkDir)"; "file=$(Test-Path -LiteralPath $f)"
if (Test-Path -LiteralPath $f) { "b64=$([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Get-Content -LiteralPath $f -Raw))))" }
'@ }

$script:CSnippets = @{ Find = $script:CFind; Run = $script:CRun; Merge = $script:CPresetMerge }

# One act, ready to send: the call's variables, the common helpers, the snippets it uses, then the act.
function Get-CPayload {
    param([string] $Act, [hashtable] $Vars = @{})
    $def = $script:CActs[$Act]
    if (-not $def) { throw "no C act '$Act'" }
    $head = ($Vars.Keys | Sort-Object | ForEach-Object { "`$$_ = $(Quote-Ps "$($Vars[$_])")" }) -join "`n"
    $uses = ($def.Uses | ForEach-Object { $script:CSnippets[$_] }) -join "`n"
    $head + "`n" + $script:CCommon + "`n" + $uses + "`n" + $def.Body
}

# Windows PowerShell 5.1 has no `?.`, `??`, `&&`, `||` between statements, a ternary, Join-Path with three arguments, or
# ConvertFrom-Json -AsHashtable. A payload that uses one parses here and fails on the room, so the test looks for them.
function Find-Ps7Only {
    param([string] $Text)
    $found = @()
    $Text = [regex]::Replace($Text, "(?s)'(?:[^']|'')*'", "''")
    if ($Text -match '\?\.|\?\?') { $found += '?. or ??' }
    if ($Text -match '\s\?\s+[^\s{]') { $found += 'a ternary' }
    if ($Text -match '(?<![|&])(&&|\|\|)(?![|&])') { $found += '&& or ||' }
    if (@($Text -split "`n" | Where-Object { $_ -match 'Join-Path' -and $_ -notmatch '^function J ' }).Count) { $found += 'Join-Path outside J' }
    if ($Text -match '-AsHashtable') { $found += '-AsHashtable' }
    if ($Text -match '\$IsWindows|\$IsMacOS|\$IsLinux') { $found += '$IsWindows and friends' }
    $found
}

# ── the Unix act ────────────────────────────────────────────────────────────

# The C profile on macOS and Linux: report only. cc and gcc are both asked for, because either builds the SDK.
$script:CUnixAct = @'
cprobe)
  for t in cc gcc clang cmake ninja git; do
    p=$(command -v "$t" 2>/dev/null) || p=; v=
    [ -n "$p" ] && v=$("$p" --version 2>&1 | head -1)
    echo "$t=$p|$v"
  done
  [ -n "$VCPKGDIR" ] || VCPKGDIR="$HOME/vcpkg"
  [ -n "$SDKDIR" ] || SDKDIR="$HOME/git/github/openziti/ziti-sdk-c"
  echo "vcpkg.dir=$VCPKGDIR"; if [ -d "$VCPKGDIR" ]; then echo vcpkg.here=True; else echo vcpkg.here=False; fi
  echo "sdk.dir=$SDKDIR"; if [ -d "$SDKDIR" ]; then echo sdk.here=True; else echo sdk.here=False; fi;;
'@

# The variables each act reads, with values as long as a real call can make them, for the payload size test. Json is the real
# preset text for long paths. The values differ from each other so gzip cannot flatter the size.
function Get-CWorstVars {
    param([string] $Act)
    $def = $script:CActs[$Act]
    $names = $def.Vars
    $long = { param($n) $r = [Random]::new([int](($n.ToCharArray() | ForEach-Object { [int]$_ } | Measure-Object -Sum).Sum)); 'C:\Users\a-long-user-name\' + ((1..4 | ForEach-Object { '{0:x8}' -f $r.Next() }) -join '\') + '\' + (-join (1..80 | ForEach-Object { 'abcdefghijklmnopqrstuvwxyz0123456789'[$r.Next(36)] })) }
    $v = @{}
    foreach ($n in $names) {
        $v[$n] = switch ($n) {
            'PathPre' { (1..4 | ForEach-Object { & $long "p$_" }) -join ';' }
            'Rec' { (1..4 | ForEach-Object { & $long "r$_" }) -join ';' }
            'Sha' { ('0123456789abcdef' * 4) }
            'Force' { '1' } 'Init' { '1' } 'Dry' { '0' } 'Mode' { 'remove' } 'Before' { '(OI)(CI)(RX) (CI)(IO)(W)' }
            'Key' { (& $long 'k') + '|DOMAIN\someone-with-a-long-name' }
            'Pkgs' { $script:CPackages -join ' ' }
            'Accts' { 'claude,localai' }
            'Ops' { (1..3 | ForEach-Object { (& $long "o$_") + '|/grant|' + 'DOMAIN\someone-with-a-long-name:(OI)(CI)RX' }) -join "`n" }
            'Name' { 'A Person With A Long Name' } 'Email' { 'a.person.with.a.long.name@example.com' } 'Helper' { 'manager' }
            'Repos' { "https://github.com/openziti/ziti-sdk-c.git`nhttps://github.com/dovholuknf/some-private-repository-name.git" }
            'Json' { ConvertTo-PresetsJson (Get-CwdmingPresets (& $long 'v') (& $long 'm') (& $long 'c')) }
            default { & $long $n }
        }
    }
    $v
}
