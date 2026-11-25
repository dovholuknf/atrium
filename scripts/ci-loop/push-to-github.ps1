<#
.SYNOPSIS
  Pushes to GitHub without running anything the claude account can write. Runs in the operator's shell so the orchestrator can
  push to origin and rerun CI without a credential of its own.

.DESCRIPTION
  Install this script in a trusted, agent-nonwritable directory before running it.
  Polls <QueueDir>\inbox for one-line requests. Where the work happens:

  - All git work happens in a private clone under -PrivateDir, ACL'd to the operator, SYSTEM and
    Administrators only. The shared checkout (-Repo) is only a source of objects: it is fetched FROM,
    never run IN (only git rev-parse --show-toplevel runs there, with the pins below, to find it when -Repo is not given). Its .git\config, config.worktree and hooks never apply to a git process this loop starts.
  - land-history is a binary built once from a commit the operator approves at start, kept in the private dir. Nothing from
    the branch being landed is compiled or run. -Rebuild shows the land-code commits since the last pin and asks.
  - core.hooksPath, core.fsmonitor and submodule.recurse are pinned through GIT_CONFIG_COUNT for every git process,
    so no config file, including the operator's global one, can add a hook here.
  - A request is moved to done\ and read from there BEFORE it runs. A request that cannot be moved never runs.
    A .txt younger than 2 seconds waits for the next poll, so a half-written file is not read. A request or PR text
    that is a link, or over its size limit (1 KB, 64 KB), is never read, and a log that is a link is replaced.
  - The shared checkout's branches are never switched or moved. Results come back as bundles in out\:
    the orchestrator fetches from out\<request>.bundle (and <request>-ci.bundle for a split) and moves its own refs (see the log line each request prints).

  Verbs:
    land <dev-branch>   one commit on origin/main with shared claude/main's exact tree, land, force-push to
                        origin/<dev-branch>
    push [<name> [<onto>]]
                        one commit on origin/<onto> (default main) taking every file whose content differs in
                        shared hub/claude/<name> (a tree diff, not merge-base, since main squash-merges), land, force-push to
                        origin/<name>, open a PR to main with auto-merge unless it edits CI. A branch that mixes CI
                        and other files is split: origin/<name>-ci (CI only, PR without auto-merge) and origin/<name>
                        A push to main needs the PR text: write <inbox>\<request>.pr.md BEFORE the request file, first line
                        the PR title, the rest the description, in plain words: what changes for the operator and why. Refused
                        (nothing pushed) without it, with a title that is a file, script, path or commit subject, or with
                        no description. The description is Features:, Changes: and Bugs fixed: sections of bullets, 500
                        words at most. A branch with no open PR is refused while an open PR to main has a batch-* head.
    release <version>   such as 0.0.6: when origin/main's head has that VERSION, the tag is new and ci.yml succeeded on that
                        sha, sign tag v<version> there (git tag -s, the loop user's key), push only that tag; the tag push
                        starts release.yml
    delete-tag <tag>    delete an orphan release tag on origin: only vMAJOR.MINOR.PATCH, and refused when a GitHub release exists for it
    rerun <run-id>      gh run rerun <run-id> --failed
    dispatch <ref>      gh workflow run ci.yml --ref <ref>
    retrigger <branch>  rerun, in full, each newest not-green finished run on origin <branch>'s tip
    fetch [<branch>]    git fetch origin <branch>, default main, and bundle it for the orchestrator
    close <pr-number>   gh pr close <pr-number>, only for an open PR whose head is not main, master or dev-*

.EXAMPLE
  pwsh -File <trusted dir>\push-to-github.ps1 -Repo <atrium checkout>    # first run: creates the private clone, builds, asks
.EXAMPLE
  pwsh -File <trusted dir>\push-to-github.ps1 -Rebuild    # run from inside the checkout: -Repo is its top level
.PARAMETER Repo
  The shared checkout requests come from. Default: git rev-parse --show-toplevel of the current directory, else of the
  directory this script is in.
.PARAMETER QueueDir
  Where inbox\, out\ and done\ live. Default: <Repo>\.uncommitted\ci-loop.
.PARAMETER PrivateDir
  The operator-only dir for the clone, the land-history binary and its pin. Default: <LocalApplicationData>\atrium-ci-loop.
#>
param(
    [int]$PollSeconds = 5,
    [string]$Branch,
    [switch]$Rebuild,
    [string]$LandCommit = 'origin/main',
    [string]$OriginUrl = 'git@github.com:dovholuknf/atrium.git',
    [string]$GhRepo = 'dovholuknf/atrium',
    [string]$UserName = 'dovholuknf',
    [string]$UserEmail = '46322585+dovholuknf@users.noreply.github.com',
    [string]$SigningKey = '0EE08251136D3480',
    [string]$Repo,
    [string]$QueueDir,
    [string]$PrivateDir = (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'atrium-ci-loop')
)
$ErrorActionPreference = 'Stop'
$self = Get-Item -LiteralPath $PSCommandPath
$selfHash = (Get-FileHash -LiteralPath $self.FullName -Algorithm SHA256).Hash.Substring(0, 12).ToLower()
Write-Host "push-to-github $selfHash, written $($self.LastWriteTime.ToString('yyyy-MM-dd HH:mm')), $($self.FullName)" -ForegroundColor Cyan
$me = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$priv =[IO.Path]::GetFullPath($PrivateDir)
$clone = Join-Path $priv 'atrium'
$noHooks = Join-Path $priv 'no-hooks'
$pinFile = Join-Path $priv 'pin.txt'
function Test-BranchName([string]$name) { $name -cmatch '^[a-z0-9][a-z0-9-]*$' }
function Test-PushTarget([string]$name) { (Test-BranchName $name) -and $name -notin 'main', 'master' }
if ($Branch -and -not (Test-PushTarget $Branch)) { throw "bad -Branch: $Branch" }

# Highest-precedence git config, inherited by every git this process starts, land-history.exe's included.
$pins = [ordered]@{ 'core.hooksPath' = $noHooks; 'core.fsmonitor' = 'false'; 'submodule.recurse' = 'false' }
$env:GIT_CONFIG_COUNT = $pins.Count
$i = 0
foreach ($k in $pins.Keys) { Set-Item "env:GIT_CONFIG_KEY_$i" $k; Set-Item "env:GIT_CONFIG_VALUE_$i" $pins[$k]; $i++ }
Remove-Item env:GIT_DIR, env:GIT_WORK_TREE, env:GIT_CONFIG_GLOBAL, env:GIT_CONFIG_SYSTEM -ErrorAction SilentlyContinue

# The shared checkout: -Repo, else the top level of the repo this runs in, else of the one this script sits in.
$shared = if ($Repo) { $Repo } else {
    foreach ($d in (Get-Location).ProviderPath, $PSScriptRoot) {
        $top = & git -C $d rev-parse --show-toplevel 2>$null
        if ($LASTEXITCODE -eq 0 -and $top) { $top; break }
    }
}
if (-not $shared) { throw 'no repo: run from inside the atrium checkout, or pass -Repo <path>' }
$shared = [IO.Path]::GetFullPath($shared)
$root = if ($QueueDir) { [IO.Path]::GetFullPath($QueueDir) } else { Join-Path $shared '.uncommitted\ci-loop' }
$inbox = Join-Path $root 'inbox'
$out = Join-Path $root 'out'
$done = Join-Path $root 'done'

function G {
    $o = & git -C $clone @args
    if ($LASTEXITCODE -ne 0) { throw "git $($args -join ' ') failed ($LASTEXITCODE)" }
    $o
}

# Checked GitHub CLI calls; raw calls are reserved for expected failures.
function Invoke-GH {
    $o = & gh @args
    if ($LASTEXITCODE -ne 0) { throw "gh $($args -join ' ') failed ($LASTEXITCODE)" }
    $o
}
# Expected failures: true when the git command succeeds, and its output or nothing.
function Test-Git { & git -C $clone @args *>$null; $LASTEXITCODE -eq 0 }
function Get-MergeBase([string]$a, [string]$b) { $o = & git -C $clone merge-base $a $b 2>$null; if ($LASTEXITCODE -eq 0) { $o } }
function Get-RemoteTip([string]$branch) { $r = G ls-remote origin "refs/heads/$branch"; if ($r) { ($r -split '\s+')[0] } }
# NUL-separated, so a name with a quote, a tab or non-ASCII comes back as it is.
function Get-ChangedFiles($from, $to) {
    (G diff --name-only --no-renames -z $from $to '--' @args | Out-String) -split "`0" | Where-Object { $_.Trim() }
}
# Reads a file the agent wrote: never a link, never over $max bytes.
function Read-Small([string]$path, [int]$max) {
    $i = Get-Item -LiteralPath $path -Force
    if ($i.LinkType) { throw "$($i.Name) is a $($i.LinkType), not reading it" }
    if ($i.Length -gt $max) { throw "$($i.Name) is over $max bytes, not reading it" }
    (Get-Content -LiteralPath $path -Raw) ?? ''
}
function Assert-Arg($parts, [string]$pattern, [string]$what, [string]$line) {
    if ($parts.Count -ne 2 -or $parts[1] -cnotmatch $pattern) { throw "bad ${what}: $line" }
}
function Get-OriginCommit([string]$branch) {
    G fetch --quiet origin "+refs/heads/${branch}:refs/remotes/origin/$branch"
    G rev-parse --verify "refs/remotes/origin/$branch^{commit}"
}
function Invoke-Rerun($id, [switch]$FailedOnly) {
    $flags = if ($FailedOnly) { '--failed' }
    Invoke-GH run rerun $id --repo $GhRepo @flags
}

# Fails when anyone but the operator, SYSTEM or Administrators can write under $priv.
function Assert-Private {
    $ok = @($me, 'S-1-5-18', 'S-1-5-32-544', 'S-1-3-0')
    $write = [Security.AccessControl.FileSystemRights]'Write, Delete, ChangePermissions, TakeOwnership'
    # Everything below these inherits their ACL, and changing a child's ACL needs a write right checked here.
    foreach ($p in $priv, (Join-Path $priv 'bin'), $clone, (Join-Path $clone '.git') | Where-Object { Test-Path $_ }) {
        foreach ($r in (Get-Acl $p).Access) {
            $sid = try { $r.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value } catch { "$($r.IdentityReference)" }
            if ($r.AccessControlType -eq 'Allow' -and ($r.FileSystemRights -band $write) -and $sid -notin $ok) {
                throw "$p grants write to $($r.IdentityReference): the private dir must be operator-only. Fix the ACL or delete $priv."
            }
        }
    }
}

function Initialize-Private {
    if (-not (Test-Path $priv)) {
        New-Item -ItemType Directory $priv | Out-Null
        & icacls $priv /inheritance:r /grant:r "*${me}:(OI)(CI)F" `
            '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "icacls on $priv failed ($LASTEXITCODE)" }
    }
    New-Item -ItemType Directory -Force $noHooks | Out-Null
    Assert-Private
    if (-not (Test-Path (Join-Path $clone '.git'))) {
        & git init --quiet $clone
        if ($LASTEXITCODE -ne 0) { throw "git init failed ($LASTEXITCODE)" }
        G remote add origin $OriginUrl
        # Seed objects from the shared checkout so the first origin fetch is small. Only objects come over: the
        # seed refs are deleted, and every origin/* ref is set by a real fetch from GitHub below.
        Write-Host "first run: copying objects from $shared into $clone (a few minutes)" -ForegroundColor Cyan
        G fetch --quiet --no-tags $shared '+refs/remotes/origin/*:refs/seed/*'
        foreach ($r in @(G for-each-ref --format='%(refname)' refs/seed/)) { G update-ref -d $r }
    }
    # The private clone's config is written here every start, so it says only what this script says.
    G config user.name $UserName
    G config user.email $UserEmail
    G config user.signingkey $SigningKey
    G config gpg.format openpgp
    G config commit.gpgsign false
    G config core.hooksPath $noHooks
    G config remote.origin.url $OriginUrl
    Write-Host "fetching origin into $clone" -ForegroundColor Cyan
    G fetch --quiet origin
}

function Get-LandExe {
    $pin = if (Test-Path $pinFile) { (Get-Content $pinFile -Raw).Trim() } else { '' }
    $exe = if ($pin) { Join-Path $priv "bin\land-history-$($pin.Substring(0, 12)).exe" } else { '' }
    if ($pin -and -not $Rebuild -and (Test-Path $exe)) { return $exe }

    $want = G rev-parse --verify "$LandCommit^{commit}"
    $paths = 'scripts/land-history.go', 'internal/base/land', 'go.mod', 'go.sum'
    Write-Host "land-history will be built from $want ($(G log -1 --format='%h %an %ad: %s' --date=short $want '--'))" -ForegroundColor Cyan
    if ($pin) {
        Write-Host "land code commits since the last pin $($pin.Substring(0, 12)):" -ForegroundColor Cyan
        G log --format='  %h %an %ad: %s' --date=short "$pin..$want" '--' @paths | Write-Host
    } else {
        Write-Host "no pin yet. Last commits touching the land code:" -ForegroundColor Cyan
        G log -10 --format='  %h %an %ad: %s' --date=short $want '--' @paths | Write-Host
    }
    if ((Read-Host "Build and execute land-history from $want? Type YES to approve") -cne 'YES') {
        throw 'land-history build not approved'
    }
    Write-Host "building and pinning $($want.Substring(0, 12))" -ForegroundColor Cyan

    $build = Join-Path $priv 'build'
    if (Test-Path $build) { & git -C $clone worktree remove --force $build 2>$null; Remove-Item -Recurse -Force $build -ErrorAction SilentlyContinue }
    G worktree prune
    G worktree add --quiet --detach $build $want
    try {
        New-Item -ItemType Directory -Force (Join-Path $priv 'bin') | Out-Null
        $exe = Join-Path $priv "bin\land-history-$($want.Substring(0, 12)).exe"
        Push-Location $build
        try {
            $env:GOFLAGS = '-mod=readonly'
            & go build -trimpath -o $exe ./scripts/land-history.go
            if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
        } finally { Pop-Location; Remove-Item env:GOFLAGS }
    } finally {
        & git -C $clone worktree remove --force $build
        G worktree prune
    }
    Set-Content -LiteralPath $pinFile -Value $want -NoNewline
    Write-Host "pinned $exe" -ForegroundColor Green
    $exe
}

# Copies a ref from the shared checkout into the private clone as refs/incoming/<as>. Returns its sha.
function Get-Incoming([string]$ref, [string]$as) {
    G fetch --quiet --no-tags $shared "+${ref}:refs/incoming/$as"
    G rev-parse --verify "refs/incoming/$as^{commit}"
}

# Writes out\<req>.bundle with $ref, minus what the shared checkout already has, and says how to take it.
function Write-Bundle([string]$req, [string]$ref, [string]$to) {
    $have = Get-Incoming 'refs/heads/claude/main' 'shared-claude-main'
    $sha = G rev-parse --verify "$ref^{commit}"
    if (Test-Git merge-base --is-ancestor $sha $have) { "  ref     the shared checkout has $sha already: git update-ref $to $sha"; return }
    $b = Join-Path $out "$req.bundle"
    G bundle create --quiet $b $ref "^$have"
    "  bundle   git fetch $b +${ref}:$to"
}

# Requires $srcSha to descend from the already fetched origin/$onto, lands it and pushes with a lease.
# Nothing is pushed unless $dest ends with $tree, or with no $tree, the tree it had before landing.
function Invoke-Deliver([string]$srcSha, [string]$onto, [string]$dest, [string]$tree) {
    $base = G rev-parse --verify "origin/$onto^{commit}"
    $expected = Get-RemoteTip $dest
    G switch --quiet --detach --force $srcSha
    G clean -ffdxq
    if (-not (Test-Git merge-base --is-ancestor $base HEAD)) { throw "source is not based on ${base}: nothing pushed" }
    G branch --force $dest $srcSha
    if (-not $tree) { $tree = G rev-parse 'HEAD^{tree}' }
    & $script:landExe -base "origin/$onto" -branch $dest
    if ($LASTEXITCODE -ne 0) { throw "land-history failed ($LASTEXITCODE)" }
    if ((G rev-parse "${dest}^{tree}") -ne $tree) { throw "$dest does not have the tree it should land with: nothing pushed" }
    G push --quiet "--force-with-lease=refs/heads/${dest}:$expected" origin "refs/heads/${dest}:refs/heads/$dest"
    "pushed $(G rev-parse --short $dest) to origin/$dest, on $base"
    "  commit   $(G log -1 --format='%h %an: %s' $dest '--')"
    "  ahead    $(G rev-list --count "$base..$dest") commit(s) over $base"
}

function Show-Run($id) {
    $j = & gh run view $id --repo $GhRepo --json databaseId,workflowName,event,headBranch,headSha,displayTitle,status,conclusion,attempt,createdAt,url,jobs 2>$null | ConvertFrom-Json
    if (-not $j) { "  run $id (could not read it)"; return }
    $by = $j.jobs | Group-Object { if ($_.conclusion) { $_.conclusion } else { $_.status } } | ForEach-Object { "$($_.Count) $($_.Name)" }
    "  run      $($j.databaseId)  attempt $($j.attempt)  $($j.workflowName) ($($j.event))"
    "  branch   $($j.headBranch) @ $($j.headSha.Substring(0, 9))"
    "  title    $($j.displayTitle)"
    "  state    $($j.status) $($j.conclusion)  created $($j.createdAt)"
    "  jobs     $($j.jobs.Count): $($by -join ', ')"
    "  url      $($j.url)"
}

# A branch that edits CI and anything else is split in two. A CI-only one gets a PR but no auto-merge.
$gatesMain = '^(\.github/|scripts/ci[^/]*$|scripts/flaky-suite\.json$)'

# The pathspec for $files, as the complement of the other touched files once the list would pass the argv limit.
function Get-Pathspec([string]$srcSha, [string]$onto, [string[]]$files) {
    $lit = @($files | ForEach-Object { ":(literal)$_" })
    if (($lit -join ' ').Length -lt 8000) { return $lit }
    $rest = @(Get-ChangedFiles "origin/$onto" $srcSha | Where-Object { $_ -notin $files })
    if (($rest -join ' ').Length -ge 8000) { throw 'too many files on both sides of the split for a git command line' }
    @('.') + @($rest | ForEach-Object { ":(exclude,literal)$_" })
}

# One commit on origin/$onto holding only $files as $srcSha has them, with the message $msg.
function Split-Branch([string]$srcSha, [string]$onto, [string[]]$files, [string]$msg) {
    $patch = Join-Path $priv 'split.patch'
    $files = Get-Pathspec $srcSha $onto $files
    G diff --binary --no-renames "--output=$patch" "origin/$onto" $srcSha '--' @files
    G switch --quiet --force -C claude/split "origin/$onto"
    G clean -ffdxq
    G apply --index --binary $patch
    G commit --quiet -m $msg
    $left = Get-ChangedFiles $srcSha HEAD @files
    if ($left) { throw "the split commit differs from $srcSha in: $($left -join ', ')" }
    G rev-parse HEAD
}

# The title of a split half comes from the commits that touch its own files, never from the branch tip.
function Get-SplitMessage([string]$srcSha, [string]$onto, [string[]]$files, [string]$prefix) {
    $spec = @(Get-Pathspec $srcSha $onto $files)
    $subjects = @(G log --no-merges --format=%s "origin/$onto..$srcSha" '--' @spec | Select-Object -Unique -First 3)
    if (-not $subjects) { $subjects = @("update $(@($files).Count) file(s)") }
    "$prefix$($subjects -join ', ')"
}

# Real attribution only: a bare product word (claude, codex) is a runner name and passes.
function Test-Attribution([string]$line) {
    $line -match '(?i)co-authored-by|generated with|generated by (an? )?(ai|claude|chatgpt|codex|llm)|🤖|@(anthropic|openai)\.com'
}

# "<what>: <line>" for each line of each {what, text} that carries attribution.
function Get-AttributionHits([object[]]$texts) {
    foreach ($t in $texts) {
        foreach ($l in ($t.text -split "`n")) { if (Test-Attribution $l) { "$($t.what): $($l.Trim())" } }
    }
}

# True when origin/$onto already has these files as $sha has them, or an open PR branch carries them.
function Test-CiCovered([string]$sha, [string]$onto, [string[]]$files) {
    $spec = @(Get-Pathspec $sha $onto $files)
    if (-not (Get-ChangedFiles "origin/$onto" $sha @spec)) { return $true }
    if ($onto -ne 'main') { return $false }
    foreach ($head in @(Invoke-GH pr list --repo $GhRepo --state open --limit 200 --json headRefName --jq '.[].headRefName')) {
        if (-not $head) { continue }
        if (-not (Test-Git fetch --quiet origin $head)) { continue }
        if (-not (Get-ChangedFiles FETCH_HEAD $sha @spec)) { return $true }
    }
    $false
}

# The PR title and description the sender wrote in <inbox>\<req>.pr.md: first line the title, the rest the body. Refuses a
# missing file, a title that is a script, a file, a path or one of the branch's commit subjects, a body with no words, and a body that is not Features:, Changes: and Bugs fixed: sections of bullets in 500 words.
function Get-PrText([string]$req, [string[]]$subjects) {
    $f = Join-Path $inbox "$req.pr.md"
    if (-not (Test-Path -LiteralPath $f)) { throw "no PR text: write $f before the request (first line the title, then what changes for the operator and why). Nothing pushed." }
    $lines = @((Read-Small $f 65536) -split "`r?`n")
    $title = $lines[0].Trim() -replace '^#+\s*', ''
    $body = (($lines | Select-Object -Skip 1) -join "`n").Trim()
    if ($title.Length -lt 12) { throw "PR title '$title' is too short to say what changes. Nothing pushed." }
    if ($title -match '\S+\.(ps1|sh|js|go|py|ya?ml|json|md|html|css|txt)\b|\S/\S') { throw "PR title '$title' names a file, a script or a path. Say what changes for the operator and why. Nothing pushed." }
    if ($title -in $subjects) { throw "PR title '$title' is a commit subject. Say what changes for the operator and why. Nothing pushed." }
    if ($body.Length -lt 20) { throw "PR $req.pr.md has no description under the title. Nothing pushed." }
    $secs = [Collections.Generic.List[object]]::new()
    foreach ($ln in ($body -split "`n")) {
        $t = $ln.TrimEnd()
        if (-not $t) { continue }
        if ($t -match '^(Features|Changes|Bugs fixed):$') {
            $secs.Add(@{ name = $Matches[1]; bullets = 0 })
        } elseif ($t -match '^[-*] \S') {
            if (-not $secs.Count) { throw "PR $req.pr.md: a bullet sits outside a Features:, Changes: or Bugs fixed: section. Nothing pushed." }
            $secs[-1].bullets++
        } else {
            throw "PR $req.pr.md: the description holds only Features:, Changes: and Bugs fixed: sections of bullets, no other prose (not '$t'). Nothing pushed."
        }
    }
    if (-not $secs.Count) { throw "PR $req.pr.md: the description needs at least one of Features:, Changes: or Bugs fixed: with bullets. Nothing pushed." }
    foreach ($s in $secs) { if (-not $s.bullets) { throw "PR $req.pr.md: the '$($s.name)' section has no bullets, leave an empty section out. Nothing pushed." } }
    if (@($body -split '\s+' | Where-Object { $_ }).Count -gt 500) { throw "PR $req.pr.md: the description is over 500 words. Nothing pushed." }
    @{ title = $title; body = $body }
}

function Invoke-Request([string]$req, [string]$line) {
    Assert-Private
    $parts = $line -split '\s+'
    switch ($parts[0]) {
        'land' {
            Assert-Arg $parts '^dev-[a-z0-9-]+$' 'land target' $line
            $src = Get-Incoming 'refs/heads/claude/main' 'claude-main'
            $null = Get-OriginCommit main
            if (-not (G diff --name-only origin/main $src '--')) { throw 'nothing to land: claude/main has the same tree as origin/main' }
            # One commit with claude/main's tree: a rebase drops merges, and with them what their resolutions deleted.
            $one = G commit-tree "${src}^{tree}" -p origin/main -m "Land $($parts[1])"
            Invoke-Deliver $one 'main' $parts[1] (G rev-parse "${src}^{tree}")
            Write-Bundle $req "refs/heads/$($parts[1])" "refs/remotes/origin/$($parts[1])"
        }
        'push' {
            if ($parts.Count -gt 3) { throw "bad push: $line" }
            $name = if ($parts.Count -ge 2) { $parts[1] } else { $Branch }
            $onto = if ($parts.Count -eq 3) { $parts[2] } else { 'main' }
            if (-not $name) { throw 'push needs a branch: push <name> [<onto>], or start the loop with -Branch <name>' }
            if (-not (Test-PushTarget $name)) { throw "never pushes to: $name" }
            if (-not (Test-BranchName $onto) -or $onto -eq $name) { throw "bad onto: $onto" }
            if ($onto -eq 'main') {
                $open = @(Invoke-GH pr list --repo $GhRepo --base main --state open --limit 200 --json 'number,headRefName' --jq '.[] | "\(.number) \(.headRefName)"' | ForEach-Object { , ($_ -split ' ', 2) })
                if (-not ($open | Where-Object { $_[1] -in $name, "$name-ci" })) {
                    $batch = $open | Where-Object { $_[1] -like 'batch-*' } | Select-Object -First 1
                    if ($batch) { throw "PR #$($batch[0]) ($($batch[1])) is still open: re-send to that branch or close it first. Nothing pushed." }
                }
            }
            $src = Get-Incoming "refs/remotes/hub/claude/$name" "hub-$name"
            $null = Get-OriginCommit $onto
            $touched = Get-ChangedFiles "origin/$onto" $src
            if (-not $touched) { throw "nothing to push: hub/claude/$name has the same tree as origin/$onto" }
            "$(@($touched).Count) file(s) differ from origin/$onto"
            # A branch cut off an older origin still holds the old content of files origin changed since. A tree diff would
            # put those back, so refuse and list them. No merge-base (a branch off claude/main) means nothing to compare.
            $mb = Get-MergeBase "origin/$onto" $src
            if ($mb) {
                $originMoved = [Collections.Generic.HashSet[string]]::new([string[]]@(Get-ChangedFiles $mb "origin/$onto"))
                $branchMoved = [Collections.Generic.HashSet[string]]::new([string[]]@(Get-ChangedFiles $mb $src))
                # origin squashes claude/main, so a branch cut from claude/main shares only the old seed with origin: what
                # origin holds that differs from the branch's fork point on claude/main is the only stale content.
                $fp = Get-MergeBase (Get-Incoming 'refs/heads/claude/main' 'shared-claude-main') $src
                if ($fp) {
                    $originMoved.IntersectWith([string[]]@(Get-ChangedFiles $fp "origin/$onto"))
                }
                $reverts = @($touched | Where-Object { $originMoved.Contains($_) -and -not $branchMoved.Contains($_) })
                if ($reverts) {
                    throw "refusing: $name would put $($reverts.Count) file(s) back to the content they had at the merge-base $($mb.Substring(0, 9)), undoing what origin/$onto changed since. Rebase the branch onto origin/$onto and send it again: $($reverts -join ', ')"
                }
            }
            $ciEdit = @($touched | Where-Object { $_ -match $gatesMain })
            $other = @($touched | Where-Object { $_ -notmatch $gatesMain })
            if ($ciEdit -and $other) {
                "mixed branch: splitting into $name-ci (CI files) and $name (the rest)"
                $sets = @(
                    @{ name = "$name-ci"; src = (Split-Branch $src $onto $ciEdit (Get-SplitMessage $src $onto $ciEdit 'ci: ')); ci = $ciEdit }
                    @{ name = $name; src = (Split-Branch $src $onto $other (Get-SplitMessage $src $onto $other '')); ci = $null }
                )
            } else {
                # With nothing split, the one commit has $src's tree, which is origin/$onto plus every touched file.
                $msg = (G log -1 --format=%B $src '--' | Out-String).Trim()
                $sets = @(@{ name = $name; src = (G commit-tree "${src}^{tree}" -p "origin/$onto" -m $msg); ci = $ciEdit })
            }
            # Every set is checked, and its PR text read, before the first one is pushed.
            $todo = @()
            foreach ($s in $sets) {
                if ($s.ci -and (Test-CiCovered $s.src $onto $s.ci)) {
                    "  skipped  $($s.name): origin/$onto or an open PR already has these CI files ($($s.ci -join ', '))"
                    continue
                }
                $s.pr = if ($onto -eq 'main') { Invoke-GH pr list --repo $GhRepo --head $s.name --state open --json number --jq '.[0].number' }
                if ($onto -eq 'main' -and -not $s.pr) {
                    $s.text = Get-PrText $req @(G log --format=%s "origin/$onto..$($s.src)")
                }
                $todo += $s
            }
            foreach ($s in $todo) {
                Invoke-Deliver $s.src $onto $s.name
                $bundleReq = if ($s.name -eq $name) { $req } else { "$req-ci" }
                Write-Bundle $bundleReq "refs/heads/$($s.name)" "refs/remotes/origin/$($s.name)"
                if ($onto -eq 'main') {
                    if (-not $s.pr) {
                        $prTitle = if ($s.ci) { "$($s.text.title) (workflow files)" } else { $s.text.title }
                        Invoke-GH pr create --repo $GhRepo --base main --head $s.name --title $prTitle --body $s.text.body
                    }
                    if ($s.ci) {
                        # An open PR for this branch may have had auto-merge on before the push added CI files.
                        if ($s.pr -and (Invoke-GH pr view $s.name --repo $GhRepo --json autoMergeRequest --jq '.autoMergeRequest != null') -eq 'true') {
                            Invoke-GH pr merge $s.name --repo $GhRepo --disable-auto
                            "  pr       $($s.name): auto-merge was on, turned off"
                        }
                        "  pr       $($s.name): NO auto-merge, it edits CI ($($s.ci -join ', ')), the operator merges it by hand"
                    } else {
                        Invoke-GH pr merge $s.name --repo $GhRepo --auto --merge
                        "  pr       $($s.name): auto-merge on, merges when ci-ok passes"
                    }
                }
            }
        }
        'release' {
            Assert-Arg $parts '^\d+\.\d+\.\d+$' 'release version' $line
            $ver = $parts[1]; $tag = "v$ver"
            $sha = Get-OriginCommit main
            & git -C $clone ls-remote --exit-code --tags origin "refs/tags/$tag" *>$null
            if ($LASTEXITCODE -eq 0) { throw "tag $tag already exists on origin" }
            if ($LASTEXITCODE -ne 2) { throw "could not check remote tag $tag ($LASTEXITCODE)" }
            $have = ((G show "${sha}:internal/cli/VERSION") -join '').Trim()
            if ($have -ne $ver) { throw "VERSION at origin/main's head $sha is $have, not $ver" }
            $ci = [int]((Invoke-GH run list --repo $GhRepo --workflow ci.yml --branch main --commit $sha --json 'status,conclusion' --jq '[.[] | select(.status == "completed" and .conclusion == "success")] | length') -join '').Trim()
            if ($ci -lt 1) { throw "ci.yml has no successful run on origin/main for ${sha}: not releasing" }
            # No AI vendor or attribution in the tag message, the release notes or any commit message since the last tag.
            G fetch --quiet --tags origin
            $last = & git -C $clone describe --tags --abbrev=0 --match 'v[0-9]*' $sha 2>$null
            if (-not (Test-Git cat-file -e "${sha}:changelog/$tag.md")) { throw "no changelog/$tag.md at origin/main's head ${sha}: not releasing" }
            $texts = @(@{ what = 'tag message'; text = $tag }; @{ what = "changelog/$tag.md"; text = ((G show "${sha}:changelog/$tag.md") -join "`n") })
            foreach ($c in @(G log --format='%H' $(if ($last) { "$last..$sha" } else { $sha }) '--')) {
                $texts += @{ what = "commit $($c.Substring(0, 9))"; text = ((G show -s --format=%B $c '--') -join "`n") }
            }
            $hits = Get-AttributionHits $texts
            if ($hits) { throw "not releasing $tag, attribution is named in:`n  $(@($hits) -join "`n  ")" }
            # Signed with the key of whoever runs the loop, so the tag shows Verified. The tag push fires release.yml's own tag trigger.
            $signFlag = if ($script:TagFlag) { $script:TagFlag } else { '-s' }
            G tag $signFlag $tag $sha -m $tag
            G push --quiet origin "refs/tags/$tag"
            "tagged $tag at $sha and pushed it; the tag push starts the release workflow"
        }
        'delete-tag' {
            Assert-Arg $parts '^v\d+\.\d+\.\d+$' 'tag, want vMAJOR.MINOR.PATCH' $line
            $tag = $parts[1]
            # Only an orphan tag goes: one with a release is what people download, so it is refused, and so is any answer but "not found".
            $seen = (& gh release view $tag --repo $GhRepo --json tagName 2>&1 | Out-String)
            if ($LASTEXITCODE -eq 0) { throw "a GitHub release exists for $tag, not deleting its tag" }
            if ($seen -notmatch 'release not found') { throw "could not tell whether a release exists for ${tag}: $seen" }
            Invoke-GH api -X DELETE "repos/$GhRepo/git/refs/tags/$tag"
            "deleted tag $tag on origin (no release existed for it)"
        }
        'rerun' {
            Assert-Arg $parts '^\d+$' 'run id' $line
            Invoke-Rerun $parts[1] -FailedOnly
            "rerun started for $($parts[1]) (failed and cancelled jobs only)"
            Show-Run $parts[1]
        }
        'dispatch' {
            if ($parts.Count -ne 2 -or -not (Test-BranchName $parts[1])) { throw "bad dispatch ref: $line" }
            $latest = { Invoke-GH run list --repo $GhRepo --workflow ci.yml --event workflow_dispatch --branch $parts[1] --limit 1 --json databaseId --jq '.[0].databaseId' }
            $before = & $latest
            Invoke-GH workflow run ci.yml --repo $GhRepo --ref $parts[1] -f name=loop-dispatch
            "dispatched ci on $($parts[1])"
            # The new run takes a moment to be listed: until it is, the newest one is the previous dispatch.
            foreach ($try in 1..5) {
                $new = & $latest
                if ($new -and $new -ne $before) { Show-Run $new; break }
                Start-Sleep -Seconds 2
            }
            if (-not $new -or $new -eq $before) { "  run      not listed yet, look for it on $($parts[1])" }
        }
        'retrigger' {
            if ($parts.Count -ne 2 -or -not (Test-BranchName $parts[1])) { throw "bad retrigger: $line" }
            $tip = Get-RemoteTip $parts[1]
            if (-not $tip) { throw "no origin branch $($parts[1])" }
            $runs = Invoke-GH run list --repo $GhRepo --branch $parts[1] --limit 30 `
                --json 'databaseId,workflowName,event,status,conclusion,createdAt,headSha' | ConvertFrom-Json
            # a run for an older tip is stale: a push just after it would otherwise rerun the old commit
            $runs = $runs | Where-Object headSha -eq $tip
            if (-not $runs) { "no runs yet for $($parts[1]) tip $($tip.Substring(0, 9))"; break }
            $latest = $runs | Sort-Object createdAt -Descending | Group-Object workflowName, event | ForEach-Object { $_.Group[0] }
            $redo = $latest | Where-Object { $_.status -eq 'completed' -and $_.conclusion -ne 'success' }
            if (-not $redo) { "nothing to retrigger on $($parts[1]): every latest run is green or still running"; break }
            foreach ($r in $redo) {
                Invoke-Rerun $r.databaseId
                "reran $($r.workflowName) ($($r.event)) run $($r.databaseId), was $($r.conclusion)"
                Show-Run $r.databaseId
            }
        }
        'fetch' {
            $b = if ($parts.Count -ge 2) { $parts[1] } else { 'main' }
            if ($parts.Count -gt 2 -or -not (Test-BranchName $b)) { throw "bad fetch: $line" }
            $null = Get-OriginCommit $b
            "origin/$b at $(G log -1 --format='%h %s' "origin/$b" '--')"
            Write-Bundle $req "refs/remotes/origin/$b" "refs/remotes/origin/$b"
            if ($b -eq 'main') { "  then     fast-forward claude/main to origin/main yourself: this loop never moves it" }
        }
        'close' {
            Assert-Arg $parts '^\d+$' 'close' $line
            $pr = Invoke-GH pr view $parts[1] --repo $GhRepo --json 'state,headRefName,title' | ConvertFrom-Json
            if (-not $pr) { throw "no PR $($parts[1])" }
            if ($pr.state -ne 'OPEN') { throw "PR $($parts[1]) is $($pr.state), not open" }
            if ($pr.headRefName -match '^(main|master|dev-.*)$') { throw "never closes a PR from $($pr.headRefName)" }
            Invoke-GH pr close $parts[1] --repo $GhRepo
            "closed PR $($parts[1]) ($($pr.headRefName)): $($pr.title)"
        }
        default { throw "unknown request: $line" }
    }
}

foreach ($d in $inbox, $out, $done) { New-Item -ItemType Directory -Force $d | Out-Null }
# One loop at a time: a second one would take requests the first is about to run.
$mutex = [Threading.Mutex]::new($false, "Global\atrium-ci-loop-$me")
$locked = $false
$startDir = Get-Location
try {
    try { $locked = $mutex.WaitOne(0) } catch [Threading.AbandonedMutexException] { $locked = $true }
    if (-not $locked) { throw 'the ci-loop is already running' }
    Initialize-Private
    $script:landExe = Get-LandExe
    Set-Location $clone
    [Environment]::CurrentDirectory = $clone

    Write-Host "push-to-github: watching $inbox, working in $clone, landing with $script:landExe (Ctrl+C to stop)" -ForegroundColor Cyan
    $stuck = @{}
    while ($true) {
        foreach ($d in $inbox, $out, $done) { if ((Get-Item -LiteralPath $d -Force).LinkType) { throw "$d is a link: the queue must be plain directories" } }
        foreach ($f in Get-ChildItem $inbox -File -Filter *.txt | Sort-Object LastWriteTime) {
            if (((Get-Date) - $f.LastWriteTime).TotalSeconds -lt 2) { continue }
            try {
                Move-Item -LiteralPath $f.FullName (Join-Path $done $f.Name) -Force
            } catch {
                if (-not $stuck[$f.Name]) { Write-Host "cannot claim $($f.Name), not running it: $_" -ForegroundColor Red; $stuck[$f.Name] = 1 }
                continue
            }
            $log = Join-Path $out "$($f.BaseName).log"
            if (Get-Item -LiteralPath $log -Force -ErrorAction SilentlyContinue) { Remove-Item -LiteralPath $log -Force }
            $sw = [Diagnostics.Stopwatch]::StartNew()
            try {
                $line = (Read-Small (Join-Path $done $f.Name) 1024).Trim()
                Write-Host "$(Get-Date -Format HH:mm:ss) [$($f.Name)] $line" -ForegroundColor Yellow
                Invoke-Request $f.BaseName $line *>&1 | Tee-Object -FilePath $log
                Add-Content $log 'EXIT 0'
                Write-Host ("ok in {0:n1}s, log {1}" -f $sw.Elapsed.TotalSeconds, $log) -ForegroundColor Green
            } catch {
                Add-Content $log "ERROR $_"
                Add-Content $log 'EXIT 1'
                Write-Host "failed: $_" -ForegroundColor Red
            }
            $prText = Join-Path $inbox "$($f.BaseName).pr.md"
            if (Test-Path -LiteralPath $prText) { Move-Item -LiteralPath $prText (Join-Path $done "$($f.BaseName).pr.md") -Force }
        }
        Start-Sleep -Seconds $PollSeconds
    }
} finally {
    Set-Location $startDir
    [Environment]::CurrentDirectory = $startDir.ProviderPath
    if ($locked) { $mutex.ReleaseMutex() }
    $mutex.Dispose()
}
