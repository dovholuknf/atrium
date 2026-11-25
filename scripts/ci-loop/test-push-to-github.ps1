# Runs the push, release and split logic of push-to-github.ps1 against a local bare repo as origin and a stub gh.
$ErrorActionPreference = 'Stop'
$work = Join-Path $PSScriptRoot '../../build.claude/test-push'
if (Test-Path $work) { Remove-Item -Recurse -Force $work }
New-Item -ItemType Directory $work | Out-Null
$work = (Resolve-Path $work).Path

$ast = [Management.Automation.Language.Parser]::ParseFile((Resolve-Path "$PSScriptRoot/push-to-github.ps1").Path, [ref]$null, [ref]$null)
$want = 'G', 'Invoke-GH', 'Get-ChangedFiles', 'Get-OriginCommit', 'Invoke-Rerun', 'Test-BranchName', 'Test-PushTarget', 'Get-Incoming', 'Write-Bundle', 'Invoke-Deliver', 'Get-Pathspec', 'Split-Branch','Get-SplitMessage', 'Test-CiCovered', 'Test-Attribution', 'Get-PrText', 'Invoke-Request', 'Test-Git', 'Get-MergeBase', 'Get-RemoteTip', 'Read-Small', 'Assert-Arg', 'Get-AttributionHits'
foreach ($f in $ast.FindAll({ $args[0] -is [Management.Automation.Language.FunctionDefinitionAst] -and $args[0].Name -in $want }, $true)) {
    Invoke-Expression $f.Extent.Text
}
$gatesMain = ($ast.FindAll({ $args[0] -is [Management.Automation.Language.AssignmentStatementAst] -and $args[0].Left.Extent.Text -eq '$gatesMain' }, $true))[0].Right.Expression.Value

function Run-Git { & git @args; if ($LASTEXITCODE -ne 0) { throw "git $args failed" } }
function Assert($ok, $what) { if (-not $ok) { throw "FAIL: $what" }; "ok   $what" }

$GhRepo = 'fake/repo'
$inbox = Join-Path $work 'inbox'
New-Item -ItemType Directory $inbox | Out-Null
function Write-Pr([string]$req, [string]$text = "Faster builds for the board`n`nChanges:`n- The board builds quicker because the test step no longer repeats itself.") {
    Set-Content -LiteralPath (Join-Path $inbox "$req.pr.md") $text
}
$Branch = ''
$priv = Join-Path $work 'priv'
$out = Join-Path $work 'out'
$origin = Join-Path $work 'origin.git'
$shared = Join-Path $work 'shared'
$clone = Join-Path $work 'clone'
New-Item -ItemType Directory $priv, $out | Out-Null
$nohooks = Join-Path $work 'nohooks'
New-Item -ItemType Directory $nohooks | Out-Null
Run-Git init --quiet --bare -b main $origin
Run-Git -C $origin config core.hooksPath $nohooks
Run-Git init --quiet -b claude/seed $shared
Run-Git -C $shared config core.hooksPath $nohooks
Run-Git -C $shared config user.name t
Run-Git -C $shared config user.email t@example.com
Run-Git -C $shared config commit.gpgsign false
foreach ($p in 'README.md', '.github/workflows/ci.yml', 'scripts/ci.sh', 'src/a.go') {
    New-Item -Force -ItemType File (Join-Path $shared $p) -Value "base $p`n" | Out-Null
}
New-Item -ItemType Directory (Join-Path $shared 'docs/backlog') | Out-Null
foreach ($n in 1..2500) { Set-Content (Join-Path $shared ('docs/backlog/item-with-a-long-name-for-the-argv-limit-{0:d5}.md' -f $n)) "i$n" }
Run-Git -C $shared add -A
Run-Git -C $shared commit --quiet -m base
Run-Git -C $shared remote add origin $origin
Run-Git -C $shared push --quiet origin claude/seed:main
Run-Git -C $shared update-ref refs/heads/claude/main HEAD
Run-Git clone --quiet $origin $clone
Run-Git -C $clone config core.hooksPath $nohooks
Run-Git -C $clone config user.name t
Run-Git -C $clone config user.email t@example.com
Run-Git -C $clone config commit.gpgsign false

function New-Hub([string]$name, [hashtable]$files, [string]$from = 'claude/seed') {
    Run-Git -C $shared switch --quiet -C "claude/w-$name" $from
    foreach ($p in $files.Keys) {
        $full = Join-Path $shared $p
        if ($files[$p] -eq $null) { Run-Git -C $shared rm --quiet $p } else { New-Item -Force -ItemType File $full -Value $files[$p] | Out-Null }
    }
    Run-Git -C $shared add -A
    Run-Git -C $shared commit --quiet -m "work on $name"
    Run-Git -C $shared update-ref "refs/remotes/hub/claude/$name" HEAD
    Run-Git -C $shared switch --quiet claude/seed
}

$landExe = Join-Path $work 'land.ps1'
Set-Content $landExe '$global:LASTEXITCODE = 0'
$script:landExe = $landExe
$script:gh = [Collections.Generic.List[string]]::new()
function Assert-Private {}
$script:ghArgv = [Collections.Generic.List[object]]::new()
$script:ghOpen = ''
$script:ghBatch = @()
$script:ghReleases = @('v0.0.6')
$script:ghCi = 1
$script:ghHeads = @{}
$script:ghAuto = ''
$script:TagFlag = '-a'
function gh {
    $script:gh.Add("$args"); $script:ghArgv.Add($args); $global:LASTEXITCODE = 0
    if ("$args" -like 'pr list*--head *') { $script:ghHeads[$args[[array]::IndexOf($args, '--head') + 1]] }
    elseif ("$args" -like 'pr view*') { $script:ghAuto }
    elseif ("$args" -like 'pr list*--json headRefName*') { $script:ghOpen }
    elseif ("$args" -like 'pr list*--base main*') { $script:ghBatch }
    elseif ("$args" -like 'run list*') { $script:ghCi }
    elseif ("$args" -like 'release view*') {
        $line = "$args"
        if ($script:ghReleases | Where-Object { $line -like "release view $_ *" }) { '{"tagName":"x"}' } else { $global:LASTEXITCODE = 1; 'release not found' }
    }
}

function Origin-Files($b) { @(Run-Git -C $origin diff --name-only main $b) }

New-Hub 'mixed' @{ '.github/workflows/ci.yml' = "changed`n"; 'scripts/ci.sh' = $null; 'src/a.go' = "changed`n"; 'src/new.go' = "new`n" }
$script:gh.Clear()
Write-Pr 'r1'
Invoke-Request 'r1' 'push mixed' | Out-Null
Assert ((Origin-Files 'mixed-ci') -join ',' -eq '.github/workflows/ci.yml,scripts/ci.sh') 'mixed: -ci branch holds only the CI files'
Assert ((Origin-Files 'mixed') -join ',' -eq 'src/a.go,src/new.go') 'mixed: main branch holds only the other files'
Assert ((Run-Git -C $origin merge-base main mixed-ci) -eq (Run-Git -C $origin rev-parse main)) 'mixed: -ci is based on main'
Assert (@($script:gh | Where-Object { $_ -like 'pr create*--head mixed-ci *' }).Count -eq 1) 'mixed: PR for -ci'
Assert (@($script:gh | Where-Object { $_ -like 'pr create*--head mixed *' }).Count -eq 1) 'mixed: PR for the rest'
Assert (@($script:gh | Where-Object { $_ -like 'pr merge*' }) -join '|' -eq 'pr merge mixed --repo fake/repo --auto --merge') 'mixed: auto-merge only on the rest'
Assert ((Run-Git -C $origin log -1 --format=%s mixed-ci) -eq 'ci: work on mixed') 'mixed: -ci title is built from the CI commits'

Run-Git -C $shared switch --quiet -C claude/w-titles claude/seed
foreach ($t in @(@('src/a.go', 'rest first'), @('scripts/ci.sh', 'tune ci'), @('src/new.go', 'rest last'))) {
    New-Item -Force -ItemType File (Join-Path $shared $t[0]) -Value "$($t[1])`n" | Out-Null
    Run-Git -C $shared add -A
    Run-Git -C $shared commit --quiet -m $t[1]
}
Run-Git -C $shared update-ref refs/remotes/hub/claude/titles HEAD
Run-Git -C $shared switch --quiet claude/seed
Write-Pr 'r1b'
Invoke-Request 'r1b' 'push titles' | Out-Null
Assert ((Run-Git -C $origin log -1 --format=%s titles-ci) -eq 'ci: tune ci') 'titles: -ci never takes the tip subject'
Assert ((Run-Git -C $origin log -1 --format=%s titles) -eq 'rest last, rest first') 'titles: the rest never takes a CI subject'

Run-Git -C $shared switch --quiet -C claude/w-many claude/seed
Run-Git -C $shared rm --quiet -r docs/backlog
New-Item -Force -ItemType File (Join-Path $shared 'src/a.go') -Value "many`n" | Out-Null
New-Item -Force -ItemType File (Join-Path $shared 'scripts/ci.sh') -Value "many`n" | Out-Null
Run-Git -C $shared add -A
Run-Git -C $shared commit --quiet -m 'work on many'
Run-Git -C $shared update-ref refs/remotes/hub/claude/many HEAD
Run-Git -C $shared switch --quiet claude/seed
Write-Pr 'r1c'
Invoke-Request 'r1c' 'push many' | Out-Null
Assert ((Origin-Files 'many-ci') -join ',' -eq 'scripts/ci.sh') 'many files: -ci holds only the CI file'
Assert (@(Origin-Files 'many').Count -eq 2501) 'many files: the rest holds every deletion and the edit'

New-Hub 'plain' @{ 'src/a.go' = "plain`n" }
$script:gh.Clear()
Write-Pr 'r2'
Invoke-Request 'r2' 'push plain' | Out-Null
Assert ((Origin-Files 'plain') -join ',' -eq 'src/a.go') 'plain: one branch'
Assert (-not (Run-Git -C $origin branch --list 'plain-ci')) 'plain: no -ci branch'
Assert (@($script:gh | Where-Object { $_ -like 'pr merge plain *--auto*' }).Count -eq 1) 'plain: auto-merge'

New-Hub 'cionly' @{ 'scripts/ci.sh' = "changed`n" }
$script:gh.Clear()
Write-Pr 'r3'
Invoke-Request 'r3' 'push cionly' | Out-Null
Assert (-not (Run-Git -C $origin branch --list 'cionly-ci')) 'ci-only: no split'
Assert (@($script:gh | Where-Object { $_ -like 'pr create*--head cionly *' }).Count -eq 1) 'ci-only: one PR'
Assert (@($script:gh | Where-Object { $_ -like 'pr merge*' }).Count -eq 0) 'ci-only: no auto-merge'

New-Hub 'cionly2' @{ 'scripts/ci.sh' = "changed`n" }
$script:gh.Clear()
$script:ghOpen = 'cionly'
Write-Pr 'r3b'
Invoke-Request 'r3b' 'push cionly2' | Out-Null
$script:ghOpen = ''
Assert (-not (Run-Git -C $origin branch --list 'cionly2')) 'ci-only duplicate of an open PR: nothing pushed'
Assert (@($script:gh | Where-Object { $_ -like 'pr create*' }).Count -eq 0) 'ci-only duplicate of an open PR: no PR'

# origin main takes claude/main as one squash commit, so their merge base stays at the old seed.
Run-Git -C $shared switch --quiet -C claude/main claude/seed
foreach ($v in 'one', 'two') {
    New-Item -Force -ItemType File (Join-Path $shared 'src/a.go') -Value "$v`n" | Out-Null
    New-Item -Force -ItemType File (Join-Path $shared "src/$v.go") -Value "$v`n" | Out-Null
    Run-Git -C $shared add -A
    Run-Git -C $shared commit --quiet -m $v
}
Run-Git -C $shared switch --quiet -C claude/squash claude/seed
Run-Git -C $shared checkout claude/main -- .
Run-Git -C $shared commit --quiet -m 'squash of one and two'
Run-Git -C $shared push --quiet --force origin claude/squash:main
Run-Git -C $shared switch --quiet claude/seed

New-Hub 'sqmixed' @{ '.github/workflows/ci.yml' = "sq`n"; 'src/c.go' = "c`n" } 'claude/main'
$script:gh.Clear()
Write-Pr 'r6'
Invoke-Request 'r6' 'push sqmixed' | Out-Null
Assert ((Origin-Files 'sqmixed-ci') -join ',' -eq '.github/workflows/ci.yml') 'squashed main, mixed: -ci holds only the CI file'
Assert ((Origin-Files 'sqmixed') -join ',' -eq 'src/c.go') 'squashed main, mixed: the rest holds only the new file'

New-Hub 'sqplain' @{ 'src/c.go' = "c`n" } 'claude/main'
$script:gh.Clear()
Write-Pr 'r7'
Invoke-Request 'r7' 'push sqplain' | Out-Null
Assert ((Origin-Files 'sqplain') -join ',' -eq 'src/c.go') 'squashed main, plain: one commit with only the new file'
Assert ((Run-Git -C $origin rev-list --count main..sqplain) -eq '1') 'squashed main, plain: one commit over main'

# f-037: a rename left both files on origin.
New-Hub 'sqrename' @{ 'src/one.go' = $null; 'src/one_integration.go' = "one`n"; '.github/workflows/ci.yml' = "rn`n" } 'claude/main'
Write-Pr 'r8'
Invoke-Request 'r8' 'push sqrename' | Out-Null
Assert ((Run-Git -C $origin ls-tree -r --name-only sqrename src) -join ',' -eq 'src/a.go,src/one_integration.go,src/two.go') 'rename, pushed: the old path is gone'

Run-Git -C $shared switch --quiet -C claude/rename claude/main
Run-Git -C $shared mv src/two.go src/two_integration.go
Run-Git -C $shared commit --quiet -m 'rename two'
Run-Git -C $shared switch --quiet claude/main
Run-Git -C $shared merge --quiet --no-ff -m 'Merge claude/rename' claude/rename
Run-Git -C $shared switch --quiet claude/seed
Invoke-Request 'r9' 'land dev-rename' | Out-Null
Assert ((Run-Git -C $origin ls-tree -r --name-only dev-rename src) -join ',' -eq 'src/a.go,src/one.go,src/two_integration.go') 'rename, landed: the old path is gone'
Assert ((Run-Git -C $origin rev-parse 'dev-rename^{tree}') -eq (Run-Git -C $shared rev-parse 'claude/main^{tree}')) 'land: the tree is claude/main''s'

$bad = try { Invoke-Request 'r5' 'release v0.0.6'; $false } catch { $true }
Assert $bad 'release: refuses a v-prefixed version'

$script:gh.Clear()
Invoke-Request 'r6' 'delete-tag v0.1.0' | Out-Null
Assert ($script:gh -join '|' -eq 'release view v0.1.0 --repo fake/repo --json tagName|api -X DELETE repos/fake/repo/git/refs/tags/v0.1.0') 'delete-tag: an orphan tag is deleted'
$script:gh.Clear()
$bad = try { Invoke-Request 'r7' 'delete-tag v0.0.6'; $false } catch { $true }
Assert ($bad -and @($script:gh | Where-Object { $_ -like 'api*' }).Count -eq 0) 'delete-tag: a tag with a release is refused and nothing is deleted'
foreach ($t in '0.1.0', 'v0.1', 'v0.1.0-rc1', 'main') {
    $bad = try { Invoke-Request 'r8' "delete-tag $t"; $false } catch { $true }
    Assert $bad "delete-tag: refuses $t"
}

# The PR text: refused before anything is pushed when it is missing, names a file or a commit subject, or has no description.
New-Hub 'prtext' @{ 'src/a.go' = "prtext`n" } 'claude/squash'
function Refused-Push([string]$req, [string]$like, [string]$what) {
    $m = try { Invoke-Request $req 'push prtext'; '' } catch { "$_" }
    Assert ($m -like $like -and -not (Run-Git -C $origin branch --list prtext)) $what
}
Refused-Push 'p1' '*no PR text*' 'pr text: refused without the file'
Write-Pr 'p2' "publish-release.sh trim

Changes:
- The release notes no longer break the release."
Refused-Push 'p2' '*names a file*' 'pr text: a title naming a script is refused'
Write-Pr 'p3' "work on prtext

Changes:
- The release notes no longer break the release."
Refused-Push 'p3' '*commit subject*' 'pr text: a commit subject as the title is refused'
Write-Pr 'p4' "Release notes fit GitHub again"
Refused-Push 'p4' '*no description*' 'pr text: no description is refused'
Write-Pr 'p4b' "Release notes fit GitHub again`n`n  `n"
Refused-Push 'p4b' '*no description*' 'pr text: a title-only file with blank lines after it is refused'
$script:gh.Clear()
$fmt = "Release notes fit GitHub again`n`n"
Write-Pr 'p4c' ($fmt + "Features:`n- A long changelog is cut and linked, with a good enough reason.`n`nHow it was checked: by hand, twice.")
Refused-Push 'p4c' '*no other prose*' 'pr text: a How it was checked line is refused'
Write-Pr 'p4d' ($fmt + "A long changelog used to make the release fail, so it is now cut and linked.")
Refused-Push 'p4d' '*no other prose*' 'pr text: a description with no sections is refused'
Write-Pr 'p4e' ($fmt + "- A long changelog is cut and linked, with a good enough reason.")
Refused-Push 'p4e' '*outside*section*' 'pr text: a bullet before any section is refused'
Write-Pr 'p4f' ($fmt + "Features:`n`nBugs fixed:`n- A long changelog is cut and linked, with a good enough reason.")
Refused-Push 'p4f' '*Features*no bullets*' 'pr text: a section with no bullets is refused'
Write-Pr 'p4g' ($fmt + "Changes:`n- " + ((1..501 | ForEach-Object { 'w' }) -join ' '))
Refused-Push 'p4g' '*over 500 words*' 'pr text: a description over 500 words is refused'
Write-Pr 'p5' ($fmt + "Features:`n- A long changelog used to make the release fail, so it is now cut and linked.`n`nBugs fixed:`n- The release no longer fails on a long changelog.")
Invoke-Request 'p5' 'push prtext' | Out-Null
$create = @($script:gh | Where-Object { $_ -like 'pr create*' })
Assert ($create.Count -eq 1 -and $create[0] -like '*--title Release notes fit GitHub again --body *' -and $create[0] -like '*A long changelog used to make the release fail*' -and $create[0] -notlike '*Changes*' -and $create[0] -notlike '*src/a.go*') 'pr text: the title and description are used, the body holds no file list'
New-Hub 'batch-9-n2' @{ 'src/a.go' = "batch`n" } 'claude/squash'
Write-Pr 'p6'
$script:ghBatch = @('77 batch-9-n1')
$m = try { Invoke-Request 'p6' 'push batch-9-n2'; '' } catch { "$_" }
Assert ($m -like '*PR #77 (batch-9-n1) is still open: re-send to that branch or close it first*' -and -not (Run-Git -C $origin branch --list batch-9-n2)) 'batch: a second batch PR is refused and nothing is pushed'
$script:ghBatch = @('77 batch-9-n1', '78 batch-9-n2')
$script:ghArgv.Clear()
Invoke-Request 'p6' 'push batch-9-n2' | Out-Null
$list = @($script:ghArgv | Where-Object { "$($_[0]) $($_[1])" -eq 'pr list' -and $_ -contains '--base' })[0]
$at = [array]::IndexOf($list, '--json')
Assert ($at -ge 0 -and $list[$at + 1] -is [string] -and $list[$at + 1] -ceq 'number,headRefName') 'json fields: one comma value reaches gh as a single argument'
Assert ([bool](Run-Git -C $origin branch --list batch-9-n2)) 'batch: a re-send to the branch that holds the open PR is allowed'
$script:ghBatch = @()
# A name that is a glob is a file name, not a pattern: scripts/c[i].sh must not pull in scripts/ci.sh.
New-Hub 'globs' @{ 'scripts/ci.sh' = "globbed`n"; 'scripts/c[i].sh' = "literal`n" } 'claude/squash'
Write-Pr 'g1'
Invoke-Request 'g1' 'push globs' | Out-Null
Assert ((Origin-Files 'globs-ci') -join ',' -eq 'scripts/ci.sh') 'pathspec: the CI half holds only its own file'
Assert ((Origin-Files 'globs') -join ',' -eq 'scripts/c[i].sh') 'pathspec: a name with glob characters does not match another file'

# Every half is checked before the first is pushed: a missing PR text for the second leaves the first unpushed.
New-Hub 'part' @{ '.github/workflows/ci.yml' = "part`n"; 'src/part.go' = "part`n" } 'claude/squash'
$script:ghHeads['part-ci'] = '5'
$m = try { Invoke-Request 'g2' 'push part'; '' } catch { "$_" }
$script:ghHeads.Clear()
Assert ($m -like '*no PR text*' -and -not (Run-Git -C $origin branch --list 'part*')) 'mixed: a refusal on the second half pushes nothing'

# A CI push to a branch whose open PR has auto-merge on turns it off.
New-Hub 'amerge' @{ 'scripts/ci.sh' = "amerge`n" } 'claude/squash'
$script:ghHeads['amerge'] = '6'
$script:ghAuto = 'true'
$script:gh.Clear()
Invoke-Request 'g3' 'push amerge' | Out-Null
$script:ghHeads.Clear(); $script:ghAuto = ''
Assert (@($script:gh | Where-Object { $_ -like 'pr merge amerge *--disable-auto' }).Count -eq 1) 'auto-merge: switched off for a branch that now edits CI'

# A request the agent wrote is never followed through a link or read past its size limit.
$small = Join-Path $work 'small.txt'
Set-Content $small 'push x'
Assert ((Read-Small $small 1024).Trim() -eq 'push x') 'read: a plain small file is read'
Set-Content $small ('x' * 2000)
$m = try { Read-Small $small 1024; '' } catch { "$_" }
Assert ($m -like '*over 1024 bytes*') 'read: a file over the limit is refused'
$secret = Join-Path $work 'secret.txt'
Set-Content $secret 'push stolen'
$link = Join-Path $work 'link.txt'
New-Item -ItemType HardLink -Path $link -Target $secret | Out-Null
$m = try { Read-Small $link 1024; '' } catch { "$_" }
Assert ($m -like '*HardLink*not reading it*') 'read: a link is refused'
# A branch cut before origin moved README.md would put it back: refused with the file listed, nothing pushed.
New-Hub 'stale' @{ 'src/new2.go' = "n`n" }
function Move-Origin([string]$file, [string]$value, [string]$msg) {
    Run-Git -C $shared fetch --quiet origin main
    Run-Git -C $shared switch --quiet -C claude/adv FETCH_HEAD
    New-Item -Force -ItemType File (Join-Path $shared $file) -Value $value | Out-Null
    Run-Git -C $shared add -A
    Run-Git -C $shared commit --quiet -m $msg
    Run-Git -C $shared push --quiet origin claude/adv:main
    Run-Git -C $shared switch --quiet claude/seed
}
Move-Origin 'README.md' "moved`n" 'origin moves README'
$msg = try { Invoke-Request 'r11' 'push stale'; '' } catch { "$_" }
Assert ($msg -like '*refusing*README.md*' -and $msg -notlike '*new2.go*') 'stale base: refused, and only the reverted file is listed'
Assert (-not (Run-Git -C $origin branch --list stale)) 'stale base: nothing was pushed'

# release: refuses unless every check passes, then tags origin/main's head and pushes only the tag.
function Refused([string]$line, [string]$like, [string]$what) {
    $m = try { Invoke-Request 'rr' $line; '' } catch { "$_" }
    Assert ($m -like $like) $what
}
Move-Origin 'internal/cli/VERSION' "0.2.0`n" 'Version 0.2.0'
Refused 'release 0.2.0' '*no changelog/v0.2.0.md*' 'release: refused without the changelog file'
Move-Origin 'changelog/v0.2.0.md' "notes`n" 'Notes 0.2.0'
$script:ghCi = 0
Refused 'release 0.2.0' '*no successful run*' 'release: refused without a green ci run on the head'
Assert (-not (Run-Git -C $origin tag --list v0.2.0)) 'release: no tag after a refusal'
$script:ghCi = 1
$script:ghHeads = @{}
$script:ghAuto = ''
Refused 'release 0.2.1' '*is 0.2.0, not 0.2.1*' 'release: refused when VERSION is another version'
$script:gh.Clear()
Invoke-Request 'r12' 'release 0.2.0' | Out-Null
Assert ((Run-Git -C $origin rev-parse 'v0.2.0^{commit}') -eq (Run-Git -C $origin rev-parse main)) 'release: the tag is on origin main''s head'
Assert (@($script:gh | Where-Object { $_ -like 'workflow run*' }).Count -eq 0) 'release: no workflow is dispatched, the tag push starts it'
Assert ((Run-Git -C $origin tag -l --format='%(contents)' v0.2.0).Trim() -eq 'v0.2.0') 'release: the tag message is plain'
Refused 'release 0.2.0' '*already exists*' 'release: refused when the tag exists'
Move-Origin 'internal/cli/VERSION' "0.2.2`n" "Version 0.2.2`n`nCo-Authored-By: someone"
Move-Origin 'changelog/v0.2.2.md' "notes`n" 'Notes 0.2.2'
Refused 'release 0.2.2' '*Co-Authored-By*' 'release: refused when a commit since the last tag carries attribution'
Assert (-not (Run-Git -C $origin tag --list v0.2.2)) 'release: no tag after an attribution refusal'
foreach ($ok in 'Route Codex native approvals through atrium', 'claude/main', 'Support claude and codex runners') { Assert (-not (Test-Attribution $ok)) "attribution: a bare product word passes: $ok" }
foreach ($bad in 'Co-Authored-By: Claude <noreply@anthropic.com>', 'Generated with Claude Code', 'generated by an AI', "robot $([char]::ConvertFromUtf32(0x1F916))", 'mail bot@openai.com') { Assert (Test-Attribution $bad) "attribution: real attribution fails: $bad" }

'PASS'
