<#
.SYNOPSIS
  Build the atrium docs site (website/) into a static folder ready for GitHub Pages.

.DESCRIPTION
  All of the docs publishing logic lives here, so it runs the same on this machine as in
  .github/workflows/docs.yml. The workflow only checks out, calls this, and hands the output folder to the Pages
  actions. Nothing here publishes anything: it builds, checks, and stops.

  The workflow runs on a push to main that touches website/, docs/, the workflow or this script, and by hand.

  Checks, each fatal:
    - npm ci, from the committed lockfile, so a build is the same build everywhere
    - the starter gate script in docs/hooks.md, run against a mock of atrium's agent port
    - docusaurus build, which already refuses broken links and anchors
    - every root-relative href and src in the output starts with the base URL, so the site works under /atrium/
    - .nojekyll is present, so Pages serves the assets folder Docusaurus writes

.PARAMETER Url
  The site's origin, e.g. https://dovholuknf.github.io. The Pages action reports it as `origin`.

.PARAMETER BaseUrl
  The path the site is served under, e.g. /atrium/. The Pages action reports it as `base_path`, without the
  trailing slash, which this adds.

.PARAMETER OutDir
  Where the finished site goes. Defaults to website/build.

.EXAMPLE
  ./scripts/build-docs.ps1
  npx --prefix website docusaurus serve website --port 3031   # then open http://localhost:3031/atrium/
#>
[CmdletBinding()]
param(
  [string]$Url = 'https://dovholuknf.github.io',
  [string]$BaseUrl = '/atrium/',
  [string]$OutDir = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repo = Split-Path -Parent $PSScriptRoot
$site = Join-Path $repo 'website'
if (-not $OutDir) { $OutDir = Join-Path $site 'build' }

if (-not $BaseUrl.StartsWith('/')) { $BaseUrl = "/$BaseUrl" }
if (-not $BaseUrl.EndsWith('/')) { $BaseUrl = "$BaseUrl/" }
$Url = $Url.TrimEnd('/')

function Invoke-Step([string]$what, [scriptblock]$body) {
  Write-Host "==> $what"
  & $body
  if ($LASTEXITCODE -ne 0) { throw "$what failed with exit code $LASTEXITCODE" }
}

Push-Location $site
try {
  Invoke-Step 'npm ci' { npm ci --no-audit --no-fund }
  Invoke-Step 'the gate script in docs/hooks.md runs as documented' { node scripts/test-gate-hook.js docs/hooks.md }

  $env:ATRIUM_DOCS_URL = $Url
  $env:ATRIUM_DOCS_BASE_URL = $BaseUrl
  Invoke-Step "docusaurus build ($Url$BaseUrl)" { npx docusaurus build --out-dir $OutDir }
} finally {
  Remove-Item Env:ATRIUM_DOCS_URL, Env:ATRIUM_DOCS_BASE_URL -ErrorAction SilentlyContinue
  Pop-Location
}

Write-Host "==> checking every link carries $BaseUrl"
$bad = foreach ($f in Get-ChildItem -Path $OutDir -Recurse -Filter *.html) {
  $html = Get-Content -Raw -LiteralPath $f.FullName
  foreach ($m in [regex]::Matches($html, '(?:href|src)="(/[^"/][^"]*|/)"')) {
    $link = $m.Groups[1].Value
    if (-not $link.StartsWith($BaseUrl)) { "$($f.FullName): $link" }
  }
}
if ($bad) {
  $bad | Select-Object -First 20 | ForEach-Object { Write-Host "  $_" }
  throw "links outside $BaseUrl would break on Pages"
}

$nojekyll = Join-Path $OutDir '.nojekyll'
if (-not (Test-Path $nojekyll)) { New-Item -ItemType File -Path $nojekyll | Out-Null }

Write-Host "built: $OutDir"
