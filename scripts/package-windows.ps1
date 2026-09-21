# Build the Windows installer from an already-built release.
#
#   bash scripts/release.sh v0.4.1              # first: the binaries and archives
#   pwsh scripts/package-windows.ps1 v0.4.1     # then: the .msi
#
# Separate from release.sh for the same reason package-linux.sh and
# package-macos.sh are: release.sh needs only a Go toolchain and is what you run
# to check a build compiles everywhere, and this needs the WiX toolset, which is
# a .NET tool. Unlike the .pkg this CAN be built anywhere .NET runs, so it is not
# gated to Windows, but it is written and tested on Windows because that is where
# the MSI is installed.
#
# ALL THE LOGIC IS HERE and the workflow only checks out and calls it, per the
# convention every script in this directory follows.
#
# WHAT IT DOES NOT DO: sign. An unsigned MSI installs with a SmartScreen warning
# and Chocolatey will not accept it. Signing needs a code signing certificate,
# which belongs to a person and not to a repository. Set ATRIUM_SIGN_THUMBPRINT
# to a certificate in the machine store to sign, and leave it empty for the
# unsigned artefact. What signing needs is listed at the end.

param(
    [string] $Version
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $PSScriptRoot
Set-Location $here

if (-not $Version) {
    $Version = (git describe --tags --exact-match 2>$null)
    if (-not $Version) { $Version = 'dev' }
}

# THE NUMERIC MSI VERSION, and why it is not the tag. Windows Installer stores
# ProductVersion as up to four integers and compares them numerically, so it
# rejects a leading v and ignores anything after a prerelease dash. `v0.4.1`
# becomes `0.4.1` and `v0.5.0-rc1` becomes `0.5.0`. The tag keeps its v because
# that is what the release URL contains; the MSI carries the number it can sort.
#
# `dev` has no number, so a dev build is stamped 0.0.0. It is not a version
# anybody upgrades between, which is exactly what that says.
$msiVersion = $Version -replace '^v', ''
$msiVersion = ($msiVersion -split '-')[0]
if ($msiVersion -notmatch '^[0-9]+(\.[0-9]+){1,3}$') {
    if ($Version -eq 'dev' -or $Version -like 'dev*') {
        $msiVersion = '0.0.0'
    } else {
        throw "cannot turn '$Version' into a numeric MSI version. want vMAJOR.MINOR.PATCH."
    }
}

$out = "build.claude/release/$Version"
if (-not (Test-Path -LiteralPath $out)) {
    throw "no release at $out. build one first:  bash scripts/release.sh $Version"
}

$binDir = Join-Path $out "atrium_${Version}_windows_amd64"
$binary = Join-Path $binDir 'atrium.exe'
if (-not (Test-Path -LiteralPath $binary)) {
    throw "no windows/amd64 binary at $binary. run scripts/release.sh $Version first."
}

# FINDING WiX, in the order that respects what is already on the machine, the same
# order package-linux.sh uses for nfpm. A copy on PATH is somebody's choice. Then
# a copy under build.claude/bin, which is where this repository's tools go and is
# gitignored. Otherwise it is installed there with `dotnet tool`, pinned rather
# than @latest: a packaging tool that changes under you between two releases
# produces two differently-shaped installers from one source.
$wixVersion = '5.0.2'
$toolPath = Join-Path $here 'build.claude/bin'
$wix = (Get-Command wix -CommandType Application -ErrorAction SilentlyContinue |
    Select-Object -First 1).Source
if (-not $wix) {
    $local = Join-Path $toolPath 'wix.exe'
    if (Test-Path -LiteralPath $local) { $wix = $local }
}
if (-not $wix) {
    if (-not (Get-Command dotnet -CommandType Application -ErrorAction SilentlyContinue)) {
        throw "no wix and no dotnet to install it. install the .NET SDK, or put wix on PATH."
    }
    Write-Host "wix is not here. installing it into build.claude/bin ($wixVersion)"
    New-Item -ItemType Directory -Path $toolPath -Force | Out-Null
    dotnet tool install wix --version $wixVersion --tool-path $toolPath | Out-Null
    $wix = Join-Path $toolPath 'wix.exe'
}
if (-not (Test-Path -LiteralPath $wix)) {
    throw "could not find or install wix, so there is no way to build the MSI."
}

Write-Host "atrium $Version -> Windows .msi (MSI version $msiVersion)"
Write-Host "  wix: $wix"
$thumbprint = $env:ATRIUM_SIGN_THUMBPRINT
if ($thumbprint) {
    Write-Host "  signing with certificate thumbprint $thumbprint"
} else {
    Write-Host "  UNSIGNED. set ATRIUM_SIGN_THUMBPRINT to sign. see the notes below."
}
Write-Host ""

# ANY .msi ALREADY HERE IS SWEPT AWAY FIRST, for the reason the other packagers
# spell out: a run that fails halfway leaves a partial file behind, and the next
# run hashes whatever it finds and puts that into the release.
Remove-Item -LiteralPath (Join-Path $out '*.msi') -Force -ErrorAction SilentlyContinue

$msi = Join-Path $out "atrium_${Version}_windows_amd64.msi"

# WiX reads $(Version), $(BinDir), $(ScriptsDir) and $(DocDir) from these -d
# definitions. BinDir and ScriptsDir are absolute so the build does not depend on
# where it was invoked from.
& $wix build 'packaging/windows/atrium.wxs' `
    -arch x64 `
    -d "Version=$msiVersion" `
    -d "BinDir=$((Resolve-Path $binDir).Path)" `
    -d "ScriptsDir=$PSScriptRoot" `
    -d "DocDir=$here" `
    -o $msi
if ($LASTEXITCODE -ne 0) { throw "wix build failed." }

if ($thumbprint) {
    # signtool is in the Windows SDK. Resolved from PATH, and its absence is a
    # clear failure rather than a silently unsigned MSI, because asking to sign
    # and getting an unsigned file is the worst of both.
    $signtool = (Get-Command signtool.exe -ErrorAction SilentlyContinue).Source
    if (-not $signtool) {
        throw "ATRIUM_SIGN_THUMBPRINT is set but signtool.exe is not on PATH (install the Windows SDK)."
    }
    & $signtool sign /sha1 $thumbprint /fd SHA256 `
        /tr http://timestamp.digicert.com /td SHA256 $msi
    if ($LASTEXITCODE -ne 0) { throw "signtool failed." }
}

Write-Host "  $(Split-Path -Leaf $msi)"
Write-Host ""

# ONE CHECKSUM FILE, REWRITTEN FROM WHAT IS ACTUALLY PRESENT, and it covers every
# artefact type so this can run in any order alongside the Linux and macOS
# packagers without dropping their .deb, .rpm or .pkg. The format is the lowercase
# `hash  name` that GNU sha256sum writes and that publish-release.sh reads back
# with `sha256sum -c`, so the file this writes on Windows verifies the same as the
# one the bash scripts write elsewhere.
$lines =
    Get-ChildItem -LiteralPath $out -File |
    Where-Object { $_.Extension -in '.zip', '.gz', '.deb', '.rpm', '.pkg', '.msi' } |
    Sort-Object Name |
    ForEach-Object {
        $h = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower()
        "$h  $($_.Name)"
    }
# LF LINE ENDINGS, NOT CRLF, and this is not cosmetic. `sha256sum -c` reads each
# line whole, so a trailing carriage return from a Windows-style newline becomes
# part of the filename and every check fails with "FAILED open or read". WriteAllText
# with an explicit \n join and a BOM-free UTF-8 keeps the file the exact shape the
# bash scripts produce.
[System.IO.File]::WriteAllText(
    (Join-Path (Resolve-Path $out).Path 'checksums.txt'),
    (($lines -join "`n") + "`n"),
    (New-Object System.Text.UTF8Encoding($false)))
$lines | ForEach-Object { Write-Host $_ }

Write-Host ""
Write-Host "in $out"
Write-Host ""
if (-not $thumbprint) {
    @'
NOTHING WAS SIGNED. To ship this without a SmartScreen warning clint must supply:

  A code signing certificate (an OV or EV certificate from a CA, or an Azure
  Trusted Signing subscription). Import it into the machine store, then re-run:

    $env:ATRIUM_SIGN_THUMBPRINT = "<the certificate thumbprint>"
    pwsh scripts/package-windows.ps1 <version>

  An EV certificate builds SmartScreen reputation immediately. An OV one earns it
  over time and downloads. Chocolatey requires a signed binary; Scoop, which is
  already wired up in packaging/scoop-atrium.json, does not.

An unsigned MSI still installs: SmartScreen shows "More info" -> "Run anyway".
'@ | Write-Host
}
