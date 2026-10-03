# Tests for the C profile of scripts/room-toolchain.ps1 (scripts/room-toolchain-c.ps1). Needs no ssh, no Windows and no
# network:
#
#   pwsh -NoProfile -File scripts/test-room-toolchain-c.ps1
#
# What runs here: the pure functions (quoting, the preset content and its merge, the MSYS2 release choice and discovery,
# icacls parsing and plans, exit codes, the needs-human summary), every Windows payload built and parsed and measured, the
# acts that need no Windows API run under pwsh against FAKE directories (a fake MSYS2 with scripts for gcc and cmake, a fake
# vcpkg and checkout), and `local -Check -Profile c` on this machine for the Unix report.
# What it cannot run: Windows PowerShell 5.1 itself, icacls, the MSYS2 first start and pacman, Git Credential Manager,
# bootstrap-vcpkg.bat, ssh, or a cmake configure. The payloads are parsed by the PowerShell 7 parser and scanned for syntax
# that 5.1 lacks, which is not the same as running them on 5.1.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'room-toolchain-c.ps1')

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
$pwsh = (Get-Command pwsh).Source
$unix = $IsWindows -ne $true
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("room-toolchain-c-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force $tmp | Out-Null

try {

# ── quoting: values with hostile characters arrive whole and run nothing ────

$q1 = [string][char]0x2018; $q2 = [string][char]0x2019
$weird = @("C:\work\plain", "C:\my work\it's", 'C:\a b\c', "C:\x${q2}; Write-Output INJECTED; ${q2}", "C:\${q1}x${q2}", 'C:\$env:USERNAME', 'C:\`tick', 'C:\a;b&c|d', "C:\a`nb")
foreach ($w in $weird) {
    $got = & $pwsh -NoProfile -Command ("`$v = " + (Quote-Ps $w) + '; [Console]::Out.Write($v)')
    Check "quote-ps: $($w -replace "`n", '<nl>')" ($got -join "`n") $w
}
# the whole head of a payload: every value comes out as it went in, and nothing ran
$vars = @{ PathPre = $weird[3]; SdkDir = $weird[1]; Dry = '1' }
$head = (Get-CPayload 'csdk' $vars) -split "`n" | Select-Object -First 3
$ps1 = Join-Path $tmp 'head.ps1'
Set-Content -LiteralPath $ps1 -Value ($head + '[Console]::Out.Write("$PathPre|$SdkDir|$Dry")')
Check 'payload head: hostile values arrive whole, nothing runs' ((& $pwsh -NoProfile -File $ps1) -join '') "$($weird[3])|$($weird[1])|1"
Check 'quote-sh: a quote' (Quote-Sh "it's") "'it'\''s'"

# ── the arguments ───────────────────────────────────────────────────────────

Check 'path arg: drive path ok' (Test-WinPathArg 'V:\work\tools\msys64' '-Msys2Dir') $null
Check 'path arg: forward slashes ok' (Test-WinPathArg 'C:/work/msys64' '-Msys2Dir') $null
Check 'path arg: space and apostrophe ok' (Test-WinPathArg "C:\my work\it's" '-Msys2Dir') $null
Check 'path arg: relative refused' ([bool](Test-WinPathArg 'msys64' '-Msys2Dir')) $true
Check 'path arg: unix path refused' ([bool](Test-WinPathArg '/opt/msys64' '-Msys2Dir')) $true
Check 'path arg: UNC refused' ([bool](Test-WinPathArg '\\host\share' '-Msys2Dir')) $true
Check 'path arg: a double quote refused' ([bool](Test-WinPathArg 'C:\a"b' '-Msys2Dir')) $true
Check 'path arg: a pipe refused' ([bool](Test-WinPathArg 'C:\a|b' '-Msys2Dir')) $true
Check 'path arg: a newline refused' ([bool](Test-WinPathArg "C:\a`nb" '-Msys2Dir')) $true
Check 'path arg: a drive root refused' ([bool](Test-WinPathArg 'C:\' '-Msys2Dir')) $true
Check 'account: plain' (Test-AccountArg 'claude') $null
Check 'account: domain' (Test-AccountArg 'SG3\localai') $null
Check 'account: a space and a pipe refused' (@((Test-AccountArg 'a b'), (Test-AccountArg 'a|b')) | ForEach-Object { [bool]$_ }) @($true, $true)
Check 'identity: name ok' (Test-GitIdentityArg 'Clint D' '-GitUserName') $null
Check 'identity: a quote refused' ([bool](Test-GitIdentityArg 'Cl"int' '-GitUserName')) $true
Check 'identity: a dollar refused' ([bool](Test-GitIdentityArg 'a$b' '-GitUserName')) $true
Check 'identity: email ok' (Test-GitIdentityArg 'c@example.com' '-GitUserEmail') $null
Check 'identity: not an email refused' ([bool](Test-GitIdentityArg 'cexample.com' '-GitUserEmail')) $true
Check 'repo: owner/repo' (ConvertTo-RepoUrl 'dovholuknf/atrium') 'https://github.com/dovholuknf/atrium.git'
Check 'repo: https url' (ConvertTo-RepoUrl 'https://github.com/openziti/ziti-sdk-c.git') 'https://github.com/openziti/ziti-sdk-c.git'
Check 'repo: a token in the url is refused' ([bool](ConvertTo-RepoUrl 'https://user:tok3n@github.com/o/r.git')) $false
Check 'repo: http is refused' ([bool](ConvertTo-RepoUrl 'http://github.com/o/r')) $false
Check 'repo: a space is refused' ([bool](ConvertTo-RepoUrl 'o/r x')) $false

# ── versions ────────────────────────────────────────────────────────────────

Check 'version: cmake' (Get-ToolVersion 'cmake version 4.1.1') '4.1.1'
Check 'version: gcc' (Get-ToolVersion 'gcc.exe (Rev8, Built by MSYS2 project) 15.2.0') '15.2.0'
Check 'version: ninja' (Get-ToolVersion '1.13.1') '1.13.1'
Check 'version: pkgconf' (Get-ToolVersion 'pkgconf 3.0.7') '3.0.7'
Check 'version: two parts' (Get-ToolVersion 'ldd 2.41') '2.41'
Check 'version: none' (Get-ToolVersion 'error: nope') ''
Check 'at least: newer' (Test-VersionAtLeast '4.1.1' '3.23') $true
Check 'at least: equal' (Test-VersionAtLeast '3.23' '3.23') $true
Check 'at least: older' (Test-VersionAtLeast '3.22.9' '3.23') $false
Check 'at least: ten is more than nine' (Test-VersionAtLeast '3.100' '3.23') $true
Check 'at least: nothing' (Test-VersionAtLeast '' '3.23') $false

# ── the presets ─────────────────────────────────────────────────────────────

$pre = @(Get-CwdmingPresets 'C:\Users\claude\vcpkg' 'V:\work\tools\msys64' '')
Check 'presets: names in file order' ($pre | ForEach-Object { $_.name }) @('mingw-vcpkg-base', 'cwdming', 'cwdming-with-tests')
Check 'presets: the base is hidden' $pre[0].hidden $true
Check 'presets: the base inherits upstream ci-windows-x64-mingw' $pre[0].inherits @('ci-windows-x64-mingw')
Check 'presets: host triplet is mingw' $pre[0].cacheVariables.VCPKG_HOST_TRIPLET 'x64-mingw-static'
Check 'presets: target triplet is mingw' $pre[0].cacheVariables.VCPKG_TARGET_TRIPLET 'x64-mingw-static'
Check 'presets: Ninja' $pre[0].generator 'Ninja'
Check 'presets: toolchain file from VCPKG_ROOT' $pre[0].cacheVariables.CMAKE_TOOLCHAIN_FILE '$env{VCPKG_ROOT}/scripts/buildsystems/vcpkg.cmake'
Check 'presets: gcc and g++' @($pre[0].cacheVariables.CMAKE_C_COMPILER, $pre[0].cacheVariables.CMAKE_CXX_COMPILER) @('gcc', 'g++')
Check 'presets: pkg-config from the discovered MSYS2' $pre[0].cacheVariables.PKG_CONFIG_EXECUTABLE 'V:/work/tools/msys64/mingw64/bin/pkg-config.exe'
Check 'presets: openssl root from the discovered MSYS2' $pre[0].environment.OPENSSL_ROOT_DIR 'V:/work/tools/msys64/mingw64'
Check 'presets: VCPKG_ROOT from -VcpkgDir' $pre[0].environment.VCPKG_ROOT 'C:/Users/claude/vcpkg'
Check 'presets: mingw64\bin first on PATH' $pre[0].environment.PATH 'V:/work/tools/msys64/mingw64/bin;$penv{PATH}'
Check 'presets: Debug' $pre[0].cacheVariables.CMAKE_BUILD_TYPE 'Debug'
Check 'presets: no binary cache by default' $pre[0].environment.Contains('VCPKG_BINARY_SOURCES') $false
Check 'presets: cwdming' @($pre[1].inherits, $pre[1].binaryDir) @('mingw-vcpkg-base', '${sourceDir}/build/cwdming')
Check 'presets: with-tests' @($pre[2].inherits, $pre[2].cacheVariables.ziti_DEVELOPER_MODE, $pre[2].cacheVariables.VCPKG_MANIFEST_FEATURES) @('cwdming', 'ON', 'dev-features')
Check 'presets: a binary cache goes to VCPKG_BINARY_SOURCES' (Get-CwdmingPresets 'C:\v' 'C:\m' 'D:\cache dir')[0].environment.VCPKG_BINARY_SOURCES 'clear;files,D:/cache dir,readwrite'
Check 'presets: msys2 dir with a trailing slash' (Get-CwdmingPresets 'C:\v' 'C:\m\' '')[0].environment.OPENSSL_ROOT_DIR 'C:/m/mingw64'
$json = ConvertTo-PresetsJson $pre
Check 'presets: json round trips' (@((ConvertFrom-Json $json).configurePresets).Count) 3
Check 'presets: json keeps $penv and ${sourceDir}' (($json -match '\$penv\{PATH\}') -and ($json -match '\$\{sourceDir\}/build/cwdming')) $true
$oddPre = ConvertTo-PresetsJson (Get-CwdmingPresets "C:\it's here\vcpkg" "D:\a b\msys64" '')
Check 'presets: odd paths survive' ((ConvertFrom-Json $oddPre).configurePresets[0].environment.VCPKG_ROOT) "C:/it's here/vcpkg"

# ── the merge, the SAME text the room runs ──────────────────────────────────

Invoke-Expression $script:CPresetMerge
$m0 = Merge-Presets '' $json
Check 'merge: no file is created' $m0.Action 'created'
Check 'merge: created has all three, version 4' @((ConvertFrom-Json $m0.Text).version, @((ConvertFrom-Json $m0.Text).configurePresets).Count) @(4, 3)
Check 'merge: blank file is created' (Merge-Presets "  `n" $json).Action 'created'
$m1 = Merge-Presets $m0.Text $json
Check 'merge: again is ok and changes nothing' @($m1.Action, $m1.Text) @('ok', '')
Check 'merge: ok says what it kept' $m1.Kept @('mingw-vcpkg-base', 'cwdming', 'cwdming-with-tests')

$user = '{"version":6,"include":["D:/worktrees/CMakeUserPreset.json"],"configurePresets":[{"name":"mine","inherits":"cwdming"},{"name":"cwdming","binaryDir":"X:/mine"}],"buildPresets":[{"name":"b","configurePreset":"mine"}]}'
$m2 = Merge-Presets $user $json
$o2 = ConvertFrom-Json $m2.Text
Check 'merge: a file without them is merged' $m2.Action 'merged'
Check 'merge: adds only the missing ones' $m2.Added @('mingw-vcpkg-base', 'cwdming-with-tests')
Check 'merge: a differing cwdming is kept as it is' (@($o2.configurePresets | Where-Object { $_.name -eq 'cwdming' })[0].binaryDir) 'X:/mine'
Check 'merge: and reported as kept' $m2.Kept @('cwdming')
Check 'merge: the include is kept' $o2.include @('D:/worktrees/CMakeUserPreset.json')
Check 'merge: version and buildPresets are kept' @($o2.version, $o2.buildPresets[0].name) @(6, 'b')
Check 'merge: the user preset is still first' $o2.configurePresets[0].name 'mine'
Check 'merge: the result merges to ok' (Merge-Presets $m2.Text $json).Action 'ok'
Check 'merge: no configurePresets at all gets the list' (Merge-Presets '{"version":4}' $json).Action 'merged'
Check 'merge: configurePresets null gets the list' @((ConvertFrom-Json (Merge-Presets '{"version":4,"configurePresets":null}' $json).Text).configurePresets).Count 3
Check 'merge: an empty list gets the list' @((ConvertFrom-Json (Merge-Presets '{"version":4,"configurePresets":[]}' $json).Text).configurePresets).Count 3
Check 'merge: one preset is still a list' @((ConvertFrom-Json (Merge-Presets '{"version":4,"configurePresets":[{"name":"a"}]}' $json).Text).configurePresets).Count 4

# hostile existing files are left alone and say why
$hostile = [ordered]@{
    'not json'               = 'this is not json {'
    'truncated'              = '{"version":4,"configurePresets":[{"name":"a"'
    'a list at the top'      = '[1,2,3]'
    'a string at the top'    = '"hello"'
    'a number at the top'    = '42'
    'null at the top'        = 'null'
    'no version'             = '{"configurePresets":[]}'
    'version 1'              = '{"version":1,"configurePresets":[]}'
    'version a word'         = '{"version":"four","configurePresets":[]}'
    'presets an object'      = '{"version":4,"configurePresets":{"name":"a"}}'
    'presets a string'       = '{"version":4,"configurePresets":"cwdming"}'
    'a binary-ish file'      = ([string][char]0 + [string][char]1 + 'abc')
}
foreach ($k in $hostile.Keys) {
    $r = Merge-Presets $hostile[$k] $json
    Check "merge hostile: $k is left alone" @($r.Action, $r.Text) @('invalid', '')
    Check "merge hostile: $k says why" ([bool]$r.Why) $true
}
# a // comment: Windows PowerShell 5.1 refuses it (invalid), PowerShell 7 reads it, and CMake does not take comments either
Check 'merge: a file with a comment is invalid or merged, never lost' ((Merge-Presets "{`n// nope`n`"version`":4}" $json).Action -in 'invalid', 'merged') $true
# strange but valid files are not damaged
$strange = '{"version":4,"configurePresets":["a string",42,null,{"name":"cwdming"},{"noname":true}],"extra":{"deep":[1,[2,[3]]]},"unicode":"caf\u00e9 \u2018q\u2019"}'
$m3 = Merge-Presets $strange $json
Check 'merge strange: merged' $m3.Action 'merged'
$o3 = ConvertFrom-Json $m3.Text
Check 'merge strange: the odd entries are kept' @($o3.configurePresets[0], $o3.configurePresets[1], $o3.configurePresets[4].noname) @('a string', 42, $true)
Check 'merge strange: the rest is kept' @($o3.extra.deep[1][1][0], $o3.unicode) @(3, "caf$([char]0xe9) ${q1}q${q2}")
Check 'merge strange: cwdming (an entry with only a name) is kept' $m3.Kept @('cwdming')
$deep = ('{"a":' * 150) + '1' + ('}' * 150)
Check 'merge hostile: absurd nesting never writes a file that loses it' ((Merge-Presets ('{"version":4,"configurePresets":[],"x":' + $deep + '}') $json).Action -in 'invalid', 'merged') $true

# ── MSYS2: which archive ────────────────────────────────────────────────────

function New-Rel { param([string] $tag, [string[]] $names, [bool] $pre = $false, [bool] $draft = $false)
    [pscustomobject]@{ tag_name = $tag; prerelease = $pre; draft = $draft
        assets = @($names | ForEach-Object { [pscustomobject]@{ name = $_; browser_download_url = "https://github.com/msys2/msys2-installer/releases/download/$tag/$_"; digest = 'sha256:' + ('a' * 64) } }) }
}
$all = @('msys2-base-x86_64-{0}.sfx.exe', 'msys2-base-x86_64-{0}.sfx.exe.sha256', 'msys2-base-x86_64-{0}.tar.zst')
$rels = @(
    (New-Rel 'nightly' ($all | ForEach-Object { $_ -f '20261001' }) $true),
    (New-Rel '2026-09-27' ($all | ForEach-Object { $_ -f '20260927' })),
    (New-Rel '2026-09-30' @('msys2-base-x86_64-20260930.sfx.exe')),
    (New-Rel '2026-06-11' ($all | ForEach-Object { $_ -f '20260611' })),
    (New-Rel 'draft' ($all | ForEach-Object { $_ -f '20261005' }) $false $true)
)
$sel = Select-Msys2Release $rels ''
Check 'msys2 release: the newest with a sha256, not a prerelease, draft or one without' $sel.Date '20260927'
Check 'msys2 release: the file comes from repo.msys2.org' $sel.Url 'https://repo.msys2.org/distrib/x86_64/msys2-base-x86_64-20260927.sfx.exe'
Check 'msys2 release: the hash is the one next to it on the release' $sel.ShaUrl 'https://github.com/msys2/msys2-installer/releases/download/2026-09-27/msys2-base-x86_64-20260927.sfx.exe.sha256'
Check 'msys2 release: pin a date' (Select-Msys2Release $rels '20260611').Date '20260611'
Check 'msys2 release: a date that is not there' ([bool](Select-Msys2Release $rels '20200101')) $false
Check 'msys2 release: none at all' ([bool](Select-Msys2Release @() '')) $false
$h = 'ad336cccfda47758b5e15cda993fbba421115cb0b126697daef1ee4dfe37209f'
Check 'sha256 file: hash and name' (Read-Sha256File "$h  msys2-base-x86_64-20260927.sfx.exe`n" 'msys2-base-x86_64-20260927.sfx.exe') $h
Check 'sha256 file: star before the name' (Read-Sha256File "$h *msys2-base-x86_64-20260927.sfx.exe" 'msys2-base-x86_64-20260927.sfx.exe') $h
Check 'sha256 file: a bare hash' (Read-Sha256File $h 'x') $h
Check 'sha256 file: upper case is lowered' (Read-Sha256File $h.ToUpper() 'x') $h
Check 'sha256 file: another file is not this one' ([bool](Read-Sha256File "$h  other.exe" 'x.exe')) $false
Check 'sha256 file: short hash' ([bool](Read-Sha256File 'abc123  x.exe' 'x.exe')) $false
Check 'sha256 file: html is no hash' ([bool](Read-Sha256File '<html>404</html>' 'x.exe')) $false

# ── MSYS2: where it is, over fake directories ───────────────────────────────

Invoke-Expression ($script:CCommon + "`n" + $script:CFind)
function New-FakeMsys {
    param([string] $dir, [string[]] $bins = @('gcc', 'g++', 'cmake', 'ninja', 'pkg-config'), [bool] $ssl = $true, [string] $tag = '9.9.9')
    foreach ($d in 'usr/bin', 'mingw64/bin', 'mingw64/lib', 'mingw64/include/openssl', 'var/lib/pacman/local') { New-Item -ItemType Directory -Force (Join-Path $dir $d) | Out-Null }
    Set-Content -LiteralPath (Join-Path $dir 'usr/bin/pacman.exe') -Value 'x'
    Set-Content -LiteralPath (Join-Path $dir 'var/lib/pacman/local/ALPM_DB_VERSION') -Value '9'
    foreach ($b in $bins) {
        $f = Join-Path $dir "mingw64/bin/$b.exe"
        Set-Content -LiteralPath $f -Value "#!/bin/sh`necho '$b version $tag'`n"
        if ($unix) { & chmod +x $f }
    }
    if ($ssl) { Set-Content -LiteralPath (Join-Path $dir 'mingw64/lib/libssl.a') -Value x; Set-Content -LiteralPath (Join-Path $dir 'mingw64/include/openssl/ssl.h') -Value x }
}
$fm1 = Join-Path $tmp 'a/msys64'; $fm2 = Join-Path $tmp 'prefix/msys64'; $fm3 = Join-Path $tmp 'elsewhere/msys64'
New-FakeMsys $fm1; New-FakeMsys $fm2; New-FakeMsys $fm3
Check 'find: an explicit dir is the answer' (Msys2Find $fm1 (Join-Path $tmp 'prefix') @()) $fm1
Check 'find: an explicit dir that is not there is not replaced by another' ([bool](Msys2Find (Join-Path $tmp 'nope') (Join-Path $tmp 'prefix') @())) $false
Check 'find: candidates for an explicit dir are only it' (Msys2Cands 'D:\m' '/p' @('X:\y\mingw64\bin')) @('D:\m')
Check 'find: without one, C:\msys64, then the prefix, then the record' (Msys2Cands '' '/p' @('X:\y\mingw64\bin', 'D:\go\bin')) @('C:\msys64', '/p/msys64', 'X:\y')
Check 'find: the prefix one is found' (Msys2Find '' (Join-Path $tmp 'prefix') @()) $fm2
Check 'find: one named by the record is found' (Msys2Find '' (Join-Path $tmp 'none') @((Join-Path $fm3 'mingw64/bin'))) $fm3
Check 'find: the record with a forward slash' (Msys2Cands '' '/p' @('X:/y/mingw64/bin'))[2] 'X:/y'
Check 'find: a dir without pacman.exe is not MSYS2' ([bool](Msys2Find (Join-Path $tmp 'a') '' @())) $false
Check 'find: nothing anywhere' ([bool](Msys2Find '' (Join-Path $tmp 'none') @())) $false

# ── icacls ──────────────────────────────────────────────────────────────────

$dirW = 'V:\work\tools\msys64'
$ic = @(
    "$dirW BUILTIN\Administrators:(OI)(CI)(F)"
    '                     NT AUTHORITY\SYSTEM:(OI)(CI)(F)'
    '                     SG3\claude:(OI)(CI)(M)'
    '                     BUILTIN\Users:(OI)(CI)(RX)'
    '                     NT AUTHORITY\Authenticated Users:(OI)(CI)(IO)(M)'
    '                     SG3\old:(I)(OI)(CI)(R)'
    '                     SG3\denied:(DENY)(OI)(CI)(R)'
    ''
    'Successfully processed 1 files; Failed processing 0 files'
)
$aces = @(ConvertFrom-Icacls $ic $dirW)
Check 'icacls: lines read, noise skipped' $aces.Count 7
Check 'icacls: the first line carries the dir' @($aces[0].Account, $aces[0].Rights) @('BUILTIN\Administrators', 'F')
Check 'icacls: an account with a space' $aces[4].Account 'NT AUTHORITY\Authenticated Users'
Check 'icacls: inherit-only is marked' $aces[4].InheritOnly $true
Check 'icacls: inherited is marked' $aces[5].Inherited $true
Check 'icacls: deny is marked' $aces[6].Deny $true
Check 'icacls: raw keeps the flags' $aces[2].Raw '(OI)(CI)(M)'
Check 'readable: through Users' (Test-AclReadable $aces 'SG3\localai') $true
Check 'readable: by name, any domain' (Test-AclReadable $aces 'claude') $true
$noUsers = @($aces | Where-Object { $_.Account -notmatch 'Users$' })
Check 'readable: not without Users' (Test-AclReadable $noUsers 'SG3\localai') $false
Check 'readable: a deny is not a grant' (Test-AclReadable $aces 'denied') $false
Check 'readable: inherit-only does not count' (Test-AclReadable @($aces[4]) 'someone') $false
Check 'icacls args: grant' (New-IcaclsArgs $dirW 'SG3\localai' 'grant' 'RX') @($dirW, '/grant', 'SG3\localai:(OI)(CI)RX')
Check 'icacls args: modify' (New-IcaclsArgs $dirW 'claude' 'grant' 'M') @($dirW, '/grant', 'claude:(OI)(CI)M')
Check 'icacls args: remove' (New-IcaclsArgs $dirW 'claude' 'remove') @($dirW, '/remove:g', 'claude')
Check 'icacls args: restore' (New-IcaclsArgs $dirW 'claude' 'restore' '(OI)(CI)(RX)') @($dirW, '/grant:r', 'claude:(OI)(CI)(RX)')
foreach ($bad in 'Everyone', 'BUILTIN\Users', 'Users', 'Authenticated Users', 'NT AUTHORITY\Authenticated Users') {
    $threw = $false; try { New-IcaclsArgs $dirW $bad 'grant' 'RX' | Out-Null } catch { $threw = $true }
    Check "icacls args: a grant to $bad is refused" $threw $true
}
Check 'admin command: quotes what has a space, and an ACE' (Format-AdminCommand @('icacls', 'V:\my tools\msys64', '/grant', 'SG3\localai:(OI)(CI)RX')) "icacls 'V:\my tools\msys64' /grant 'SG3\localai:(OI)(CI)RX'"
Check 'admin command: and an apostrophe' (Format-AdminCommand @('icacls', "V:\it's\m", '/remove:g', 'a')) "icacls 'V:\it''s\m' /remove:g a"
# plans
$p1 = New-AclPlan $dirW $aces 'SG3\claude' $true @('SG3\localai') $true
Check 'plan: Users can read already, so no RX for localai and nothing to take back' @($p1.Grant.Count, $p1.Revert.Count, $p1.Modify) @(0, 0, $false)
$p2 = New-AclPlan $dirW $noUsers 'SG3\claude' $true @('SG3\localai') $true
Check 'plan: localai cannot read, so RX' @($p2.Grant.Count, $p2.Rx, $p2.Grant[0].Args[2]) @(1, 'SG3\localai', 'SG3\localai:(OI)(CI)RX')
$p3 = New-AclPlan $dirW @() 'SG3\claude' $false @() $true
Check 'plan: cannot write and pacman runs: Modify, then taken back' @($p3.Grant.Count, $p3.Grant[0].Args[2], $p3.Revert.Count, $p3.Revert[0].Args[1], $p3.Modify) @(1, 'SG3\claude:(OI)(CI)M', 1, '/remove:g', $true)
$p4 = New-AclPlan $dirW @() 'SG3\claude' $false @() $false
Check 'plan: cannot write but nothing to install: no Modify' @($p4.Grant.Count, $p4.Modify) @(0, $false)
$p5 = New-AclPlan $dirW @([pscustomobject]@{ Account = 'SG3\claude'; Rights = @('RX'); Inherited = $false; InheritOnly = $false; Deny = $false; Raw = '(OI)(CI)(RX)' }) 'SG3\claude' $false @() $true
Check 'plan: what the user had is put back after' @($p5.Revert.Count, $p5.Revert[1].Args[1], $p5.Revert[1].Args[2]) @(2, '/grant:r', 'SG3\claude:(OI)(CI)(RX)')
$p6 = New-AclPlan $dirW @([pscustomobject]@{ Account = 'SG3\claude'; Rights = @('R'); Inherited = $true; InheritOnly = $false; Deny = $false; Raw = '(I)(R)' }) 'SG3\claude' $false @() $true
Check 'plan: an inherited ace is not put back, it comes back by itself' $p6.Revert.Count 1
Check 'plan: ops text, one per line, args joined by a pipe' (ConvertTo-AclOps $p3.Grant) "$dirW|/grant|SG3\claude:(OI)(CI)M"
Check 'plan: ops text for two' ((ConvertTo-AclOps @($p2.Grant + $p3.Grant)) -split "`n").Count 2

# ── exit codes and the summary ──────────────────────────────────────────────

Check 'exit: clean' (Get-CExitCode 0 0) 0
Check 'exit: needs a human' (Get-CExitCode 0 2) 6
Check 'exit: a failure wins over a human' (Get-CExitCode 3 2) 3
Check 'exit: a sha256 mismatch wins' (Get-CExitCode 4 1) 4
Check 'exit: path record failure wins' (Get-CExitCode 5 1) 5
Check 'exit: a local problem wins' (Get-CExitCode 1 1) 1
Check 'summary: one line per command, prefixed' (Format-NeedsHuman @('git credential-manager github login', 'icacls x')) @('room-toolchain needs-human git credential-manager github login', 'room-toolchain needs-human icacls x')
Check 'summary: no duplicates, no blanks' @(Format-NeedsHuman @('a', '', 'a', 'b')).Count 2
Check 'summary: none' @(Format-NeedsHuman @()).Count 0

# ── the payloads: built, parsed, measured ───────────────────────────────────

foreach ($a in ($script:CActs.Keys | Sort-Object)) {
    $text = Get-CPayload $a (Get-CWorstVars $a)
    $errs = $null
    [System.Management.Automation.Language.Parser]::ParseInput($text, [ref]$null, [ref]$errs) | Out-Null
    Check "payload $a parses" @($errs).Count 0
    Check "payload $a has nothing 5.1 lacks" @(Find-Ps7Only $text).Count 0
    $len = (New-EncodedCommand $text).Length
    Check "payload $a is under the cmd.exe limit with room to spare ($len of $script:EncodedLimit)" ($len -le 7400) $true
}
Check 'payload: a named variable not in the act is not injected' ((Get-CPayload 'cgit' @{ PathPre = 'x' }) -notmatch 'SdkDir') $true
$threw = $false; try { Get-CPayload 'nope' @{} | Out-Null } catch { $threw = $true }
Check 'payload: an unknown act is refused' $threw $true
# the existing payloads, by the script's own hook: none got longer than the cmd.exe limit
$sizes = & $pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-toolchain.ps1') x -PayloadSizes -Prefix 'C:\Users\a-long-user-name\abcdef0123abcdef0123abcdef0123abcdef0123abcdef0123abcdef0123abcdef0123abcdef0123'
foreach ($l in $sizes) {
    if ($l -match '^payload (\w+) (\d+)$') { Check "script hook: $($Matches[1]) is $($Matches[2]), under 7800" ([int]$Matches[2] -le 7800) $true }
}
Check 'script hook: every act is listed (4 old + C)' @($sizes | Where-Object { $_ -match '^payload ' }).Count (4 + $script:CActs.Count)
# the check itself: a payload that grows over the limit is caught
$big = (Get-CPayload 'cgit' @{ PathPre = 'x' }) + "`n# " + ((1..400 | ForEach-Object { [guid]::NewGuid().ToString('N') }) -join '')
Check 'limit: a payload that is too long is over it' ((New-EncodedCommand $big).Length -gt $script:EncodedLimit) $true
Check 'lint: a ?? is found' @(Find-Ps7Only '$a = $b ?? 1').Count 1
Check 'lint: a ternary is found' @(Find-Ps7Only '$a = $x ? 1 : 2').Count 1
Check 'lint: && is found' @(Find-Ps7Only 'a && b').Count 1
Check 'lint: a Join-Path that is not J is found' @(Find-Ps7Only 'Join-Path $a $b $c').Count 1
Check 'lint: an alias question mark and a quoted ?? are fine' @(Find-Ps7Only "`$x | ? { `$_ }; 'a ?? b'").Count 0

# ── the acts that need no Windows API, run for real against fake directories

function Invoke-Act {
    param([string] $act, [hashtable] $vars)
    $f = Join-Path $tmp "$act-$([guid]::NewGuid().ToString('N').Substring(0, 6)).ps1"
    Set-Content -LiteralPath $f -Value (Get-CPayload $act $vars)
    $out = & $pwsh -NoProfile -File $f 2>&1
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE; Kv = (& { $h = @{}; foreach ($l in $out) { $i = "$l".IndexOf('='); if ($i -gt 0) { $h["$l".Substring(0, $i)] = "$l".Substring($i + 1) } }; $h }) }
}
$env:TEMP = $tmp
$sdk = Join-Path $tmp 'git/github/openziti/ziti-sdk-c'
$vcp = Join-Path $tmp 'vcpkg'

# cmsys: found, with all tools
$r = Invoke-Act 'cmsys' @{ PathPre = ''; Rec = ''; Msys2Dir = $fm1; Prefix = (Join-Path $tmp 'prefix'); Accts = '' }
Check 'act cmsys: found' @($r.Kv['msys2.found'], $r.Kv['msys2.dir']) @('True', $fm1)
Check 'act cmsys: a tool is its path and its version' $r.Kv['bin.gcc'] "$fm1/mingw64/bin/gcc.exe|gcc version 9.9.9"
Check 'act cmsys: openssl is there' $r.Kv['ssl'] 'True'
Check 'act cmsys: pkg-config is there' ($r.Kv['bin.pkg-config'] -like '*pkg-config.exe|pkg-config version 9.9.9') $true
Check 'act cacls: writable' (Invoke-Act 'cacls' @{ Msys2Dir = $fm1; Accts = ''; StateDir = '' }).Kv['writable'] 'True'
Check 'act cmsys: it wrote nothing' @(Get-ChildItem -LiteralPath $fm1 -Recurse -Force -File | Where-Object { $_.LastWriteTime -gt (Get-Date).AddSeconds(-600) -and $_.Name -notin 'pacman.exe', 'ALPM_DB_VERSION', 'libssl.a', 'ssl.h' -and $_.Extension -ne '.exe' }).Count 0
# partly there: gcc only
$fmp = Join-Path $tmp 'partial/msys64'; New-FakeMsys $fmp @('gcc') $false
$r = Invoke-Act 'cmsys' @{ PathPre = ''; Rec = ''; Msys2Dir = $fmp; Prefix = $tmp; Accts = '' }
Check 'act cmsys: a partial MSYS2 reports MISSING (a bare |) for the rest' @($r.Kv['bin.cmake'], $r.Kv['bin.ninja'], $r.Kv['ssl']) @('|', '|', 'False')
# not there
$r = Invoke-Act 'cmsys' @{ PathPre = ''; Rec = ''; Msys2Dir = ''; Prefix = (Join-Path $tmp 'empty'); Accts = '' }
Check 'act cmsys: absent says where it would go' @($r.Kv['msys2.found'], $r.Kv['msys2.dir']) @('False', (Join-Path (Join-Path $tmp 'empty') 'msys64'))
# found through the PATH record
$r = Invoke-Act 'cmsys' @{ PathPre = ''; Rec = "$fm3/mingw64/bin"; Msys2Dir = ''; Prefix = (Join-Path $tmp 'empty'); Accts = '' }
Check 'act cmsys: found through the record' @($r.Kv['msys2.found'], $r.Kv['msys2.dir']) @('True', $fm3)
# the gaps, as the flow reads them
Check 'gaps: none' @(Get-CGaps (Invoke-Act 'cmsys' @{ PathPre = ''; Rec = ''; Msys2Dir = $fm1; Prefix = $tmp; Accts = '' }).Kv).Count 0
Check 'gaps: the missing ones, by name' (Get-CGaps (Invoke-Act 'cmsys' @{ PathPre = ''; Rec = ''; Msys2Dir = $fmp; Prefix = $tmp; Accts = '' }).Kv) @('g++', 'cmake', 'ninja', 'pkg-config', 'openssl')

# cvcpkg and csdk, reporting only
$r = Invoke-Act 'cvcpkg' @{ PathPre = ''; VcpkgDir = $vcp; Url = 'https://example.invalid/vcpkg'; Dry = '1' }
Check 'act cvcpkg dry: absent' @($r.Kv['vcpkg.dirhere'], $r.Kv['vcpkg.git'], $r.Kv['vcpkg.inway'], $r.Kv['vcpkg.triplet']) @('False', 'False', 'False', 'False')
Check 'act cvcpkg dry: wrote nothing' (Test-Path $vcp) $false
New-Item -ItemType Directory -Force (Join-Path $vcp '.git'), (Join-Path $vcp 'triplets/community') | Out-Null
Set-Content -LiteralPath (Join-Path $vcp 'triplets/community/x64-mingw-static.cmake') -Value x
Set-Content -LiteralPath (Join-Path $vcp 'vcpkg.exe') -Value "#!/bin/sh`necho 'vcpkg package management program version 2026-09-01'`n"; if ($unix) { & chmod +x (Join-Path $vcp 'vcpkg.exe') }
$r = Invoke-Act 'cvcpkg' @{ PathPre = ''; VcpkgDir = $vcp; Url = 'https://example.invalid/vcpkg'; Dry = '0' }
Check 'act cvcpkg: a bootstrapped checkout is left alone and answers' @($r.Code, $r.Kv['vcpkg.triplet'], ($r.Kv['vcpkg.ver'] -like '*vcpkg.exe|vcpkg package management program version 2026-09-01'), $r.Kv['cloned'], $r.Kv['bootstrapped']) @(0, 'True', $true, $null, $null)
$inway = Join-Path $tmp 'inway'; New-Item -ItemType Directory -Force $inway | Out-Null; Set-Content -LiteralPath (Join-Path $inway 'precious.txt') -Value keep
$r = Invoke-Act 'cvcpkg' @{ PathPre = ''; VcpkgDir = $inway; Url = 'https://example.invalid/vcpkg'; Dry = '0' }
Check 'act cvcpkg: a non-git directory in the way is never replaced (rc 3)' @($r.Code, $r.Kv['rc'], (Test-Path (Join-Path $inway 'precious.txt'))) @(3, '3', $true)
Check 'act cvcpkg: and the error names the path' ($r.Kv['err'] -like "*$inway*") $true
$r = Invoke-Act 'cvcpkg' @{ PathPre = ''; VcpkgDir = $inway; Url = 'x'; Dry = '1' }
Check 'act cvcpkg dry: reports the directory in the way' $r.Kv['vcpkg.inway'] 'True'
$r = Invoke-Act 'csdk' @{ PathPre = ''; SdkDir = $inway; Url = 'https://example.invalid/sdk'; Dry = '0' }
Check 'act csdk: a non-git directory in the way is never replaced (rc 3), and the error is the refusal, not a failed clone' @($r.Code, (Test-Path (Join-Path $inway 'precious.txt')), ($r.Kv['err'] -like "*$inway* is there and is not a git checkout*")) @(3, $true, $true)
$r = Invoke-Act 'csdk' @{ PathPre = ''; SdkDir = $sdk; Url = 'https://example.invalid/sdk'; Dry = '1' }
Check 'act csdk dry: absent, nothing written' @($r.Kv['sdk.dirhere'], (Test-Path $sdk)) @('False', $false)

# an existing git checkout is left alone and reported (a real repo made here)
if (Get-Command git -ErrorAction SilentlyContinue) {
    New-Item -ItemType Directory -Force $sdk | Out-Null
    & git -C $sdk init -q -b trunk 2>&1 | Out-Null
    & git -C $sdk -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m first 2>&1 | Out-Null
    Set-Content -LiteralPath (Join-Path $sdk '.gitignore') -Value "/build`n/CMakeUserPresets.json"
    $r = Invoke-Act 'csdk' @{ PathPre = ''; SdkDir = $sdk; Url = 'https://example.invalid/sdk'; Dry = '0' }
    Check 'act csdk: an existing checkout, its branch and dirty state, left alone' @($r.Code, $r.Kv['sdk.branch'], $r.Kv['sdk.dirty'], $r.Kv['sdk.ignored'], $r.Kv['cloned']) @(0, 'trunk', 'True', 'True', $null)
    Check 'act csdk: no submodules, none initialised' $r.Kv['submodules'] $null
    Set-Content -LiteralPath (Join-Path $sdk '.gitmodules') -Value ""
    # a .gitmodules with no submodules in it: the update finds nothing to do and succeeds
    $r = Invoke-Act 'csdk' @{ PathPre = ''; SdkDir = $sdk; Url = 'x'; Dry = '0' }
    Check 'act csdk: submodule update runs when the repo has a .gitmodules' @($r.Code, $r.Kv['submodules']) @(0, '1')
    # some sandboxes refuse to delete a file with this name, and nothing below needs it gone
    Remove-Item -LiteralPath (Join-Path $sdk '.gitmodules') -ErrorAction SilentlyContinue
} else {
    New-Item -ItemType Directory -Force (Join-Path $sdk '.git') | Out-Null
}

# the preset file: read (-Check), created, merged, left alone, hostile
$r = Invoke-Act 'cpread' @{ SdkDir = $sdk }
Check 'act cpread: no file' @($r.Kv['dir'], $r.Kv['file'], $r.Kv['b64']) @('True', 'False', $null)
$pf = Join-Path $sdk 'CMakeUserPresets.json'
$rj = ConvertTo-PresetsJson (Get-CwdmingPresets $vcp $fm1 '')
$r = Invoke-Act 'cpjson' @{ Json = $rj }
Check 'act cpjson: staged' $r.Kv['saved'] '1'
$r = Invoke-Act 'cpresets' @{ PathPre = ''; SdkDir = $sdk }
Check 'act cpresets: created' @($r.Code, $r.Kv['preset'], $r.Kv['added']) @(0, 'created', 'mingw-vcpkg-base,cwdming,cwdming-with-tests')
Check 'act cpresets: the file is valid JSON with the three, and the staged text is gone' @(@((Get-Content -LiteralPath $pf -Raw | ConvertFrom-Json).configurePresets).Count, (Test-Path (Join-Path $tmp 'atrium-presets.json'))) @(3, $false)
Check 'act cpresets: written without a BOM' ([IO.File]::ReadAllBytes($pf)[0] -ne 0xEF) $true
$before = Get-Content -LiteralPath $pf -Raw
Invoke-Act 'cpjson' @{ Json = $rj } | Out-Null
$r = Invoke-Act 'cpresets' @{ PathPre = ''; SdkDir = $sdk }
Check 'act cpresets: a second run is ok and the file is the same' @($r.Kv['preset'], ((Get-Content -LiteralPath $pf -Raw) -eq $before), $r.Kv['wrote']) @('ok', $true, $null)
$rd = Invoke-Act 'cpread' @{ SdkDir = $sdk }
Check 'act cpread: the file comes back whole' ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($rd.Kv['b64'])) -eq $before) $true
# a user's file without ours
Set-Content -LiteralPath $pf -NoNewline -Value $user
Invoke-Act 'cpjson' @{ Json = $rj } | Out-Null
$r = Invoke-Act 'cpresets' @{ PathPre = ''; SdkDir = $sdk }
$merged = Get-Content -LiteralPath $pf -Raw | ConvertFrom-Json
Check 'act cpresets: merged, the include and the differing cwdming kept' @($r.Kv['preset'], $merged.include[0], @($merged.configurePresets | Where-Object { $_.name -eq 'cwdming' })[0].binaryDir) @('merged', 'D:/worktrees/CMakeUserPreset.json', 'X:/mine')
Check 'act cpresets: the old file is next to it' (Get-Content -LiteralPath "$pf.atrium-bak" -Raw) $user
# hostile
foreach ($k in 'not json', 'a list at the top', 'presets an object') {
    Set-Content -LiteralPath $pf -NoNewline -Value $hostile[$k]; Remove-Item -LiteralPath "$pf.atrium-bak" -ErrorAction SilentlyContinue
    Invoke-Act 'cpjson' @{ Json = $rj } | Out-Null
    $r = Invoke-Act 'cpresets' @{ PathPre = ''; SdkDir = $sdk }
    Check "act cpresets: $k is invalid and the file is untouched" @($r.Kv['preset'], ((Get-Content -LiteralPath $pf -Raw) -eq $hostile[$k]), (Test-Path "$pf.atrium-bak")) @('invalid', $true, $false)
}
Remove-Item -LiteralPath $pf
$r = Invoke-Act 'cpresets' @{ PathPre = ''; SdkDir = (Join-Path $tmp 'no-such-dir') }
Check 'act cpresets: no checkout yet' $r.Kv['preset'] 'nodir'

# ── the Unix report, on this machine ────────────────────────────────────────

if ($unix) {
    $script = Join-Path $PSScriptRoot 'room-toolchain.ps1'
    $out = & $pwsh -NoProfile -File $script local -Check -Profile c -Tools go -StateDir (Join-Path $tmp 'state') -SdkDir (Join-Path $tmp 'sdk-none') -VcpkgDir (Join-Path $tmp 'vcpkg-none') 2>&1
    $code = $LASTEXITCODE
    $steps = @($out | ForEach-Object { "$_" } | Where-Object { $_ -match '^room-toolchain (\S+) (\S+)' } | ForEach-Object { $Matches[1] })
    Check 'unix -Check -Profile c: exits 0' $code 0
    Check 'unix -Check -Profile c: reports the C tools' (@('cc', 'gcc', 'cmake', 'ninja', 'git', 'vcpkg', 'sdk-checkout') | Where-Object { $_ -in $steps }) @('cc', 'gcc', 'cmake', 'ninja', 'git', 'vcpkg', 'sdk-checkout')
    Check 'unix -Check -Profile c: a missing directory says MISSING' (($out | Where-Object { $_ -match '^room-toolchain vcpkg warn .*vcpkg-none MISSING' }).Count) 1
    Check 'unix -Check -Profile c: ends ok' ($out | Select-Object -Last 1) 'room-toolchain done ok'
    Check 'unix -Check -Profile c: nothing was created' @((Test-Path (Join-Path $tmp 'sdk-none')), (Test-Path (Join-Path $tmp 'vcpkg-none')), (Test-Path (Join-Path $tmp 'state'))) @($false, $false, $false)
    $plain = & $pwsh -NoProfile -File $script local -Check -Tools go -StateDir (Join-Path $tmp 'state') 2>&1
    Check 'no profile: not one C step is printed' @($plain | Where-Object { $_ -match '^room-toolchain (cc|gcc|cmake|ninja|vcpkg|sdk-checkout|msys2|pacman|git-identity|git-credential|cmake-preset) ' }).Count 0
    Check 'no profile: the old steps are the same (not counting the account step, see test-room-account.ps1)' ((($plain | Where-Object { $_ -notmatch '^room-toolchain account ' }) | ForEach-Object { "$_" -replace '^(room-toolchain \S+ \S+).*', '$1' }) -join '|') ((@('room-toolchain ssh ok', 'room-toolchain prefix ok', 'room-toolchain go ok', 'room-toolchain path skip', 'room-toolchain done ok')) -join '|')

    # the arguments, refused before anything runs: a fake ssh that answers like a Windows host
    $fakeSsh = Join-Path $tmp 'ssh'
    Set-Content -LiteralPath $fakeSsh -NoNewline -Value "#!/bin/sh`nfor last; do :; done`ncase `"`$last`" in uname*) echo 'not a unix' >&2; exit 1;; esac`nexit 9`n"
    & chmod +x $fakeSsh
    foreach ($c in @(
        @('-Msys2Dir', 'relative\path'), @('-Msys2Dir', '/opt/msys64'), @('-VcpkgDir', 'C:\a"b'), @('-GitUserEmail', 'not-an-email'),
        @('-GitUserName', 'Bad "Name"'), @('-CheckRepo', 'https://u:tok@github.com/o/r'), @('-CheckRepo', 'someoneelse/repo'), @('-RunnerAccounts', 'a|b'), @('-Msys2Version', 'latest'))) {
        $o = & $pwsh -NoProfile -File $script 'u@win.invalid' -Ssh $fakeSsh -Check -Profile c @c 2>&1
        Check "args: $($c -join ' ') is exit 1 with room-toolchain args fail" @($LASTEXITCODE, [bool]($o | Where-Object { $_ -match '^room-toolchain args fail ' })) @(1, $true)
    }
    $o = & $pwsh -NoProfile -File $script 'u@win.invalid' -Ssh $fakeSsh -Check -Profile cobol 2>&1
    Check 'args: an unknown profile is refused' ($LASTEXITCODE -ne 0) $true
}


# ── the whole Windows flow, against a pretend room ──────────────────────────
#
# A fake ssh answers `uname` like a Windows host and runs the -EncodedCommand payload under pwsh here. The room is a directory:
# a fake Git for Windows (a script that keeps its config in files), a fake MSYS2 (scripts for gcc, cmake and the rest, and a
# bash that "installs" them for `pacman -S`), a fake vcpkg bootstrap and fake clones. It proves the flow, the step lines, the
# exit codes, the idempotence and that -Check writes nothing. It does not prove Windows: there are no drive letters, no icacls,
# no real pacman, GCM or vcpkg here, and the payload runs under PowerShell 7, not 5.1.

if ($unix) {
    $script = Join-Path $PSScriptRoot 'room-toolchain.ps1'
    $script:simN = 0
    function New-Sim {
        param([bool] $msys = $true, [string[]] $bins = @('gcc', 'g++', 'cmake', 'ninja', 'pkg-config'), [bool] $ssl = $true)
        $script:simN++
        $root = Join-Path $tmp "sim$($script:simN)"
        foreach ($d in 'bin', 'temp', 'st', 'pfx', 'cfg') { New-Item -ItemType Directory -Force (Join-Path $root $d) | Out-Null }
        $git = @'
#!/bin/sh
CFG="$FAKE_CFG"
case "$1" in
--version) echo "git version 2.56.0.windows.1";;
config)
  shift; scope=
  if [ "$1" = --global ]; then scope=g; shift; elif [ "$1" = --system ]; then scope=s; shift; fi
  case "$1" in
    --get-all) [ "$scope" = g ] && cat "$CFG/$2" 2>/dev/null; exit 0;;
    --add) printf '%s\n' "$3" >> "$CFG/$2"; echo "$*" >> "$CFG/calls";;
    user.name|user.email) if [ $# -ge 2 ]; then printf '%s\n' "$2" > "$CFG/$1"; echo "set $1" >> "$CFG/calls"; else cat "$CFG/$1" 2>/dev/null; exit 0; fi;;
  esac;;
credential-manager) echo 2.6.1;;
ls-remote)
  [ -e "$CFG/netdown" ] && { echo "fatal: unable to access" >&2; exit 128; }
  case "$3" in *private*) [ -e "$CFG/login" ] || { echo "fatal: could not read Username for https://github.com" >&2; exit 128; };; esac
  echo "abc123 HEAD";;
rev-parse) echo main;;
status) [ -e "$CFG/dirty" ] && echo " M file";;
clone)
  d=$(printf '%s' "$3" | tr '\\' '/'); mkdir -p "$d/.git" "$d/triplets/community"; echo "clone $2 $d" >> "$CFG/calls"
  echo /CMakeUserPresets.json > "$d/.gitignore"; echo x > "$d/triplets/community/x64-mingw-static.cmake"
  printf '#!/bin/sh\necho "#!/bin/sh" > "$(dirname "$0")/vcpkg.exe"\necho "echo vcpkg package management program version 2026-09-01" >> "$(dirname "$0")/vcpkg.exe"\nchmod +x "$(dirname "$0")/vcpkg.exe"\n' > "$d/bootstrap-vcpkg.bat"; chmod +x "$d/bootstrap-vcpkg.bat";;
submodule) echo "submodule" >> "$CFG/calls";;
esac
exit 0
'@
        foreach ($n in 'git', 'git.exe') { Set-Content -LiteralPath (Join-Path $root "bin/$n") -NoNewline -Value $git; & chmod +x (Join-Path $root "bin/$n") }
        Set-Content -LiteralPath (Join-Path $root 'ssh') -NoNewline -Value "#!/bin/sh`nfor last; do :; done`ncase `"`$last`" in uname*) echo 'not unix' >&2; exit 1;; esac`nenc=`${last##* }`nexec '$pwsh' -NoProfile -NonInteractive -EncodedCommand `"`$enc`"`n"
        & chmod +x (Join-Path $root 'ssh')
        # a fake icacls: it logs what it was given, and with only a directory it prints an ACL like the real one
        Set-Content -LiteralPath (Join-Path $root 'bin/icacls.exe') -NoNewline -Value @'
#!/bin/sh
echo "$*" >> "$FAKE_CFG/icacls.log"
if [ $# -eq 1 ]; then printf '%s\n' "$1 SIMHOST\\sim:(OI)(CI)(F)" "" "Successfully processed 1 files; Failed processing 0 files"; fi
exit 0
'@
        & chmod +x (Join-Path $root 'bin/icacls.exe')
        $ms = Join-Path $root 'msys64'
        $rec = @((Join-Path $root 'bin'))
        if ($msys) {
            New-FakeMsys $ms $bins $ssl
            $rec += "$ms\mingw64\bin"
            Set-Content -LiteralPath (Join-Path $ms 'usr/bin/bash.exe') -NoNewline -Value @'
#!/bin/sh
root=$(cd "$(dirname "$0")/../.." && pwd)
case "$2" in
  exit|"pacman -Syuu"*) echo "ran $2" >> "$root/pacman.log"; exit 0;;
  "pacman -S"*) echo "ran $2" >> "$root/pacman.log"
    for b in gcc g++ cmake ninja pkg-config; do f="$root/mingw64/bin/$b.exe"; [ -e "$f" ] || { printf '#!/bin/sh\necho "%s version 9.9.9"\n' "$b" > "$f"; chmod +x "$f"; }; done
    mkdir -p "$root/mingw64/lib" "$root/mingw64/include/openssl"; touch "$root/mingw64/lib/libssl.a" "$root/mingw64/include/openssl/ssl.h";;
esac
exit 0
'@
            & chmod +x (Join-Path $ms 'usr/bin/bash.exe')
        }
        Set-Content -LiteralPath (Join-Path $root 'st/path.txt') -Value $rec
        [pscustomobject]@{ Root = $root; Msys = $ms; Cfg = (Join-Path $root 'cfg'); Sdk = (Join-Path $root 'git/github/openziti/ziti-sdk-c'); Vcpkg = (Join-Path $root 'vcpkg'); Args = @() }
    }
    # The room's home is its root, so the vcpkg and checkout directories are the defaults, <home>\vcpkg and
    # <home>\git\github\openziti\ziti-sdk-c, with the script's own backslashes (the acts turn them into slashes on a Unix disk).
    function Invoke-Sim {
        param($sim, [string[]] $argv)
        $saved = @{}; foreach ($n in 'HOME', 'PROCESSOR_ARCHITECTURE', 'TEMP', 'PATH', 'FAKE_CFG', 'USERNAME', 'USERDOMAIN') { $saved[$n] = [Environment]::GetEnvironmentVariable($n) }
        Push-Location $sim.Root
        try {
            $env:HOME = $sim.Root; $env:PROCESSOR_ARCHITECTURE = 'AMD64'; $env:TEMP = Join-Path $sim.Root 'temp'
            $env:FAKE_CFG = $sim.Cfg; $env:USERNAME = 'sim'; $env:USERDOMAIN = 'SIMHOST'; $env:PATH = (Join-Path $sim.Root 'bin') + ':' + $saved['PATH']
            $out = & $pwsh -NoProfile -File $script 'u@win.invalid' -Ssh (Join-Path $sim.Root 'ssh') -Tools git -StateDir st -Prefix pfx -Profile c @($sim.Args) @argv 2>&1
            $code = $LASTEXITCODE
        } finally { Pop-Location; foreach ($n in $saved.Keys) { [Environment]::SetEnvironmentVariable($n, $saved[$n]) } }
        if ($env:SIM_DEBUG) { Write-Host "   sim exit $code"; $out | ForEach-Object { Write-Host "   | $_" } }
        [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $code }
    }
    function Get-Step { param($r, [string] $step) @($r.Out | Where-Object { $_ -match "^room-toolchain $step " })[0] }
    function Get-Steps { param($r, [string] $step) @($r.Out | Where-Object { $_ -match "^room-toolchain $step " }) }

    # A. -Check on a room that has everything but the identity, the helper and the preset
    $sim = New-Sim
    New-Item -ItemType Directory -Force (Join-Path $sim.Vcpkg '.git'), (Join-Path $sim.Vcpkg 'triplets/community') | Out-Null
    Set-Content -LiteralPath (Join-Path $sim.Vcpkg 'triplets/community/x64-mingw-static.cmake') -Value x
    Set-Content -LiteralPath (Join-Path $sim.Vcpkg 'vcpkg.exe') -NoNewline -Value "#!/bin/sh`necho 'vcpkg package management program version 2026-09-01'`n"; & chmod +x (Join-Path $sim.Vcpkg 'vcpkg.exe')
    New-Item -ItemType Directory -Force (Join-Path $sim.Sdk '.git') | Out-Null; Set-Content -LiteralPath (Join-Path $sim.Sdk '.gitignore') -Value '/CMakeUserPresets.json'
    $before = @(Get-ChildItem -LiteralPath $sim.Root -Recurse -Force | Where-Object { $_.FullName -notmatch '/\.(cache|local)(/|$)|/cfg(/icacls\.log)?$' } | ForEach-Object { "$($_.FullName)|$($_.Length)|$($_.LastWriteTimeUtc.Ticks)" }) -join "`n"
    $r = Invoke-Sim $sim @('-Check')
    Check 'sim -Check: exit 0 even with a human step' $r.Code 0
    Check 'sim -Check: msys2 is found and used as it is' ((Get-Step $r 'msys2') -like "room-toolchain msys2 ok $($sim.Msys) (found, used as it is)") $true
    foreach ($t in 'gcc', 'cmake', 'ninja', 'pkgconf', 'openssl') { Check "sim -Check: $t ok" ((Get-Step $r $t) -like "room-toolchain $t ok *") $true }
    Check 'sim -Check: gcc says path and version' ((Get-Step $r 'gcc') -like "room-toolchain gcc ok 9.9.9 at $($sim.Msys)/mingw64/bin/gcc.exe*") $true
    Check 'sim -Check: no other runner account here, so no acl work' ((Get-Step $r 'msys2-acl') -like 'room-toolchain msys2-acl ok *') $true
    Check 'sim -Check: the identity is needs-human with the commands' ((Get-Step $r 'git-identity') -like 'room-toolchain git-identity needs-human no user.name and user.email *') $true
    Check 'sim -Check: the helper would be set' ((Get-Step $r 'git-credential') -like 'room-toolchain git-credential warn would set credential.helper manager. public access works*') $true
    Check 'sim -Check: vcpkg ok with its version' ((Get-Step $r 'vcpkg') -like 'room-toolchain vcpkg ok 2026-09-01 at *') $true
    Check 'sim -Check: triplet ok' ((Get-Step $r 'triplet') -like 'room-toolchain triplet ok *') $true
    Check 'sim -Check: the checkout is reported' ((Get-Step $r 'sdk-checkout') -like 'room-toolchain sdk-checkout ok *, branch main, clean, left alone') $true
    Check 'sim -Check: the preset would be created' ((Get-Step $r 'cmake-preset') -like 'room-toolchain cmake-preset warn would create * with mingw-vcpkg-base,cwdming,cwdming-with-tests*') $true
    Check 'sim -Check: the summary block has the git config commands' (@($r.Out | Where-Object { $_ -like 'room-toolchain needs-human git config --global user.*' }).Count) 2
    Check 'sim -Check: ends ok' $r.Out[-1] 'room-toolchain done ok'
    $after = @(Get-ChildItem -LiteralPath $sim.Root -Recurse -Force | Where-Object { $_.FullName -notmatch '/\.(cache|local)(/|$)|/cfg(/icacls\.log)?$' } | ForEach-Object { "$($_.FullName)|$($_.Length)|$($_.LastWriteTimeUtc.Ticks)" }) -join "`n"
    if ($after -ne $before) { Compare-Object ($before -split "`n") ($after -split "`n") | ForEach-Object { Write-Host "     changed: $($_.InputObject)" } }
    Check 'sim -Check: not one file on the room was created or changed' ($after -eq $before) $true
    Check 'sim -Check: git was never configured' @(Get-ChildItem -LiteralPath $sim.Cfg -Force | Where-Object { $_.Name -ne 'icacls.log' }).Count 0

    # B. a real run: identity given, a private repo that wants a login
    $r = Invoke-Sim $sim @('-GitUserName', 'Test User', '-GitUserEmail', 't@example.com', '-CheckRepo', 'dovholuknf/private-repo')
    Check 'sim run: exit 6, finished but needs a human' $r.Code 6
    Check 'sim run: identity done' ((Get-Step $r 'git-identity') -like 'room-toolchain git-identity done set user.name ''Test User'' and user.email ''t@example.com'' in the global config') $true
    Check 'sim run: private repo needs a login, with the one command' ((Get-Step $r 'git-credential') -like 'room-toolchain git-credential needs-human * needs a login *interactive session AS *not ssh*') $true
    Check 'sim run: the summary has the login command, once' (@($r.Out | Where-Object { $_ -eq 'room-toolchain needs-human git credential-manager github login' }).Count) 1
    Check 'sim run: the preset is created' ((Get-Step $r 'cmake-preset') -like 'room-toolchain cmake-preset done created * with mingw-vcpkg-base,cwdming,cwdming-with-tests. cmake --preset cwdming') $true
    $pf = Join-Path $sim.Sdk 'CMakeUserPresets.json'
    $po = Get-Content -LiteralPath $pf -Raw | ConvertFrom-Json
    Check 'sim run: the file is there and says where msys2 and vcpkg are' @($po.configurePresets[0].environment.OPENSSL_ROOT_DIR, $po.configurePresets[0].environment.VCPKG_ROOT) @(($sim.Msys + '/mingw64'), $sim.Vcpkg)
    Check 'sim run: the helper was set in git config' ((Get-Content -LiteralPath (Join-Path $sim.Cfg 'credential.helper')) -join '|') 'manager'
    Check 'sim run: ends with the needs-human step' $r.Out[-1] 'room-toolchain done needs-human 6'
    Check 'sim run: no secret anywhere in the output' (@($r.Out | Where-Object { $_ -match 'password|token|ghp_' }).Count) 0

    # C. the same again after the person logged in: nothing changes, all ok, exit 0
    Set-Content -LiteralPath (Join-Path $sim.Cfg 'login') -Value 1
    $presetBefore = Get-Content -LiteralPath $pf -Raw
    $calls0 = @(Get-Content -LiteralPath (Join-Path $sim.Cfg 'calls')).Count
    $r = Invoke-Sim $sim @('-GitUserName', 'Test User', '-GitUserEmail', 't@example.com', '-CheckRepo', 'dovholuknf/private-repo')
    Check 'sim rerun: exit 0' $r.Code 0
    Check 'sim rerun: nothing says done, nothing needs a human' (@($r.Out | Where-Object { $_ -match '^room-toolchain \S+ (done|needs-human|warn|fail) ' -and $_ -notmatch '^room-toolchain (done|account) ' }).Count) 0
    Check 'sim rerun: the identity is left alone' ((Get-Step $r 'git-identity') -like 'room-toolchain git-identity ok Test User <t@example.com> (already in the global config, left alone)') $true
    Check 'sim rerun: the credential check passes for both' ((Get-Step $r 'git-credential') -like '*public access works, and so does https://github.com/dovholuknf/private-repo.git*') $true
    Check 'sim rerun: the preset is ok and the file is the same' ((Get-Step $r 'cmake-preset') -like 'room-toolchain cmake-preset ok * already has mingw-vcpkg-base,cwdming,cwdming-with-tests, left alone' -and (Get-Content -LiteralPath $pf -Raw) -eq $presetBefore) $true
    Check 'sim rerun: git was not configured again' @(Get-Content -LiteralPath (Join-Path $sim.Cfg 'calls')).Count $calls0
    Check 'sim rerun: ends ok' $r.Out[-1] 'room-toolchain done ok'

    # D. MSYS2 with gaps: pacman runs (the fake), the tools come, the run is ok
    $sim = New-Sim $true @('gcc') $false
    $r = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'sim gaps: pacman ran and is a done step' ((Get-Step $r 'pacman') -like 'room-toolchain pacman done pacman -S --needed mingw-w64-x86_64-toolchain mingw-w64-x86_64-cmake mingw-w64-x86_64-ninja mingw-w64-x86_64-openssl mingw-w64-x86_64-pkgconf') $true
    Check 'sim gaps: only the -S step ran, no core update on an MSYS2 that was there' ((Get-Content -LiteralPath (Join-Path $sim.Msys 'pacman.log')) -join '|') 'ran pacman -S --needed --noconfirm mingw-w64-x86_64-toolchain mingw-w64-x86_64-cmake mingw-w64-x86_64-ninja mingw-w64-x86_64-openssl mingw-w64-x86_64-pkgconf'
    foreach ($t in 'gcc', 'cmake', 'ninja', 'pkgconf', 'openssl') { Check "sim gaps: $t ok after pacman" ((Get-Step $r $t) -like "room-toolchain $t ok *") $true }
    $r2 = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'sim gaps: a second run does not run pacman again' ((Get-Step $r2 'pacman') -eq $null) $true

    # E. -Check on a room with no MSYS2 says what it would do and MISSING
    $sim = New-Sim $false
    $r = Invoke-Sim $sim @('-Check')
    Check 'sim -Check with nothing there: no clone, no directory, no git call, no preset (the dry flag works)' @((Test-Path $sim.Vcpkg), (Test-Path $sim.Sdk), (Test-Path (Join-Path $sim.Cfg 'calls')), (Test-Path (Join-Path $sim.Root 'msys64'))) @($false, $false, $false, $false)
    Check 'sim no msys2: it says MISSING and the five tools too' @(((Get-Step $r 'msys2') -match '^room-toolchain msys2 warn MISSING \(not at '), ((Get-Step $r 'gcc') -like 'room-toolchain gcc warn MISSING. would run: pacman -S --needed *'), ((Get-Step $r 'openssl') -like 'room-toolchain openssl warn MISSING *')) @($true, $true, $true)

    # F. a directory that is not a checkout, in the way: never replaced, a failure (3)
    $sim = New-Sim
    New-Item -ItemType Directory -Force $sim.Sdk | Out-Null; Set-Content -LiteralPath (Join-Path $sim.Sdk 'mine.txt') -Value keep
    $r = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'sim in the way: exit 3' $r.Code 3
    Check 'sim in the way: the step names the path' ((Get-Step $r 'sdk-checkout') -like "room-toolchain sdk-checkout fail $($sim.Root)\git\github\openziti\ziti-sdk-c is there and is not a git checkout*") $true
    Check 'sim in the way: the file is still there' (Test-Path (Join-Path $sim.Sdk 'mine.txt')) $true

    # G. github unreachable: the public check must work, so it is a failure (3) even though a human step exists
    $sim = New-Sim
    Set-Content -LiteralPath (Join-Path $sim.Cfg 'netdown') -Value 1
    $r = Invoke-Sim $sim @()
    Check 'sim net down: exit 3, a failure wins over the human step' $r.Code 3
    Check 'sim net down: git-credential fails' ((Get-Step $r 'git-credential') -like 'room-toolchain git-credential fail *') $true

    # H. a hostile CMakeUserPresets.json is left alone and is a human step
    $sim = New-Sim
    New-Item -ItemType Directory -Force (Join-Path $sim.Sdk '.git') | Out-Null
    Set-Content -LiteralPath (Join-Path $sim.Sdk 'CMakeUserPresets.json') -NoNewline -Value '{ this is not json'
    $r = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'sim hostile preset: exit 6' $r.Code 6
    Check 'sim hostile preset: needs-human, and untouched' @(((Get-Step $r 'cmake-preset') -like 'room-toolchain cmake-preset needs-human * was left alone because it is not valid JSON*'), ((Get-Content -LiteralPath (Join-Path $sim.Sdk 'CMakeUserPresets.json') -Raw) -eq '{ this is not json')) @($true, $true)
    # a good file without ours is merged and keeps a copy
    Set-Content -LiteralPath (Join-Path $sim.Sdk 'CMakeUserPresets.json') -NoNewline -Value '{"version":4,"configurePresets":[{"name":"mine"}]}'
    $r = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'sim merge: merged, the user preset kept, a copy left' @(((Get-Step $r 'cmake-preset') -like 'room-toolchain cmake-preset done added mingw-vcpkg-base,cwdming,cwdming-with-tests to the existing *'), ((Get-Content -LiteralPath (Join-Path $sim.Sdk 'CMakeUserPresets.json') -Raw | ConvertFrom-Json).configurePresets[0].name), (Test-Path (Join-Path $sim.Sdk 'CMakeUserPresets.json.atrium-bak'))) @($true, 'mine', $true)

    # I. vcpkg and the checkout absent: cloned and bootstrapped (by the fakes)
    $sim = New-Sim
    $r = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'sim clone: vcpkg cloned and bootstrapped' ((Get-Step $r 'vcpkg') -like 'room-toolchain vcpkg done *, cloned, bootstrapped') $true
    Check 'sim clone: the triplet is there' ((Get-Step $r 'triplet') -like 'room-toolchain triplet ok *') $true
    Check 'sim clone: the checkout is cloned' ((Get-Step $r 'sdk-checkout') -like 'room-toolchain sdk-checkout done *, cloned now*') $true
    Check 'sim clone: the preset is created in it' (Test-Path (Join-Path $sim.Sdk 'CMakeUserPresets.json')) $true
    Check 'sim clone: both clones were of the public https urls' ((Get-Content -LiteralPath (Join-Path $sim.Cfg 'calls') | Where-Object { $_ -like 'clone *' } | ForEach-Object { ($_ -split ' ')[1] }) -join ' ') 'https://github.com/microsoft/vcpkg https://github.com/openziti/ziti-sdk-c'
}


# ── the MSYS2 stage with a pretend remote: the ACL and pacman branches the Mac disk cannot give ──
#
# The functions of room-toolchain.ps1 are lifted out of it (by the parser) and run here with CCall replaced by a script that
# answers like the room would, so the order of the remote calls, the icacls operations, the refusals and the needs-human lines
# are all checked. What the room itself does with icacls and pacman is not.

$mainAst = [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'room-toolchain.ps1'), [ref]$null, [ref]$null)
foreach ($fn in 'Invoke-CMsys2', 'Get-CMsys', 'Get-PathPre', 'Need', 'Show-Tail', 'Write-CTools', 'Get-Lines') {
    $def = $mainAst.FindAll({ param($n) $n -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq $fn }, $true) | Select-Object -First 1
    if (-not $def) { throw "room-toolchain.ps1 has no function $fn" }
    Invoke-Expression $def.Extent.Text
}
$script:steps = @()
function Step { param([string] $step, [string] $status, [string] $detail = '') $script:steps += "$step $status $detail".TrimEnd() }
function Note-Fail { param([int] $code) if ($script:rc -eq 0) { $script:rc = $code } }
function CCall { param([string] $act, [hashtable] $vars = @{}) $script:calls += [pscustomobject]@{ Act = $act; Vars = $vars }; $r = & $script:mock $act $vars; if ($r) { $r } elseif ($act -eq 'cdisk') { New-Res @{ 'drive.root' = 'V:\'; 'drive.exists' = 'True'; 'drive.readable' = 'True'; anc = 'V:\work'; 'anc.writable' = 'True'; free = '53687091200' } } else { New-Res } }
function New-Res { param($kv = @{}, [string[]] $out = @(), [int] $rc = 0, [string] $err = '') [pscustomobject]@{ Kv = $kv; Out = $out; Rc = $rc; Err = $err; Tail = @() } }
$mdir = 'V:\work\tools\msys64'
function New-Probe {
    param([string[]] $missing = @(), [bool] $writable = $true, [string[]] $acl = @("$mdir BUILTIN\Administrators:(OI)(CI)(F)", '                     SG3\claude:(OI)(CI)(RX)'), [bool] $localai = $true)
    $k = @{ user = 'SG3\claude'; home = 'C:\Users\claude'; 'msys2.dir' = $mdir; 'msys2.found' = 'True'; writable = "$writable"; ssl = $(if ('openssl' -in $missing) { 'False' } else { 'True' })
        'acct.claude' = 'True'; 'acct.localai' = "$localai" }
    foreach ($b in 'gcc', 'g++', 'cmake', 'ninja', 'pkg-config') { $k["bin.$b"] = if ($b -in $missing) { '|' } else { "$mdir\mingw64\bin\$b.exe|$b version 9.9.9" } }
    New-Res $k @($acl | ForEach-Object { "acl=$_" })
}
function Reset-Flow { param([bool] $check = $false)
    $script:steps = @(); $script:calls = @(); $script:needs = @(); $script:rc = 0; $script:cMsysDir = $null
    $script:Check = $check; $script:Force = $false; $script:TestBadHash = $false; $script:RunnerAccounts = @('claude', 'localai')
    $script:prefixR = 'C:\p'; $script:kv = @{ rec = '' }; $script:where = 'u@h'; $script:Msys2Dir = ''; $script:Target = 'u@sg3'; $script:newDirs = @(); $script:found = @{}
}
function Seq { (@($script:calls | ForEach-Object { $_.Act } | Where-Object { $_ -ne 'cacls' }) -join ',') }
function Call { param([string] $act, [int] $n = 0) @($script:calls | Where-Object { $_.Act -eq $act })[$n] }
$allMissing = @('gcc', 'g++', 'cmake', 'ninja', 'pkg-config', 'openssl')

# 1. everything works: RX for localai, Modify for claude while pacman runs, then taken back
Reset-Flow
$state = @{ pacmanDone = $false }
$script:mock = { param($act, $vars)
    switch ($act) {
        'cmsys' { if ($state.pacmanDone) { New-Probe } else { New-Probe $allMissing $false } }
        'cacl' { New-Res @{} @('ok=1') }
        'cpacman' { $state.pacmanDone = $true; New-Res @{} @() }
    } }
Invoke-CMsys2 6>$null | Out-Null
Check 'flow acl: the remote calls, in order' (Seq) 'cmsys,cacl,cstate,cacl,cpacman,cacl,cstate,cmsys'
Check 'flow acl: RX for localai first' (Call 'cacl' 0).Vars.Ops "$mdir|/grant|localai:(OI)(CI)RX"
Check 'flow acl: then Modify for the user who runs pacman' (Call 'cacl' 1).Vars.Ops "$mdir|/grant|SG3\claude:(OI)(CI)M"
Check 'flow acl: pacman gets the package set, no first start on an MSYS2 that was there' @((Call 'cpacman').Vars.Init, (Call 'cpacman').Vars.Pkgs) @('', ($script:CPackages -join ' '))
Check 'flow acl: Modify is taken back, to what the user had (RX)' (Call 'cacl' 2).Vars.Ops "$mdir|/remove:g|SG3\claude`n$mdir|/grant:r|SG3\claude:(OI)(CI)(RX)"
Check 'flow acl: the step lines' ($script:steps | ForEach-Object { ($_ -split ' ')[0..1] -join ' ' }) @('msys2 ok', 'msys2-acl done', 'msys2-acl done', 'pacman done', 'msys2-acl done', 'gcc ok', 'cmake ok', 'ninja ok', 'pkgconf ok', 'openssl ok', 'runner-path needs-human')
Check 'flow acl: no failure, and the only human item is to run it as localai too' @($script:rc, $script:needs.Count, [bool]($script:needs[0] -like "pwsh -File scripts\room-toolchain.ps1 'localai@sg3' -Profile c -Msys2Dir*")) @(0, 1, $true)
Check 'flow acl: mingw64\bin goes to the PATH record' ($script:newDirs -join ';') "$mdir\mingw64\bin"

# 2. icacls refuses the Modify grant: pacman is not run, a human item names the command, nothing is taken back
Reset-Flow
$script:mock = { param($act, $vars)
    switch ($act) {
        'cmsys' { New-Probe $allMissing $false @("$mdir BUILTIN\Administrators:(OI)(CI)(F)", '                     BUILTIN\Users:(OI)(CI)(RX)') }
        'cacl' { New-Res @{ rc = '1' } @() 1 'Access is denied.' }
    } }
Invoke-CMsys2 6>$null | Out-Null
Check 'flow refused: pacman was not run and nothing was reverted (one cacl, the refused grant)' (Seq) 'cmsys,cstate,cacl,cstate,cmsys'
Check 'flow refused: the icacls command for an admin is printed' (@($script:needs | Where-Object { $_ -eq "icacls $mdir /grant 'SG3\claude:(OI)(CI)M'" }).Count) 1
Check 'flow refused: and the pacman command for whoever can' (@($script:needs | Where-Object { $_ -like "$mdir\usr\bin\bash.exe -lc 'pacman -S --needed --noconfirm *" }).Count) 1
Check 'flow refused: those steps are needs-human, and the tools are still reported' (($script:steps | Where-Object { $_ -match '^(msys2-acl|pacman) needs-human ' }).Count) 2
Check 'flow refused: it is not a failure code, a human is' $script:rc 0
# 3. only the RX for the other account is refused: pacman still runs
Reset-Flow; $state = @{ pacmanDone = $false }
$script:mock = { param($act, $vars)
    switch ($act) {
        'cmsys' { if ($state.pacmanDone) { New-Probe } else { New-Probe $allMissing $true @("$mdir SG3\claude:(OI)(CI)(F)") } }
        'cacl' { New-Res @{ rc = '1' } @() 1 'Access is denied.' }
        'cpacman' { $state.pacmanDone = $true; New-Res @{} @() }
    } }
Invoke-CMsys2 6>$null | Out-Null
Check 'flow rx refused: pacman still ran' (Seq) 'cmsys,cacl,cpacman,cmsys'
Check 'flow rx refused: the command for localai is printed' (@($script:needs | Where-Object { $_ -eq "icacls $mdir /grant 'localai:(OI)(CI)RX'" }).Count) 1
# 4. pacman fails: a failure (3), and Modify is still taken back
Reset-Flow
$script:mock = { param($act, $vars)
    switch ($act) {
        'cmsys' { New-Probe $allMissing $false @("$mdir BUILTIN\Users:(OI)(CI)(RX)") $false }
        'cacl' { New-Res @{} @('ok=1') }
        'cpacman' { New-Res @{ rc = '3'; err = 'pacman exited 1' } @() 3 'pacman exited 1' }
    } }
Invoke-CMsys2 6>$null | Out-Null
Check 'flow pacman fails: Modify is taken back anyway' (Seq) 'cmsys,cstate,cacl,cpacman,cacl,cstate,cmsys'
Check 'flow pacman fails: exit code 3 and a pacman fail step' @($script:rc, [bool]($script:steps -like 'pacman fail pacman exited 1')) @(3, $true)
Check 'flow pacman fails: and the tools are still MISSING after it, which is exit 5 only if nothing else failed' ($script:steps | Where-Object { $_ -like 'gcc warn MISSING*' }).Count 1
# 5. -Check: nothing is called but the probe, and it says what it would do
Reset-Flow $true
$script:mock = { param($act, $vars) if ($act -eq 'cmsys') { New-Probe $allMissing $false } elseif ($act -ne 'cacls') { throw "-Check called $act" } }
Invoke-CMsys2 6>$null | Out-Null
Check 'flow check: only the probe was called' (Seq) 'cmsys'
Check 'flow check: the acl step says what it would do' ([bool]($script:steps -like 'msys2-acl warn would grant localai read and execute. would grant SG3\claude Modify while pacman runs and take it back after')) $true
Check 'flow check: each tool says MISSING and the command' (@($script:steps | Where-Object { $_ -match '^(gcc|cmake|ninja|pkgconf|openssl) warn MISSING.*would run: pacman -S --needed --noconfirm ' }).Count) 5
# 6. a fresh install gets the first start and two core updates
Reset-Flow; $state = @{ pacmanDone = $false; installed = $false }
$script:mock = { param($act, $vars)
    switch ($act) {
        'cmsys' { if (-not $state.installed) { New-Res @{ user = 'SG3\claude'; home = 'C:\h'; 'msys2.dir' = $mdir; 'msys2.found' = 'False' } } elseif ($state.pacmanDone) { New-Probe } else { New-Probe $allMissing $true @("$mdir SG3\claude:(OI)(CI)(F)") $false } }
        'cinstall' { $state.installed = $true; New-Res @{ sha = 'abc'; installed = $mdir } }
        'cpacman' { $state.pacmanDone = $true; New-Res @{} @() }
    } }
function Get-Asset { param($t) [pscustomobject]@{ Url = 'https://repo.msys2.org/distrib/x86_64/msys2-base-x86_64-20260927.sfx.exe'; File = 'msys2-base-x86_64-20260927.sfx.exe'; Sha = ('ab' * 32); Version = '20260927' } }
Invoke-CMsys2 6>$null | Out-Null
Check 'flow fresh: install, then pacman with the first start' (Seq) 'cmsys,cdisk,cinstall,cmsys,cpacman,cmsys'
Check 'flow fresh: the install is given the verified hash and the url' @((Call 'cinstall').Vars.Sha, (Call 'cinstall').Vars.Msys2Dir, (Call 'cpacman').Vars.Init) @(('ab' * 32), $mdir, '1')
# 7. a bad hash fails with 4 and nothing else runs
Reset-Flow
$script:TestBadHash = $true
$script:mock = { param($act, $vars)
    switch ($act) {
        'cmsys' { New-Res @{ user = 'u'; home = 'h'; 'msys2.dir' = $mdir; 'msys2.found' = 'False' } }
        'cinstall' { New-Res @{ rc = '4'; err = 'sha256 of x is a, the publisher says b. nothing was unpacked' } @() 4 'sha256 of x is a, the publisher says b. nothing was unpacked' }
    } }
Invoke-CMsys2 6>$null | Out-Null
Check 'flow bad hash: -TestBadHash corrupts the expected hash it sends' ((Call 'cinstall').Vars.Sha -ne ('ab' * 32)) $true
Check 'flow bad hash: exit code 4, nothing after the install' @($script:rc, (Seq)) @(4, 'cmsys,cdisk,cinstall')


# ── four lows: TLS 1.2 for the download, a grant that was cut off, every ACE put back, the 5.1 layout ──

# L1: Windows PowerShell 5.1 offers TLS 1.0 and 1.1 unless told, and cinstall downloads
Check 'L1: cinstall asks for TLS 1.2' ((Get-CPayload 'cinstall' (Get-CWorstVars 'cinstall')) -match 'SecurityProtocol = \[Net\.SecurityProtocolType\]::Tls12') $true
$dl = @($script:CActs.Keys | Where-Object { $script:CActs[$_].Body -match 'Invoke-WebRequest|Invoke-RestMethod|DownloadFile|DownloadString|WebClient' })
Check 'L1: cinstall is the only act that downloads by itself' $dl @('cinstall')
foreach ($a in $dl) { Check "L1: $a asks for TLS 1.2 before it downloads" ($script:CActs[$a].Body.IndexOf('Tls12') -ge 0 -and $script:CActs[$a].Body.IndexOf('Tls12') -lt $script:CActs[$a].Body.IndexOf('Invoke-WebRequest')) $true }

# L3: every explicit ACE the user had is put back
function New-Ace { param($acct, $raw, [bool] $inh = $false, [bool] $deny = $false) [pscustomobject]@{ Account = $acct; Rights = @(); Inherited = $inh; InheritOnly = $false; Deny = $deny; Raw = $raw } }
$two = @((New-Ace 'SG3\claude' '(OI)(CI)(RX)'), (New-Ace 'SG3\claude' '(CI)(IO)(W)'), (New-Ace 'SG3\claude' '(I)(R)' $true), (New-Ace 'SG3\claude' '(DENY)(D)' $false $true), (New-Ace 'SG3\other' '(OI)(CI)(F)'))
$p = New-AclPlan $dirW $two 'SG3\claude' $false @() $true
Check 'L3: both explicit ACEs go back, in one icacls call' @($p.Revert.Count, ($p.Revert[1].Args -join ' ')) @(2, "$dirW /grant:r SG3\claude:(OI)(CI)(RX) /grant SG3\claude:(CI)(IO)(W)")
Check 'L3: the record carries both, not the inherited one, the deny or another account' $p.Before '(OI)(CI)(RX) (CI)(IO)(W)'
Check 'L3: nothing of its own before means only the removal' @(New-RevertOps $dirW 'SG3\claude' @()).Count 1
Check 'L3: three ACEs, three grants' ((New-IcaclsArgs $dirW 'a' 'restore' @('(F)', '(M)', '(R)')) -join ' ') "$dirW /grant:r a:(F) /grant a:(M) /grant a:(R)"
Check 'L3: the ops text is one line per op' ((ConvertTo-AclOps $p.Revert) -split "`n").Count 2

# L2: the grant is recorded on the room
$gr = ConvertFrom-GrantRecord @("1:V:\m|SG3\claude|(OI)(CI)(RX) (CI)(IO)(W)", '2:V:\other|SG3\claude|(F)', '3:v:\M\|sg3\CLAUDE|') 'V:\m' 'SG3\claude'
$g = @($gr.Grants)
Check 'L2 record: only this directory and this account, case and a trailing slash do not matter' @($g.Count, $gr.Bad.Count, (($g | ForEach-Object { "$($_.Line)" }) -join ' ')) @(2, 0, '1 3')
Check 'L2 record: what the account had comes back as a list' $g[0].Before @('(OI)(CI)(RX)', '(CI)(IO)(W)')
Check 'L2 record: an empty before is an empty list' @($g[1].Before).Count 0
Check 'L2 record: the directory is the one asked about, as given' $g[1].Dir 'V:\m'
Check 'L2 record: junk lines are Bad, not acted on' @((ConvertFrom-GrantRecord @('garbage', '|x', 'a|', '||') '' 'SG3\claude').Grants.Count, (ConvertFrom-GrantRecord @('garbage', '|x', 'a|', '||') '' 'SG3\claude').Bad.Count) @(0, 4)
$stdir = Join-Path $tmp 'state'
$kA = "$fm1|SG3\claude"; $kB = "$fm2|SG3\claude"
function Get-Rec { if (Test-Path (Join-Path $stdir 'acl-grants.txt')) { @(Get-Content -LiteralPath (Join-Path $stdir 'acl-grants.txt')) } else { @() } }
$null = Invoke-Act 'cstate' @{ StateDir = $stdir; Mode = 'add'; Key = $kA; Before = '(OI)(CI)(RX)' }
Check 'L2 act: add writes dir|account|before' (Get-Rec) @("$kA|(OI)(CI)(RX)")
$null = Invoke-Act 'cstate' @{ StateDir = $stdir; Mode = 'add'; Key = $kB; Before = '' }
Check 'L2 act: a second grant is a second line' (Get-Rec).Count 2
$null = Invoke-Act 'cstate' @{ StateDir = $stdir; Mode = 'add'; Key = $kA; Before = '(M)' }
Check 'L2 act: the same grant again replaces its line' @((Get-Rec).Count, ((Get-Rec) -contains "$kA|(M)")) @(2, $true)
$r = Invoke-Act 'cacls' @{ Msys2Dir = $fm1; Accts = ''; StateDir = $stdir }
Check 'L2 act: the probe lists what the room remembers' @($r.Out | Where-Object { $_ -like 'grant=*' }).Count 2
$gg = @((ConvertFrom-GrantRecord (@($r.Out | Where-Object { $_ -like 'grant=*' }) | ForEach-Object { $_.Substring(6) }) $fm1 'SG3\claude').Grants)
Check 'L2 act: and it reads back as this directory, this account, what it had' @($gg.Count, $gg[0].Account, ($gg[0].Before -join ' ')) @(1, 'SG3\claude', '(M)')
Check 'L2 act: each line is numbered by its place in the file' @($r.Out | Where-Object { $_ -like 'grant=*' } | ForEach-Object { ($_ -split ':')[0] }) @('grant=1', 'grant=2')
$null = Invoke-Act 'cstate' @{ StateDir = $stdir; Mode = 'remove'; Key = $kA; Before = '' }
Check 'L2 act: remove takes only its line' (Get-Rec) @("$kB|")
$null = Invoke-Act 'cstate' @{ StateDir = $stdir; Mode = 'remove'; Key = $kB; Before = '' }
Check 'L2 act: the last one removed deletes the file' (Test-Path (Join-Path $stdir 'acl-grants.txt')) $false
$odd = Join-Path $tmp "it's here/msys64"
$null = Invoke-Act 'cstate' @{ StateDir = $stdir; Mode = 'add'; Key = "$odd|SG3\o'neil"; Before = "(OI)(CI)(RX)" }
Check 'L2 act: an apostrophe in the directory and the account arrives whole' (Get-Rec) @("$odd|SG3\o'neil|(OI)(CI)(RX)")
$null = Invoke-Act 'cstate' @{ StateDir = $stdir; Mode = 'remove'; Key = "$odd|SG3\o'neil"; Before = '' }

# L2: the flow, with a pretend remote that keeps the room's record
function New-StateMock {
    $script:store = @(); $script:atGrant = @()
    $script:mock = { param($act, $vars)
        switch ($act) {
            'cacls' { $i = 0; New-Res @{} @($script:store | ForEach-Object { $i++; "grant=${i}:$_" }) }
            'cstate' {
                $script:store = @($script:store | Where-Object { -not $_.StartsWith("$($vars.Key)|") })
                if ($vars.Mode -eq 'add') { $script:store += "$($vars.Key)|$($vars.Before)" }
                New-Res
            }
            'cacl' { $script:atGrant += $script:store.Count; if ($script:cfail) { New-Res @{ rc = '1' } @() 1 'Access is denied.' } else { New-Res } }
            'cpacman' { if ($script:drop) { throw 'ssh connection lost' }; $script:pacmanDone = $true; New-Res }
            'cmsys' { & $script:probe }
        } }
}
# a. the record is made BEFORE the grant and dropped after the take-back
Reset-Flow; New-StateMock; $script:cfail = $false; $script:drop = $false; $script:pacmanDone = $false
$script:probe = { if ($script:pacmanDone) { New-Probe -localai $false } else { New-Probe -missing $allMissing -writable $false -localai $false } }
Invoke-CMsys2 6>$null | Out-Null
Check 'L2 flow: the room is told before the Modify grant, and again after the take-back' (Seq) 'cmsys,cstate,cacl,cpacman,cacl,cstate,cmsys'
Check 'L2 flow: when the grant ran the record was already there, when the take-back ran it still was' ($script:atGrant -join ',') '1,1'
Check 'L2 flow: nothing is left on the room afterwards' $script:store.Count 0
Check 'L2 flow: what was recorded is the account and what it had' ((@($script:calls | Where-Object { $_.Act -eq 'cstate' })[0]).Vars.Key + '|' + (@($script:calls | Where-Object { $_.Act -eq 'cstate' })[0]).Vars.Before) "$mdir|SG3\claude|(OI)(CI)(RX)"
# b. icacls refuses the grant: the record is dropped again
Reset-Flow; New-StateMock; $script:cfail = $true; $script:drop = $false; $script:pacmanDone = $false
$script:probe = { New-Probe -missing $allMissing -writable $false -localai $false }
Invoke-CMsys2 6>$null | Out-Null
Check 'L2 flow: a refused grant leaves no record' @($script:store.Count, (Seq)) @(0, 'cmsys,cstate,cacl,cstate,cmsys')
# c. ssh drops during pacman: the run dies, the record stays, the Modify grant is still on the directory
Reset-Flow; New-StateMock; $script:cfail = $false; $script:drop = $true; $script:pacmanDone = $false
$script:probe = { New-Probe -missing $allMissing -writable $false -localai $false }
$died = $null; try { Invoke-CMsys2 6>$null | Out-Null } catch { $died = $_ }
Check 'L2 dropped: the run died in pacman' @([bool]$died, (Seq)) @($true, 'cmsys,cstate,cacl,cpacman')
Check 'L2 dropped: and the room still remembers the grant' $script:store @("$mdir|SG3\claude|(OI)(CI)(RX)")
# d. a rerun: the directory now says writable, nothing would plan a take-back, the record does
$left = @($script:store)
Reset-Flow; New-StateMock; $script:store = $left; $script:cfail = $false; $script:drop = $false
$script:probe = { New-Probe -missing @() -writable $true -localai $false }
Invoke-CMsys2 6>$null | Out-Null
Check 'L2 rerun: it takes the grant back first, then forgets it, then looks again' (Seq) 'cmsys,cacl,cstate,cmsys'
Check 'L2 rerun: with the remove and the restore of what the user had' (@($script:calls | Where-Object { $_.Act -eq 'cacl' })[0]).Vars.Ops "$mdir|/remove:g|SG3\claude`n$mdir|/grant:r|SG3\claude:(OI)(CI)(RX)"
Check 'L2 rerun: the record is gone and the step says so' @($script:store.Count, [bool]($script:steps -like 'msys2-acl done took back the Modify grant for SG3\claude on *')) @(0, $true)
Check 'L2 rerun: pacman was not run, nothing was missing' ((Seq) -notmatch 'cpacman') $true
# e. -Check says it as a warn and writes nothing
Reset-Flow $true; New-StateMock; $script:store = $left; $script:probe = { New-Probe -missing @() -writable $true -localai $false }
Invoke-CMsys2 6>$null | Out-Null
Check 'L2 check: a warn names the account and the directory, no revert, no record change' @((Seq), [bool]($script:steps -like 'msys2-acl warn SG3\claude still has the Modify grant on *'), $script:store.Count) @('cmsys', $true, 1)
# f. the take-back is refused: a human item with the exact commands, and the record stays
Reset-Flow; New-StateMock; $script:store = $left; $script:cfail = $true; $script:probe = { New-Probe -missing @() -writable $true -localai $false }
Invoke-CMsys2 6>$null | Out-Null
Check 'L2 rerun refused: needs-human with the two icacls commands, the record kept' @([bool]($script:steps -like 'msys2-acl needs-human could not take back the Modify grant for SG3\claude*'), @($script:needs | Where-Object { $_ -like 'icacls *' }).Count, $script:store.Count) @($true, 2, 1)

# L2 in the simulated room: a record left by a cut-off run, a fake icacls
if ($unix) {
    $sim = New-Sim
    $rec = Join-Path $sim.Root 'st/acl-grants.txt'
    Set-Content -LiteralPath $rec -Value @("$($sim.Msys)|SIMHOST\sim|(OI)(CI)(RX)", 'garbage', '|x')
    $r = Invoke-Sim $sim @('-Check', '-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'L2 sim -Check: it warns, exits 0 and leaves the record and the ACL alone' @($r.Code, [bool](Get-Steps $r 'msys2-acl' | Where-Object { $_ -like '*SIMHOST\sim still has the Modify grant on *' }), (@(Get-Content -LiteralPath $rec).Count), (-not (Test-Path (Join-Path $sim.Cfg 'icacls.log') -PathType Leaf) -or -not (@(Get-Content -LiteralPath (Join-Path $sim.Cfg 'icacls.log')) -match '/remove:g'))) @(0, $true, 3, $true)
    $r = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    $log = @(Get-Content -LiteralPath (Join-Path $sim.Cfg 'icacls.log'))
    Check 'L2 sim run: the take-back ran (remove, then restore) and the record line is gone, junk is left' @(($log -match '/remove:g SIMHOST\\sim').Count, ($log -match '/grant:r SIMHOST\\sim:\(OI\)\(CI\)\(RX\)').Count, @(Get-Content -LiteralPath $rec).Count, (@(Get-Content -LiteralPath $rec) -contains 'garbage')) @(1, 1, 2, $true)
    Check 'L2 sim run: the step says so and the run is ok' @([bool](Get-Steps $r 'msys2-acl' | Where-Object { $_ -like '*took back the Modify grant for SIMHOST\sim*' }), $r.Code) @($true, 0)
    $r = Invoke-Sim $sim @('-GitUserName', 'T', '-GitUserEmail', 't@example.com')
    Check 'L2 sim rerun: nothing more to take back' @((@(Get-Content -LiteralPath (Join-Path $sim.Cfg 'icacls.log')) -match '/remove:g').Count, $r.Code) @(1, 0)
}

# L4: Windows PowerShell 5.1 writes the merged file in its own layout, and the docs say so
$doc = Get-Content -LiteralPath (Join-Path $PSScriptRoot '../docs/changes/f-c-toolchain.md') -Raw
Check 'L4: the docs say 5.1 reformats a merged CMakeUserPresets.json' ($doc -match '(?s)reformat.{0,200}CMakeUserPresets\.json|CMakeUserPresets\.json.{0,200}reformat') $true
Check 'L4: and say the content is the same' ($doc -match 'same content') $true
Check 'L4: the merge itself keeps the content whatever the layout' ((ConvertFrom-Json (Merge-Presets '{"version":4,"configurePresets":[{"name":"mine","cacheVariables":{"A":"b"}}]}' $json).Text).configurePresets[0].cacheVariables.A) 'b'


# ── M1: the grant record is not believed, and every printed command is quoted in one place ──────────────────────────

# A printed line, parsed by the PowerShell parser the way a person pasting it would run it. $null unless it is exactly one command with no
# subexpression, no variable, no script block and no second statement. Words are the program and each argument as the shell sees it.
function Get-Cmd {
    param([string] $Line)
    $ns = 'System.Management.Automation.Language'
    $errs = $null; $toks = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseInput($Line, [ref]$toks, [ref]$errs)
    if ($errs.Count) { return $null }
    $st = @($ast.EndBlock.Statements)
    if ($st.Count -ne 1 -or $st[0] -isnot [System.Management.Automation.Language.PipelineAst] -or $st[0].PipelineElements.Count -ne 1) { return $null }
    $c = $st[0].PipelineElements[0]
    if ($c -isnot [System.Management.Automation.Language.CommandAst]) { return $null }
    $bad = $ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.SubExpressionAst] -or $n -is [System.Management.Automation.Language.ExpandableStringExpressionAst] -or
        $n -is [System.Management.Automation.Language.VariableExpressionAst] -or $n -is [System.Management.Automation.Language.ScriptBlockExpressionAst] -or
        $n -is [System.Management.Automation.Language.ParenExpressionAst] -or $n -is [System.Management.Automation.Language.ArrayExpressionAst] }, $true)
    if ($bad.Count) { return $null }
    $words = @()
    foreach ($e in $c.CommandElements) {
        if ($e -is [System.Management.Automation.Language.StringConstantExpressionAst]) { $words += $e.Value }
        elseif ($e -is [System.Management.Automation.Language.CommandParameterAst]) { $words += $e.Extent.Text }
        else { return $null }
    }
    [pscustomobject]@{ Words = $words; Amp = ($c.InvocationOperator -eq 'Ampersand') }
}
function Show-Cmd { param([string] $Line) $c = Get-Cmd $Line; if ($c) { ($(if ($c.Amp) { '&' }) + ' ' + ($c.Words -join '<>')).Trim() } else { 'NOT ONE COMMAND' } }

# the quoting function: an honest line, then every kind of value that must stay text
Check 'M1 pasted: an honest icacls line is one command with the ACE whole' (Show-Cmd (Format-AdminCommand @('icacls', 'V:\work\tools\msys64', '/grant', 'SG3\claude:(OI)(CI)M'))) 'icacls<>V:\work\tools\msys64<>/grant<>SG3\claude:(OI)(CI)M'
Check 'M1 pasted: the old unquoted form does not parse as one command (the bug this closes)' (Show-Cmd 'icacls V:\m /grant SG3\claude:(OI)(CI)M') 'NOT ONE COMMAND'
$nasty = @('V:\m', 'a b', "it's", "it$([char]0x2019)s", "it$([char]0x2018)s", 'x$(Write-Output-INJECTED);calc.exe', 'a;calc.exe', 'a&calc.exe', 'a|calc.exe', '`calc', '$env:USERNAME', '(OI)(CI)(F) x', '@(1)', '{ calc }', "a`tb", "a`nb", '', 'SG3\at:(OI)(CI)RX', '--global', '-File', '#c', "a$([char]0x201C)b")
foreach ($w in $nasty) {
    $line = Format-AdminCommand @('icacls', $w, '/remove:g', 'x')
    Check "M1 pasted: '$($w -replace "[`r`n`t]", ' ')' stays one literal argument" (Show-Cmd $line) ('icacls<>' + $w + '<>/remove:g<>x')
}
Check 'M1 pasted: a program that needs quoting is called with &, as one literal' (Show-Cmd (Format-AdminCommand @('V:\my tools\bash.exe', '-lc', 'pacman -S a b'))) '& V:\my tools\bash.exe<>-lc<>pacman -S a b'
Check 'M1 pasted: the git identity and login lines' @((Show-Cmd (Format-AdminCommand @('git', 'config', '--global', 'user.name', 'Your Name'))), (Show-Cmd (Format-AdminCommand @('git', 'credential-manager', 'github', 'login')))) @('git<>config<>--global<>user.name<>Your Name', 'git<>credential-manager<>github<>login')
Check 'M1 pasted: a trailing newline does not pass as a safe word' (Format-AdminCommand @('a', "b`n")) "a 'b`n'"

# the ACE grammar, the one validator for what is given and what is read back
foreach ($ok in '(OI)(CI)(RX)', '(F)', '(CI)(IO)(W)', '(I)(OI)(CI)(RX)', '(OI)(CI)(M)', '(NP)(F)', '(S,RD,AD)') { Check "M1 ace: '$ok' is an ACE" (Test-AceRaw $ok) $true }
foreach ($no in '', '(OI)', '(OI)(CI)(F) x', "(F)`n", '(f)', '(OI)(CI)(F);calc', '$(x)', '(F)(M)', 'F', '(OI)(CI)(F)(F)', "(F)$([char]0x00A0)", '(OI) (F)', '(OI)(CI)(F) (RX)') { Check "M1 ace: '$($no -replace "`n", '\n')' is not" ([bool](Test-AceRaw $no)) $false }
$threw = { param($b) try { & $b | Out-Null; $false } catch { $true } }
Check 'M1 args: a restore with something that is not an ACE is refused' (& $threw { New-IcaclsArgs 'V:\m' 'a' 'restore' @('(F) x$(calc);calc.exe') }) $true
Check 'M1 args: a grant of something that is not a right is refused' (& $threw { New-IcaclsArgs 'V:\m' 'a' 'grant' @('F;calc') }) $true
Check 'M1 args: the rights the script itself grants are allowed' @((& $threw { New-IcaclsArgs 'V:\m' 'a' 'grant' @('RX') }), (& $threw { New-IcaclsArgs 'V:\m' 'a' 'grant' @('M') })) @($false, $false)

# the record, forged
$U = 'SG3\claude'; $D = 'V:\work\tools\msys64'
$forged = @(
    @('account', "1:$D|SG3\attacker|(OI)(CI)(F)"),
    @('account that is only the name', "1:$D|claude|(OI)(CI)(F)"),
    @('account of another domain', "1:$D|OTHER\claude|(OI)(CI)(F)"),
    @('before', "1:$D|$U|(OI)(CI)(F) x"),
    @('injection in before', "1:$D|$U|(OI)(CI)(F) x`$(Write-Output-INJECTED);calc.exe"),
    @('the reviewer line', "1:$D|SG3\attacker|(OI)(CI)(F) x`$(Write-Output-INJECTED);calc.exe"),
    @('injection in the account', "1:$D|SG3\claude;calc.exe|(F)"),
    @('semicolon after an ACE', "1:$D|$U|(F);calc"),
    @('lower case ACE', "1:$D|$U|(f)"),
    @('too few fields', "1:$D|$U"),
    @('a tab in before', "1:$D|$U|(F)`t(M)"),
    @('no number', "$D|SG3\attacker|(OI)(CI)(F)")
)
foreach ($f in $forged) {
    $r = ConvertFrom-GrantRecord @($f[1]) $D $U
    Check "M1 record: a forged $($f[0]) is Bad and never a grant" @($r.Grants.Count, $r.Bad.Count) @(0, 1)
}
$r = ConvertFrom-GrantRecord @("1:C:\other|$U|(F)", "2:${D}\sub|$U|(F)", "3:C:\x|SG3\attacker|(OI)(CI)(F) x`$(calc);calc.exe", "4:${D}2|$U|(F)") $D $U
Check 'M1 record: a forged directory is never acted on and never printed' @($r.Grants.Count, $r.Bad.Count) @(0, 0)
$r = ConvertFrom-GrantRecord @("1:$D|SG3\attacker|(OI)(CI)(F) x`$(Write-Output-INJECTED);calc.exe", "2:$D|$U|(RX)", "3:$D|$U|(F) `$(calc);calc.exe") $D $U
Check 'M1 record: honest and forged lines together, only the honest one is a grant' @(($r.Grants | ForEach-Object { $_.Line }), ($r.Bad -join '|' -replace '\(.*?\)', '()')) @('2', 'line 1 ()|line 3 ()')
Check 'M1 record: what is said about a bad line has no command text in it, only a number and a short excerpt' @((($r.Bad -join ' ') -match 'calc|INJECTED'), (($r.Bad | ForEach-Object { $_.Length } | Measure-Object -Maximum).Maximum -le 40)) @($false, $true)
$e = (ConvertFrom-GrantRecord @("9:ab$([char]27)[31m`r$([char]7)$([char]0x2028)cd|x") $D $U).Bad[0]
Check 'M1 record: control characters are stripped from the excerpt' ($e -match '[\x00-\x1f\x7f-\x9f\u2028\u2029]') $false
Check 'M1 record: and a very long line is cut' ((ConvertFrom-GrantRecord @("5:" + ('x' * 5000)) $D $U).Bad[0].Length -le 40) $true
Check 'M1 record: the line number is the file line' (ConvertFrom-GrantRecord @("17:junk") $D $U).Bad[0] 'line 17 (junk)'

# the flow: a forged record is never acted on, an honest one beside it still is
$evil1 = "$mdir|SG3\attacker|(OI)(CI)(F) x`$(Write-Output-INJECTED);calc.exe"; $evil2 = "$mdir|SG3\claude|(OI)(CI)(F);calc"
foreach ($chk in $false, $true) {
    Reset-Flow $chk; New-StateMock; $script:store = @($evil1, $evil2); $script:cfail = $false; $script:drop = $false
    $script:probe = { New-Probe -missing @() -writable $true -localai $false }
    Invoke-CMsys2 6>$null | Out-Null
    $w = @($script:steps | Where-Object { $_ -like 'msys2-acl warn *' })
    Check "M1 flow$(if ($chk) { ' -Check' }): forged lines are never acted on, no icacls, no record change, no human item" @((Seq), $script:needs.Count, $script:store.Count, @($script:calls | Where-Object { $_.Act -in 'cacl', 'cstate' }).Count) @('cmsys', 0, 2, 0)
    Check "M1 flow$(if ($chk) { ' -Check' }): one warn names both lines by number and echoes no command text" @($w.Count, [bool]($w[0] -like 'msys2-acl warn acl-grants.txt has 2 line(s) this script did not write*line 1 (*line 2 (*'), [bool]($w[0] -match 'calc|INJECTED')) @(1, $true, $false)
}
Reset-Flow; New-StateMock; $script:store = @($evil1, "$mdir|SG3\claude|(OI)(CI)(RX)"); $script:cfail = $false; $script:drop = $false
$script:probe = { New-Probe -missing @() -writable $true -localai $false }
Invoke-CMsys2 6>$null | Out-Null
Check 'M1 flow: the honest line is taken back and the forged one touches nothing' @((Seq), [bool](@($script:calls | Where-Object { $_.Act -eq 'cacl' }).Vars.Ops -match 'attacker|calc'), $script:store.Count) @('cmsys,cacl,cstate,cmsys', $false, 1)

# every printed command, from the flows that print them, is one command with the literal words, even for a directory with the nasty text in it
$evilDir = 'V:\a b\$(Write-Output-INJECTED);calc.exe\it''s\msys64'
Reset-Flow; New-StateMock; $script:cfail = $true; $script:drop = $false; $script:pacmanDone = $false
$script:probe = { $pr = New-Probe -missing $allMissing -writable $false -localai $false; $pr.Kv['msys2.dir'] = $evilDir; $pr }
Invoke-CMsys2 6>$null | Out-Null
Check 'M1 printed: the refused Modify grant gives the icacls line and the pacman line, each one command' @(($script:needs | ForEach-Object { Show-Cmd $_ })) @("icacls<>$evilDir<>/grant<>SG3\claude:(OI)(CI)M", "& $evilDir\usr\bin\bash.exe<>-lc<>pacman -S --needed --noconfirm $($script:CPackages -join ' ')")
Reset-Flow; New-StateMock; $script:store = @("$mdir|SG3\claude|(OI)(CI)(RX) (CI)(IO)(W)"); $script:cfail = $true; $script:probe = { New-Probe -missing @() -writable $true -localai $false }
Invoke-CMsys2 6>$null | Out-Null
Check 'M1 printed: a refused take-back gives the two icacls lines, each one command with the ACEs whole' @(($script:needs | ForEach-Object { Show-Cmd $_ })) @("icacls<>$mdir<>/remove:g<>SG3\claude", "icacls<>$mdir<>/grant:r<>SG3\claude:(OI)(CI)(RX)<>/grant<>SG3\claude:(CI)(IO)(W)")
Reset-Flow; New-StateMock; $script:cfail = $false; $script:drop = $false; $script:pacmanDone = $false; $script:Target = 'u@sg3'; $script:Msys2Dir = ''
$script:probe = { $pr = New-Probe -missing @() -writable $true; $pr.Kv['msys2.dir'] = $evilDir; $pr }
Invoke-CMsys2 6>$null | Out-Null
Check 'M1 printed: the line for another runner account is one command, the directory a literal' @(($script:needs | ForEach-Object { Show-Cmd $_ })) @("pwsh<>-File<>scripts\room-toolchain.ps1<>localai@sg3<>-Profile<>c<>-Msys2Dir<>$evilDir")

# one place: nothing prints a command by itself
Check 'M1 static: the old icacls formatter is gone' ([bool](Get-Command Format-IcaclsCommand -ErrorAction SilentlyContinue)) $false
$needCalls = @($mainAst.FindAll({ param($n) $n -is [System.Management.Automation.Language.CommandAst] -and $n.GetCommandName() -eq 'Need' }, $true))
Check 'M1 static: Need is called from the places that need a person' ($needCalls.Count -ge 8) $true
Check 'M1 static: no caller gives its commands as a string' @($needCalls | Where-Object { $a = $_.CommandElements[-1]; $a -is [System.Management.Automation.Language.StringConstantExpressionAst] -or $a -is [System.Management.Automation.Language.ExpandableStringExpressionAst] }).Count 0
Reset-Flow
Check 'M1 static: Need refuses a command that is a string' (& $threw { Need 'x' 'y' @('git config --global user.name "a"') }) $true
Reset-Flow; Need 'x' 'y' @(, @('git', 'config', '--global', 'user.name', 'A B'))
Check 'M1 static: and Need prints and keeps what Format-AdminCommand made' @($script:needs) @("git config --global user.name 'A B'")

# the simulated room: a forged record on the room is not acted on and not printed
if ($unix) {
    $sim = New-Sim
    $rec = Join-Path $sim.Root 'st/acl-grants.txt'
    Set-Content -LiteralPath $rec -Value @("$($sim.Msys)|SIMHOST\attacker|(OI)(CI)(F) x`$(Write-Output-INJECTED);calc.exe", "$($sim.Msys)|SIMHOST\sim|(OI)(CI)(F) y;calc.exe")
    foreach ($chk in $true, $false) {
        $r = Invoke-Sim $sim @(if ($chk) { '-Check' }; '-GitUserName', 'T', '-GitUserEmail', 't@example.com')
        $icl = if (Test-Path (Join-Path $sim.Cfg 'icacls.log')) { @(Get-Content -LiteralPath (Join-Path $sim.Cfg 'icacls.log') | Where-Object { $_ -match '/grant|/remove|attacker|calc' }) } else { @() }
        Check "sim M1 $(if ($chk) { '-Check' } else { 'run' }): no icacls ran for the forged lines, the record is untouched, nothing with calc.exe in the output" @($icl.Count, @(Get-Content -LiteralPath $rec).Count, [bool](($r.Out -join "`n") -match 'calc|INJECTED'), $r.Code) @(0, 2, $false, 0)
        Check "sim M1 $(if ($chk) { '-Check' } else { 'run' }): it warns about the two lines by number" ([bool](Get-Steps $r 'msys2-acl' | Where-Object { $_ -like '*acl-grants.txt has 2 line(s) this script did not write*line 1 (*line 2 (*' })) $true
    }
}


# ── L5: a record that says the user HAD more than Modify is not believed, and a restore from the record says so ─────────

foreach ($ok in '(OI)(CI)(RX)', '(CI)(IO)(W)', '(OI)(CI)(M)', '(R)', '(N)', '(OI)(CI)(RD,WD,AD)', '(I)(OI)(CI)(RX)', '(RX)') { Check "L5 cap: '$ok' is no higher than Modify" (Test-AceBelowModify $ok) $true }
foreach ($no in '(F)', '(OI)(CI)(F)', '(WDAC)', '(OI)(CI)(WDAC)', '(WO)', '(GA)', '(OI)(CI)(RX,WDAC)', '(M,WO)', '(RX,F)', '(OI)(CI)(XYZ)', '(OI)', '(f)', '(OI)(CI)(M);calc') { Check "L5 cap: '$no' is refused" ([bool](Test-AceBelowModify $no)) $false }
foreach ($f in '(OI)(CI)(F)', '(F)', '(WDAC)', '(OI)(CI)(RX,WDAC)', '(WO)', '(GA)', '(OI)(CI)(RX) (OI)(CI)(F)') {
    $r = ConvertFrom-GrantRecord @("1:$D|$U|$f") $D $U
    Check "L5 record: the user's own account with '$f' is Bad and never a grant" @($r.Grants.Count, $r.Bad.Count) @(0, 1)
}
$r = ConvertFrom-GrantRecord @("1:$D|$U|(OI)(CI)(RX)", "2:$D|$U|(OI)(CI)(M) (CI)(IO)(W)") $D $U
Check 'L5 record: what is no higher than Modify is still a grant' @($r.Grants.Count, $r.Bad.Count) @(2, 0)

# the flow: a forged Full for the user's own account is never restored, and the room is not touched
Reset-Flow; New-StateMock; $script:store = @("$mdir|SG3\claude|(OI)(CI)(F)"); $script:cfail = $false; $script:drop = $false
$script:probe = { New-Probe -missing @() -writable $true -localai $false }
Invoke-CMsys2 6>$null | Out-Null
Check 'L5 flow: a forged (F) for the user is never acted on, nothing printed, one warn' @((Seq), $script:needs.Count, $script:store.Count, @($script:steps | Where-Object { $_ -like 'msys2-acl warn acl-grants.txt has 1 line(s)*' }).Count) @('cmsys', 0, 1, 1)

# a restore that came from the record is labelled, in the line and in the step, and is still one command
Reset-Flow; New-StateMock; $script:store = @("$mdir|SG3\claude|(OI)(CI)(RX) (CI)(IO)(W)"); $script:cfail = $true; $script:probe = { New-Probe -missing @() -writable $true -localai $false }
Invoke-CMsys2 6>$null | Out-Null
$restore = @($script:needs | Where-Object { $_ -like '*/grant:r*' })
$remove = @($script:needs | Where-Object { $_ -like '*/remove:g*' })
Check 'L5 label: the restore line from the record says so' @($restore.Count, [bool]($restore[0] -like "* # from the record, check it")) @(1, $true)
Check 'L5 label: the remove line is not labelled' @($remove.Count, [bool]($remove[0] -like '*# *')) @(1, $false)
Check 'L5 label: the step says the restore comes from the record' ([bool]($script:steps -like 'msys2-acl needs-human could not take back the Modify grant*the restore line comes from the record on the room, check it')) $true
Check 'L5 label: the labelled line is still exactly one command with the same words' (Show-Cmd $restore[0]) "icacls<>$mdir<>/grant:r<>SG3\claude:(OI)(CI)(RX)<>/grant<>SG3\claude:(CI)(IO)(W)"
Check 'L5 label: and so is the summary the run ends with' @((Format-NeedsHuman $script:needs) | ForEach-Object { [bool]($_ -like 'room-toolchain needs-human icacls *') }) @($true, $true)

# a restore the script worked out itself (after pacman, from the live ACL) is not labelled
Reset-Flow; New-StateMock; $script:cfail = $false; $script:drop = $false; $script:pacmanDone = $false
$script:baseMock = $script:mock
$script:mock = { param($act, $vars) if ($act -eq 'cacl' -and $script:pacmanDone) { New-Res @{ rc = '1' } @() 1 'Access is denied.' } else { & $script:baseMock $act $vars } }
$script:probe = { if ($script:pacmanDone) { New-Probe -localai $false } else { New-Probe -missing $allMissing -writable $false -localai $false } }
Invoke-CMsys2 6>$null | Out-Null
Check 'L5 label: a take-back the run planned itself has the two lines and no label' @(@($script:needs | Where-Object { $_ -like 'icacls *' }).Count, @($script:needs | Where-Object { $_ -like '*from the record*' }).Count) @(2, 0)
Check 'L5 label: Format-AdminCommand puts the note after the command and keeps it a comment' @((Format-AdminCommand @('icacls', 'a b') 'from the record, check it'), (Show-Cmd (Format-AdminCommand @('icacls', 'a b') 'from the record, check it'))) @("icacls 'a b' # from the record, check it", 'icacls<>a b')
Check 'L5 label: a note cannot carry a command' (Format-AdminCommand @('icacls', 'a') "x`n; calc.exe `$(1)") 'icacls a # x calc.exe 1'


# ── f-c-preflight: the -Msys2Dir target is probed before anything is said to be installable; an explicit identity is honoured ──

$tdir = 'V:\work\tools\msys64'; $tuser = 'SG3\claude'
$okKv = @{ 'drive.root' = 'V:\'; 'drive.exists' = 'True'; 'drive.readable' = 'True'; anc = 'V:\work'; 'anc.writable' = 'True'; free = "$(50GB)" }
function With-Kv { param($over) $k = $okKv.Clone(); foreach ($n in $over.Keys) { $k[$n] = $over[$n] }; $k }
$v = Test-Msys2Target $okKv $tdir $tuser
Check 'target: a readable drive, a writable folder and room is ok' @($v.Ok, $v.Cmds.Count) @($true, 0)
$v = Test-Msys2Target (With-Kv @{ 'drive.exists' = 'False'; 'drive.readable' = 'False' }) $tdir $tuser
Check 'target: a drive that is not there is not ok, and the command lists the drives' @($v.Ok, (Format-AdminCommand $v.Cmds[0]), [bool]($v.Why -like 'the drive V:\ for *does not exist*')) @($false, 'Get-PSDrive -PSProvider FileSystem', $true)
$v = Test-Msys2Target (With-Kv @{ 'drive.readable' = 'False' }) $tdir $tuser
Check 'target: an unreadable drive is not ok, and the exact icacls line grants RX' @($v.Ok, (Format-AdminCommand $v.Cmds[0])) @($false, "icacls V:\ /grant 'SG3\claude:(OI)(CI)RX'")
$v = Test-Msys2Target (With-Kv @{ 'anc.writable' = 'False' }) $tdir $tuser
Check 'target: a read-only ancestor is not ok, and the exact icacls line grants Modify on it' @($v.Ok, (Format-AdminCommand $v.Cmds[0])) @($false, "icacls V:\work /grant 'SG3\claude:(OI)(CI)M'")
$v = Test-Msys2Target (With-Kv @{ free = "$(1GB)" }) $tdir $tuser
Check 'target: too little space is not ok and says both figures' @($v.Ok, [bool]($v.Why -like 'V:\ has 1 GB free and MSYS2 with its packages needs 6 GB*')) @($false, $true)
Check 'target: space exactly at the need is ok' (Test-Msys2Target (With-Kv @{ free = "$($script:Msys2NeedBytes)" }) $tdir $tuser).Ok $true
Check 'target: a drive that cannot say its free space does not block' (Test-Msys2Target (With-Kv @{ free = '-1' }) $tdir $tuser).Ok $true
Check 'target: the drive is judged before the folder' (Test-Msys2Target (With-Kv @{ 'drive.readable' = 'False'; 'anc.writable' = 'False' }) $tdir $tuser).Cmds[0][1] 'V:\'

# the act, run under pwsh on this disk (no drive letters here): an existing writable folder, a target below it that is not there
$r = Invoke-Act 'cdisk' @{ Msys2Dir = (Join-Path $tmp 'not/yet/msys64') }
Check 'act cdisk: the nearest existing ancestor is found, and it is writable' @($r.Kv['anc'], $r.Kv['anc.writable']) @($tmp, 'True')
Check 'act cdisk: the probe file is gone and free space is a number' @(@(Get-ChildItem -LiteralPath $tmp -Force | Where-Object { $_.Name -like '.atrium-probe-*' }).Count, ([long]$r.Kv['free'] -gt 0)) @(0, $true)

# the stage: MSYS2 not found. Blocked: needs-human, nothing installed, never "would install", the tools not claimed MISSING-and-installable
function New-NotFound { $p = New-Probe -missing $allMissing; $p.Kv['msys2.found'] = 'False'; $p }
foreach ($chk in $false, $true) {
    Reset-Flow $chk
    $script:mock = { param($act, $vars) switch ($act) { 'cmsys' { New-NotFound } 'cdisk' { New-Res (With-Kv @{ 'drive.readable' = 'False' }) } 'cacls' { } default { throw "called $act" } } }
    Invoke-CMsys2 6>$null | Out-Null
    $tag = "stage blocked (check=$chk)"
    Check "$tag`: only the probes ran, nothing was installed" (Seq) 'cmsys,cdisk'
    Check "$tag`: one needs-human step with the icacls line, no would-install" @(($script:steps -like 'msys2 needs-human *').Count, ($script:steps -like '*would install*').Count, ($script:steps -like 'msys2 warn*').Count, ($script:needs -join '|')) @(1, 0, 0, "icacls V:\ /grant 'SG3\claude:(OI)(CI)RX'")
    Check "$tag`: exit code 6 path, not a failure" @($script:rc, (Get-CExitCode $script:rc $script:needs.Count)) @(0, 6)
}
# not blocked: the install goes on exactly as before
Reset-Flow $true
$script:mock = { param($act, $vars) switch ($act) { 'cmsys' { New-NotFound } 'cdisk' { New-Res $okKv } 'cacls' { } default { throw "called $act" } } }
Invoke-CMsys2 6>$null | Out-Null
Check 'stage ok target: -Check says would install, as before' @(($script:steps -like 'msys2 warn MISSING*would install*').Count, ($script:steps -like 'msys2 needs-human*').Count) @(1, 0)

# identity
$ip = Get-GitIdentityPlan 'Old' 'old@x.org' '' ''
Check 'identity plan: nothing given leaves the config alone' @($ip.SetName, $ip.SetEmail, $ip.LackName, $ip.LackEmail) @('', '', $false, $false)
$ip = Get-GitIdentityPlan 'Old' 'old@x.org' 'Old' 'old@x.org'
Check 'identity plan: given and the same is nothing to set' @($ip.SetName, $ip.SetEmail) @('', '')
$ip = Get-GitIdentityPlan 'Old' 'old@x.org' 'New' 'new@x.org'
Check 'identity plan: given and different is set, both' @($ip.SetName, $ip.SetEmail) @('New', 'new@x.org')
$ip = Get-GitIdentityPlan 'Old' 'old@x.org' '' 'new@x.org'
Check 'identity plan: only the email given and different sets only the email' @($ip.SetName, $ip.SetEmail) @('', 'new@x.org')
$ip = Get-GitIdentityPlan '' '' 'New' ''
Check 'identity plan: an empty config takes the given name and still lacks the email' @($ip.SetName, $ip.LackName, $ip.LackEmail) @('New', $false, $true)
$ip = Get-GitIdentityPlan 'Old' 'old@x.org' 'old' 'OLD@x.org'
Check 'identity plan: a difference of case is a difference' @($ip.SetName, $ip.SetEmail) @('old', 'OLD@x.org')


} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ''
if ($script:failed) { Write-Host "$script:failed of $script:ran checks failed."; exit 1 }
Write-Host "all $script:ran checks pass."
