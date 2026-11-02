# What this checkout calls itself, for the PowerShell scripts. The same rule as scripts/version.sh, which is the one
# to read, and it exists because bash on a Windows PATH may be the WSL stub.
#
#   scripts\version.ps1            the tag on HEAD without its v, else <base>+<shorthash>
#   scripts\version.ps1 commit     the full commit, or empty
#   scripts\version.ps1 tree       clean or modified, over tracked files only
param([ValidateSet('version', 'commit', 'tree')][string] $What = 'version')

$root = Split-Path -Parent $PSScriptRoot
$commit = if ($env:ATRIUM_COMMIT) { $env:ATRIUM_COMMIT } else { (git -C $root rev-parse HEAD 2>$null) }
switch ($What) {
    'commit' { "$commit" }
    'tree' { if (git -C $root status --porcelain --untracked-files=no 2>$null) { 'modified' } else { 'clean' } }
    'version' {
        $tag = (git -C $root describe --tags --exact-match 2>$null)
        if ($tag) { $tag -replace '^v', '' } else {
            $base = (Get-Content -LiteralPath (Join-Path $root 'internal/cli/VERSION') -Raw).Trim()
            if ($commit) { "$base+$($commit.Substring(0, 7))" } else { $base }
        }
    }
}
