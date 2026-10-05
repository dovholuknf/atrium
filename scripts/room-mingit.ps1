# MinGit for a Windows room that has no git, dot-sourced by room-git.ps1 and scripts/test-room-mingit.ps1. It holds
# functions and runs nothing when it is dot-sourced.
#
# WHY MINGIT. A bare Windows room has no winget and no admin over ssh, so `winget install Git.Git` cannot run. MinGit is
# the zip Git for Windows publishes for exactly this: unzip it into the user's home and put its cmd folder on the USER
# Path. Nothing is pinned silently: the latest release is asked for, or the one `-GitVersion` names, and the zip is
# checked against the SHA256 the release itself publishes (the table in its body, else the asset's own digest). A zip
# that does not match is never unpacked. macOS and Linux are not here: they need sudo or xcode-select, so room-git.ps1
# prints the command for those.

# Where on the remote it goes, relative to the home: ~\.local\git, with ~\.local\git\cmd on the user Path.
$script:MinGitHome = '.local\git'

# Get-ReleaseJson is the one network call, a function so a test can replace it. $path is under
# https://api.github.com/repos/git-for-windows/git/.
function Get-ReleaseJson {
    param([string] $path)
    Invoke-RestMethod -Uri "https://api.github.com/repos/git-for-windows/git/$path" -Headers @{ 'User-Agent' = 'atrium-provision'; Accept = 'application/vnd.github+json' } -TimeoutSec 60
}

# Get-MinGitTag turns -GitVersion into the release tag. 2.56.0 is v2.56.0.windows.1, and a full 2.56.1.windows.2 is
# taken as it is. Empty is the latest release, and $null is returned for it. Anything else is refused.
function Get-MinGitTag {
    param([string] $version)
    if (-not $version) { return $null }
    $v = $version.Trim() -replace '^v', ''
    if ($v -match '^\d+\.\d+\.\d+$') { return "v$v.windows.1" }
    if ($v -match '^\d+\.\d+\.\d+\.windows\.\d+$') { return "v$v" }
    throw "-GitVersion '$version' is not a Git for Windows version. use 2.56.0 or 2.56.1.windows.2"
}

# Get-MinGitAssetName is the zip for the remote's architecture.
function Get-MinGitAssetName {
    param([string] $version, [string] $arch)
    $suffix = switch -Regex ($arch) {
        '^(AMD64|amd64|x86_64)$' { '64-bit' }
        '^(ARM64|arm64|aarch64)$' { 'arm64' }
        '^(x86|X86|i386)$' { '32-bit' }
        default { $null }
    }
    if (-not $suffix) { return $null }
    "MinGit-$version-$suffix.zip"
}

# Read-MinGitHash is the SHA256 the release body publishes for one asset, a line `<name> | <hash>`. $null when the body
# has none.
function Read-MinGitHash {
    param([string] $body, [string] $name)
    foreach ($l in ("$body" -split "`r?`n")) {
        if ($l -match "^\s*\|?\s*$([regex]::Escape($name))\s*\|\s*([0-9a-fA-F]{64})\b") { return $Matches[1].ToLower() }
    }
    $null
}

# Get-MinGitRelease says which zip to fetch and what its hash must be: Tag, Version, Name, Url, Sha256 and Source (where
# the hash was read from). Throws, with the reason, when the release has no zip for the arch or publishes no hash.
function Get-MinGitRelease {
    param([string] $version, [string] $arch)
    $tag = Get-MinGitTag $version
    $rel = Get-ReleaseJson $(if ($tag) { "releases/tags/$tag" } else { 'releases/latest' })
    if (-not $rel.tag_name) { throw 'the release answer has no tag_name' }
    if ($rel.tag_name -notmatch '^v(\d+\.\d+\.\d+)\.windows\.\d+$') { throw "the release tag $($rel.tag_name) is not a Git for Windows tag" }
    $ver = $Matches[1]
    # MinGit names the zip by the version without the .windows.N, except for a .windows.2 and later, which keep it.
    $full = $rel.tag_name -replace '^v', ''
    $name = Get-MinGitAssetName $ver $arch
    if (-not $name) { throw "no MinGit zip for architecture $arch" }
    $asset = @($rel.assets) | Where-Object { $_.name -eq $name } | Select-Object -First 1
    if (-not $asset -and $full -ne "$ver.windows.1") {
        $name = Get-MinGitAssetName $full $arch
        $asset = @($rel.assets) | Where-Object { $_.name -eq $name } | Select-Object -First 1
    }
    if (-not $asset) { throw "release $($rel.tag_name) has no asset $name" }
    $sha = Read-MinGitHash $rel.body $asset.name
    $source = 'the release notes'
    if (-not $sha -and "$($asset.digest)" -match '^sha256:([0-9a-fA-F]{64})$') { $sha = $Matches[1].ToLower(); $source = "the asset's digest" }
    if (-not $sha) { throw "release $($rel.tag_name) publishes no SHA256 for $($asset.name), so it is not installed" }
    [pscustomobject]@{ Tag = $rel.tag_name; Version = $full; Name = $asset.name; Url = $asset.browser_download_url; Sha256 = $sha; Source = $source }
}

# Get-MinGitInstallScript is the Windows PowerShell that runs ON THE REMOTE once the zip is there. It checks the zip's
# SHA256 again (so a zip damaged in the copy is caught), unpacks it beside the final folder and moves it into place, and
# puts its cmd folder first on the USER Path. Ends with `git=<git --version>`, or `err=<why>` and a nonzero exit. A rerun
# with the folder already there unpacks nothing and only makes sure the Path is right.
function Get-MinGitInstallScript {
    param([string] $zipRel, [string] $sha256, [string] $homeRel = $script:MinGitHome)
    $q = { param($s) "'" + ($s -replace "'", "''") + "'" }
    "`$Zip = Join-Path `$HOME $(& $q $zipRel)`n`$Want = $(& $q $sha256)`n`$Dest = Join-Path `$HOME $(& $q $homeRel)`n" + @'
$ErrorActionPreference = 'Stop'
$exe = Join-Path $Dest 'cmd\git.exe'
if (-not (Test-Path -LiteralPath $exe)) {
    if (-not (Test-Path -LiteralPath $Zip)) { 'err=the MinGit zip did not arrive'; exit 4 }
    $got = (Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $Want) { "err=SHA256 of the zip on the remote is $got, not the published $Want"; exit 4 }
    $tmp = "$Dest.new"
    if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force }
    Expand-Archive -LiteralPath $Zip -DestinationPath $tmp -Force
    if (-not (Test-Path -LiteralPath (Join-Path $tmp 'cmd\git.exe'))) { 'err=the zip has no cmd\git.exe'; exit 4 }
    if (Test-Path -LiteralPath $Dest) { Remove-Item -LiteralPath $Dest -Recurse -Force }
    Move-Item -LiteralPath $tmp -Destination $Dest
    Remove-Item -LiteralPath $Zip -Force -ErrorAction SilentlyContinue
    'unpacked=1'
}
$cmd = Join-Path $Dest 'cmd'
$cur = [Environment]::GetEnvironmentVariable('Path', 'User')
$parts = @($cur -split ';' | Where-Object { $_ })
if (-not ($parts | Where-Object { $_.TrimEnd('\') -ieq $cmd.TrimEnd('\') })) {
    [Environment]::SetEnvironmentVariable('Path', ((@($cmd) + $parts) -join ';'), 'User')
    'path=added'
}
$env:Path = $cmd + ';' + $env:Path
"git=$(& $exe --version)"
'@
}
