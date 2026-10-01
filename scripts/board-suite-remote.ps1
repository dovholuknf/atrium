# Run the sharded board suite on another room's machine and bring the report back.
#
#   pwsh -File scripts\board-suite-remote.ps1 [-Room sg3] [-SuiteArgs "--units mHome,phonePan"]
#
# This is what `node scripts/test-board-sharded.js` hands off to by default (see dispatchRemote() there), because the
# suite drives 9 to 15 browsers and lags the machine a person is working at. sg4 lagged (50ms timer drift p99 104ms,
# max 317ms) where sg3 (p99 26ms) and m1mini (p99 5ms) did not, at the same wall time. `--local` on the runner, or
# ATRIUM_SUITE_LOCAL=1, runs it here instead.
#
# WHAT RUNS THERE is the working tree as it is now, committed or not: a snapshot commit built on a temporary index
# (tracked changes AND new files that are not ignored), so nothing here is staged, stashed or committed. It is pushed
# to refs/suite/<id> in the room's clone (the git remote named <room>, made by room-git.ps1), checked out as a
# detached worktree beside the clone, run with the room's own node_modules, and removed afterwards along with the ref.
# Nothing is merged and no branch is made.
#
# The output streams back as it is written and the exit code is the suite's own. 1 and 2 are the suite's. These are
# this script's:
#   3  no git remote named <room> here, or the snapshot could not be pushed
#   4  ssh to the room failed, or its remote runner could not start
param(
    [string] $Room = $(if ($env:ATRIUM_SUITE_ROOM) { $env:ATRIUM_SUITE_ROOM } else { 'sg3' }),
    # Passed to `node scripts/test-board-sharded.js --local` on the room.
    [string] $SuiteArgs = ''
)

$ErrorActionPreference = 'Stop'
$repo = (git rev-parse --show-toplevel).Trim()
Set-Location $repo

$url = (git remote get-url $Room 2>$null)
if (-not $url) { Write-Host "board-suite fail no git remote named $Room here. run scripts/room-git.ps1 init $Room"; exit 3 }

$id = (Get-Date -Format 'MMdd-HHmmss') + '-' + $PID
$head = (git rev-parse HEAD).Trim()

# The working tree as a commit, on a temporary index so the real one is never touched.
$idx = Join-Path ([IO.Path]::GetTempPath()) "suite-index-$id"
try {
    $env:GIT_INDEX_FILE = $idx
    git read-tree HEAD
    git add -A
    $tree = (git write-tree).Trim()
    $sha = (git commit-tree $tree -p $head -m "board suite snapshot $id").Trim()
} finally {
    Remove-Item Env:GIT_INDEX_FILE -ErrorAction SilentlyContinue
    Remove-Item $idx -Force -ErrorAction SilentlyContinue
}
$dirty = $tree -ne (git rev-parse "$head^{tree}").Trim()
Write-Host "board-suite $Room runs $($head.Substring(0, 8))$(if ($dirty) { ' plus the uncommitted changes in this tree' }) as refs/suite/$id"

git push --quiet --force $Room "${sha}:refs/suite/$id" 2>&1 | ForEach-Object { Write-Host $_ }
if ($LASTEXITCODE -ne 0) { Write-Host "board-suite fail could not push the snapshot to $Room"; exit 3 }

$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'; if (-not (Test-Path $ssh)) { $ssh = 'ssh' }
$scp = 'C:\Windows\System32\OpenSSH\scp.exe'; if (-not (Test-Path $scp)) { $scp = 'scp' }

# The remote runner is a file of this repository, copied over fresh on every run so it is always the version here.
$runner = Join-Path $PSScriptRoot 'board-suite-run.ps1'
& $scp -q $runner "${Room}:board-suite-run.ps1"
if ($LASTEXITCODE -ne 0) { Write-Host "board-suite fail could not copy the remote runner to $Room"; exit 4 }

# The room's clone path is the remote url's path part, `host:C:/.../atrium` on Windows.
$clone = ($url -replace '^[^:]+:', '' -replace '^//[^/]+', '')
# A leading `A` keeps the value from being empty, which ssh would drop and the remote parameter would then miss.
$enc = 'A' + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($SuiteArgs))
& $ssh $Room "powershell -NoProfile -ExecutionPolicy Bypass -File board-suite-run.ps1 -Clone $clone -Id $id -Sha $sha -ArgsB64 $enc"
exit $LASTEXITCODE
