# Install atrium from its latest GitHub release and start it. Run it with:
#   irm https://raw.githubusercontent.com/dovholuknf/atrium/main/install.ps1 | iex
# To pass options, save it and run it:  .\install.ps1 -Dir D:\atrium\bin -Addr 127.0.0.1:7778
# It downloads the zip for this machine, checks it against the release's checksums.txt, puts atrium.exe in -Dir, adds
# -Dir to your user PATH, starts `atrium run` hidden, and prints the board URL.
param(
    [string]$Dir = (Join-Path $HOME '.atrium/bin'),
    [string]$Version = 'latest',
    [string]$Repo = 'dovholuknf/atrium',
    [string]$Addr = '',
    [string[]]$RunArgs = @(),
    [switch]$NoPath,
    [switch]$NoStart
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    default { throw "no atrium release for architecture $($env:PROCESSOR_ARCHITECTURE)" }
}

$api = "https://api.github.com/repos/$Repo/releases"
$rel = if ($Version -eq 'latest') { Invoke-RestMethod "$api/latest" } else { Invoke-RestMethod "$api/tags/$Version" }
$tag = $rel.tag_name
$asset = "atrium_${tag}_windows_$arch.zip"
$base = "https://github.com/$Repo/releases/download/$tag"
Write-Host "installing atrium $tag ($asset)"

$work = Join-Path ([IO.Path]::GetTempPath()) ("atrium-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force $work | Out-Null
try {
    $zip = Join-Path $work $asset
    Invoke-WebRequest "$base/$asset" -OutFile $zip
    $sums = Join-Path $work 'checksums.txt'
    Invoke-WebRequest "$base/checksums.txt" -OutFile $sums

    $want = $null
    foreach ($line in Get-Content $sums) {
        $f = $line -split '\s+', 2
        if ($f.Count -eq 2 -and $f[1].Trim().TrimStart('*') -eq $asset) { $want = $f[0].ToLower() }
    }
    if (-not $want) { throw "checksums.txt has no entry for $asset" }
    $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want) { throw "checksum mismatch for $asset`: want $want, got $got" }
    Write-Host 'checksum ok'

    Expand-Archive $zip (Join-Path $work 'x') -Force
    $exe = Get-ChildItem (Join-Path $work 'x') -Recurse -Filter atrium.exe | Select-Object -First 1
    if (-not $exe) { throw "$asset holds no atrium.exe" }
    New-Item -ItemType Directory -Force $Dir | Out-Null
    $dest = Join-Path $Dir 'atrium.exe'
    try { Copy-Item $exe.FullName $dest -Force } catch { throw "cannot write $dest (is atrium running? run 'atrium stop' first): $_" }
} finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
$dir = (Resolve-Path $Dir).Path
Write-Host "installed $dest"

if (-not $NoPath) {
    $user = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @($user -split ';' | Where-Object { $_ })
    if ($parts -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', (($parts + $dir) -join ';'), 'User')
        Write-Host "added $dir to your user PATH (open a new terminal to use it)"
    }
    if (($env:Path -split ';') -notcontains $dir) { $env:Path = "$env:Path;$dir" }
}

if ($NoStart) { Write-Host "not started. start it with: atrium run"; return }

# atrium run moves to free ports when the default ones are held, and writes the board it bound in hub-addr.json.
$hub = if ($env:WORKTREE_ROOT) { Join-Path $env:WORKTREE_ROOT 'hub/hub' } else { Join-Path $HOME '.atrium/hub' }
function Get-Board {
    try { $b = (Get-Content (Join-Path $hub 'hub-addr.json') -Raw | ConvertFrom-Json).board } catch { return $null }
    if (-not $b) { return $null }
    try { Invoke-WebRequest "http://$b" -UseBasicParsing -TimeoutSec 2 | Out-Null; return "http://$b" } catch { return $null }
}
$log = Join-Path $dir 'run.log'
$argv = @('run') + $(if ($Addr) { @('--addr', $Addr) } else { @() }) + $RunArgs
$p = Start-Process -FilePath $dest -ArgumentList $argv -WindowStyle Hidden -PassThru `
    -RedirectStandardOutput $log -RedirectStandardError "$log.err"
$null = $p.Handle
$url = $null
for ($i = 0; $i -lt 60 -and -not $url; $i++) {
    Start-Sleep -Milliseconds 500
    if ($p.HasExited) {
        Get-Content $log, "$log.err" -ErrorAction SilentlyContinue | Write-Host
        if ($p.ExitCode -ne 0) { throw "atrium run stopped, see $log and $log.err" }
        return
    }
    $url = Get-Board
}
if ($url) { Write-Host "board: $url" } else { Write-Warning "atrium is still starting, its log is $log" }
