# Tests for scripts/room-start.ps1 (the start step on a Windows room) and scripts/room-mingit.ps1 (MinGit for a bare
# Windows room). Needs no ssh, no hub and no room:
#
#   pwsh -NoProfile -File scripts/test-room-start.ps1
#
# The start script is RUN here, locally, with a fake schtasks, a fake health endpoint and a fake atrium, so its branches
# are really taken: the room already up, the task bringing it up, the task unable to (267011) and `room --detach` doing
# it, and both failing. The MinGit release choice is run against a fake GitHub answer. What this cannot run: a real
# Windows remote over ssh, the registry Path edit and the unzip (the install script is only parsed here).
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'room-start.ps1')
. (Join-Path $PSScriptRoot 'room-mingit.ps1')

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
function Check-Match {
    param([string] $name, $got, [string] $pattern)
    $script:ran++
    if ("$got" -match $pattern) { Write-Host "ok   $name" }
    else { $script:failed++; Write-Host "FAIL $name`n     want a match for: $pattern`n     got:  $got" }
}
function Throws { param([scriptblock] $b) try { & $b | Out-Null; $null } catch { "$($_.Exception.Message)" } }
function ConvertTo-Bool { param($msg, [string] $pattern) [bool] ($msg -and $msg -match $pattern) }
function Test-Parses {
    param([string] $text)
    $errs = $null
    [void] [Management.Automation.Language.Parser]::ParseInput($text, [ref] $null, [ref] $errs)
    @($errs).Count -eq 0
}

# ── the start script ────────────────────────────────────────────────────────

$start = Get-WindowsAutostartScript
Check 'start: the script parses' (Test-Parses $start) $true
Check 'start: it falls back to room --detach' ($start -match 'room --detach') $true
Check 'start: it names 267011 and 0x41303' (($start -match '267011') -and ($start -match '41303')) $true

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("room-start-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force $tmp | Out-Null
try {
    $flag = Join-Path $tmp 'up.flag'
    $bin = Join-Path $tmp 'fake-atrium.ps1'
    $runner = Join-Path $tmp 'run.ps1'
    # Fakes for what the remote prelude gives the script: Sch (schtasks), Get-AT, $Bin, $P, and the health call.
    Set-Content -LiteralPath $runner -Value @"
`$ErrorActionPreference = 'Stop'
`$Bin = '$bin'
`$P = '$tmp'
`$StartWait = 2
`$flag = '$flag'
function Get-AT { if (`$env:FAKE_TASK -eq '0') { return `$null }; "`$Bin room --detach" }
function Sch {
    if (`$args[0] -eq '/Run') { if (`$env:FAKE_TASK_RUNS -eq '1') { New-Item -ItemType File -Force `$flag | Out-Null } }
    elseif (`$args[0] -eq '/Query') { "Last Result:                          `$(`$env:FAKE_LAST)" }
}
function Invoke-RestMethod { if (Test-Path `$flag) { 'ok' } else { throw 'connection refused' } }
$start
"@
    Set-Content -LiteralPath $bin -Value "if (`$env:FAKE_DETACH_WORKS -eq '1') { New-Item -ItemType File -Force '$flag' | Out-Null }"
    function Invoke-Start {
        param([hashtable] $env_)
        Remove-Item -LiteralPath $flag -Force -ErrorAction SilentlyContinue
        foreach ($k in 'FAKE_TASK', 'FAKE_TASK_RUNS', 'FAKE_LAST', 'FAKE_DETACH_WORKS', 'FAKE_UP') { Remove-Item "Env:$k" -ErrorAction SilentlyContinue }
        foreach ($k in $env_.Keys) { Set-Item "Env:$k" $env_[$k] }
        if ($env_.FAKE_UP -eq '1') { New-Item -ItemType File -Force $flag | Out-Null }
        $o = & pwsh -NoProfile -File $runner 2>&1
        $kv = @{}
        foreach ($l in $o) { $s = "$l"; $i = $s.IndexOf('='); if ($i -gt 0) { $kv[$s.Substring(0, $i)] = $s.Substring($i + 1) } }
        $kv
    }

    $k = Invoke-Start @{ FAKE_UP = '1' }
    Check 'run: already up is start ok' $k.start 'ok'
    Check 'run: the task was there' $k.autostart 'ok'

    $k = Invoke-Start @{ FAKE_TASK_RUNS = '1' }
    Check 'run: the task brings it up is start done' $k.start 'done'
    Check 'run: no detach was needed' $k.started $null

    $k = Invoke-Start @{ FAKE_LAST = '267011'; FAKE_DETACH_WORKS = '1' }
    Check-Match 'run: 267011 and detach working is start warn' $k.start '^warn '
    Check-Match 'run: the warn says nobody is logged in' $k.start 'nobody is logged in'
    Check-Match 'run: the warn names the result' $k.start 'last result 267011'
    Check-Match 'run: the warn says the task starts it at the next logon' $k.start 'next logon'
    Check 'run: a detach start is marked started' $k.started '1'

    $k = Invoke-Start @{ FAKE_LAST = '0x41303'; FAKE_DETACH_WORKS = '1' }
    Check-Match 'run: 0x41303 is the same warn' $k.start '^warn .*nobody is logged in'

    $k = Invoke-Start @{ FAKE_LAST = '1'; FAKE_DETACH_WORKS = '1' }
    Check-Match 'run: another result still falls back to detach, with its own words' $k.start '^warn the logon task did not bring the room up \(last result 1\)'

    $k = Invoke-Start @{ FAKE_LAST = '267011' }
    Check-Match 'run: detach failing too is start fail' $k.start '^fail .*room --detach did not bring it up either'
    Check 'run: a failure is not marked started' $k.started $null
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    foreach ($k in 'FAKE_TASK', 'FAKE_TASK_RUNS', 'FAKE_LAST', 'FAKE_DETACH_WORKS', 'FAKE_UP') { Remove-Item "Env:$k" -ErrorAction SilentlyContinue }
}

# ── MinGit ──────────────────────────────────────────────────────────────────

Check 'tag: latest is empty' (Get-MinGitTag '') $null
Check 'tag: 2.56.0' (Get-MinGitTag '2.56.0') 'v2.56.0.windows.1'
Check 'tag: a leading v' (Get-MinGitTag 'v2.56.0') 'v2.56.0.windows.1'
Check 'tag: a full windows tag' (Get-MinGitTag '2.56.1.windows.2') 'v2.56.1.windows.2'
Check 'tag: nonsense is refused' ([bool] (Throws { Get-MinGitTag 'latest' })) $true
Check 'tag: a path is refused' ([bool] (Throws { Get-MinGitTag '2.56.0/../x' })) $true

Check 'asset: amd64' (Get-MinGitAssetName '2.56.0' 'AMD64') 'MinGit-2.56.0-64-bit.zip'
Check 'asset: arm64' (Get-MinGitAssetName '2.56.0' 'ARM64') 'MinGit-2.56.0-arm64.zip'
Check 'asset: x86' (Get-MinGitAssetName '2.56.0' 'x86') 'MinGit-2.56.0-32-bit.zip'
Check 'asset: unknown arch' (Get-MinGitAssetName '2.56.0' 'riscv') $null

$h64 = '064b440ff870ed5198527e8f3a92cdf5bd2fd0fedf5e718af95e3fdaddeff718'
$harm = 'cb3b0f2d486ea52673227151a5baf5bc13861ff80e74e94e46d614d1bfcd5c06'
$body = "### Checksums`n`nFile | SHA-256`n-----|--------`nMinGit-2.56.0-64-bit.zip | $h64`nMinGit-2.56.0-arm64.zip | $harm`nMinGit-2.56.0-busybox-64-bit.zip | $('a' * 64)`n"
Check 'hash: the 64-bit zip, not the busybox one' (Read-MinGitHash $body 'MinGit-2.56.0-64-bit.zip') $h64
Check 'hash: arm64' (Read-MinGitHash $body 'MinGit-2.56.0-arm64.zip') $harm
Check 'hash: not published' (Read-MinGitHash $body 'MinGit-9.9.9-64-bit.zip') $null
Check 'hash: a table with edge bars' (Read-MinGitHash "| MinGit-2.56.0-64-bit.zip | $($h64.ToUpper()) |" 'MinGit-2.56.0-64-bit.zip') $h64

$script:asked = $null
$script:fake = $null
function Get-ReleaseJson { param([string] $path) $script:asked = $path; $script:fake }
$assets = @(
    [pscustomobject]@{ name = 'MinGit-2.56.0-64-bit.zip'; browser_download_url = 'https://example.test/MinGit-2.56.0-64-bit.zip'; digest = "sha256:$h64" },
    [pscustomobject]@{ name = 'MinGit-2.56.0-arm64.zip'; browser_download_url = 'https://example.test/MinGit-2.56.0-arm64.zip'; digest = "sha256:$harm" })

$script:fake = [pscustomobject]@{ tag_name = 'v2.56.0.windows.1'; body = $body; assets = $assets }
$r = Get-MinGitRelease '' 'AMD64'
Check 'release: latest asks for latest' $script:asked 'releases/latest'
Check 'release: version' $r.Version '2.56.0.windows.1'
Check 'release: asset' $r.Name 'MinGit-2.56.0-64-bit.zip'
Check 'release: hash from the notes' $r.Sha256 $h64
Check 'release: source' $r.Source 'the release notes'
Check 'release: url' $r.Url 'https://example.test/MinGit-2.56.0-64-bit.zip'
$r = Get-MinGitRelease '2.56.0' 'ARM64'
Check 'release: a named version asks for its tag' $script:asked 'releases/tags/v2.56.0.windows.1'
Check 'release: arm64 hash' $r.Sha256 $harm

$script:fake = [pscustomobject]@{ tag_name = 'v2.56.0.windows.1'; body = 'no table here'; assets = $assets }
$r = Get-MinGitRelease '' 'AMD64'
Check 'release: falls back to the asset digest' $r.Sha256 $h64
Check 'release: and says so' $r.Source "the asset's digest"

$noDigest = @([pscustomobject]@{ name = 'MinGit-2.56.0-64-bit.zip'; browser_download_url = 'https://example.test/x.zip'; digest = $null })
$script:fake = [pscustomobject]@{ tag_name = 'v2.56.0.windows.1'; body = 'no table here'; assets = $noDigest }
Check 'release: no published hash is refused' (ConvertTo-Bool (Throws { Get-MinGitRelease '' 'AMD64' }) 'publishes no SHA256') $true
$script:fake = [pscustomobject]@{ tag_name = 'v2.56.0.windows.1'; body = $body; assets = @() }
Check 'release: no zip is refused' (ConvertTo-Bool (Throws { Get-MinGitRelease '' 'AMD64' }) 'has no asset') $true
$script:fake = [pscustomobject]@{ tag_name = 'v2.56.0.windows.1'; body = $body; assets = $assets }
Check 'release: an unknown arch is refused' (ConvertTo-Bool (Throws { Get-MinGitRelease '' 'riscv' }) 'no MinGit zip for architecture') $true
$script:fake = [pscustomobject]@{ tag_name = 'nightly'; body = $body; assets = $assets }
Check 'release: a tag that is not git for windows is refused' ([bool] (Throws { Get-MinGitRelease '' 'AMD64' })) $true

$inst = Get-MinGitInstallScript '.local\MinGit-2.56.0-64-bit.zip' $h64
Check 'install: the script parses' (Test-Parses $inst) $true
Check 'install: it carries the hash it checks' ($inst -match $h64) $true
Check 'install: it puts cmd on the user Path' ($inst -match "SetEnvironmentVariable\('Path'.*'User'") $true
$tricky = Get-MinGitInstallScript ".local\a'b.zip" $h64
Check 'install: a quote in the zip name still parses' (Test-Parses $tricky) $true

# ── the callers parse and wire the pieces in ────────────────────────────────

foreach ($f in 'provision-room.ps1', 'room-git.ps1', 'room-start.ps1', 'room-mingit.ps1') {
    $e = $null
    [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $f), [ref] $null, [ref] $e) | Out-Null
    Check "parse: $f" @($e).Count 0
}
$prov = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'provision-room.ps1') -Raw
$rg = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'room-git.ps1') -Raw
Check 'provision: uses the start script from room-start.ps1' ($prov -match 'Get-WindowsAutostartScript') $true
Check 'provision: a detach start counts as started now' ($prov -match "needSince = if \(\`$startWord -eq 'done' -or \`$startedNow\)") $true
Check 'provision: passes -GitVersion and -Scp to room-git' (($prov -match "-GitVersion'; \`$GitVersion") -and ($prov -match '-Scp \$Scp')) $true
Check 'provision: passes -Scp to room-gate, whose scp would otherwise be Git''s' ($prov -match "room-gate\.ps1'\) \`$Name -Target \`$Target -Ssh \`$Ssh -Scp \`$Scp") $true
Check 'room-git: installs MinGit only for a Windows remote' ($rg -match "remoteOS -ne 'windows'[\s\S]{0,200}Fail 'git' 3") $true
Check 'room-git: marks the clone it makes' ($rg -match 'atrium\.clone made') $true

if ($script:failed) { Write-Host "$script:failed of $script:ran checks FAILED"; exit 1 }
Write-Host "all $script:ran checks pass"
