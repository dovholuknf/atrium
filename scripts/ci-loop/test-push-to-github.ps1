# Runs the push, release and split logic of push-to-github.ps1 against a local bare repo as origin and a stub gh.
$ErrorActionPreference = 'Stop'
$work = Join-Path $PSScriptRoot '../../build.claude/test-push'
if (Test-Path $work) { Remove-Item -Recurse -Force $work }
New-Item -ItemType Directory $work | Out-Null
$work = (Resolve-Path $work).Path

$ast = [Management.Automation.Language.Parser]::ParseFile((Resolve-Path "$PSScriptRoot/push-to-github.ps1").Path, [ref]$null, [ref]$null)
$want = 'G', 'Get-Incoming', 'Write-Bundle', 'Invoke-Deliver', 'Get-Pathspec', 'Split-Branch','Get-SplitMessage', 'Test-CiCovered', 'Invoke-Request'
foreach ($f in $ast.FindAll({ $args[0] -is [Management.Automation.Language.FunctionDefinitionAst] -and $args[0].Name -in $want }, $true)) {
    Invoke-Expression $f.Extent.Text
}
$gatesMain = ($ast.FindAll({ $args[0] -is [Management.Automation.Language.AssignmentStatementAst] -and $args[0].Left.Extent.Text -eq '$gatesMain' }, $true))[0].Right.Expression.Value

function Run-Git { & git @args; if ($LASTEXITCODE -ne 0) { throw "git $args failed" } }
function Assert($ok, $what) { if (-not $ok) { throw "FAIL: $what" }; "ok   $what" }

$GhRepo = 'fake/repo'
$Branch = ''
$priv = Join-Path $work 'priv'
$out = Join-Path $work 'out'
$origin = Join-Path $work 'origin.git'
$shared = Join-Path $work 'shared'
$clone = Join-Path $work 'clone'
New-Item -ItemType Directory $priv, $out | Out-Null
Run-Git init --quiet --bare -b main $origin
Run-Git init --quiet -b claude/seed $shared
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
$script:ghOpen = ''
function gh { $script:gh.Add("$args"); $global:LASTEXITCODE = 0; if ("$args" -like 'pr list*--json headRefName*') { $script:ghOpen } }

function Origin-Files($b) { @(Run-Git -C $origin diff --name-only main $b) }

New-Hub 'mixed' @{ '.github/workflows/ci.yml' = "changed`n"; 'scripts/ci.sh' = $null; 'src/a.go' = "changed`n"; 'src/new.go' = "new`n" }
$script:gh.Clear()
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
Invoke-Request 'r1c' 'push many' | Out-Null
Assert ((Origin-Files 'many-ci') -join ',' -eq 'scripts/ci.sh') 'many files: -ci holds only the CI file'
Assert (@(Origin-Files 'many').Count -eq 2501) 'many files: the rest holds every deletion and the edit'

New-Hub 'plain' @{ 'src/a.go' = "plain`n" }
$script:gh.Clear()
Invoke-Request 'r2' 'push plain' | Out-Null
Assert ((Origin-Files 'plain') -join ',' -eq 'src/a.go') 'plain: one branch'
Assert (-not (Run-Git -C $origin branch --list 'plain-ci')) 'plain: no -ci branch'
Assert (@($script:gh | Where-Object { $_ -like 'pr merge plain *--auto*' }).Count -eq 1) 'plain: auto-merge'

New-Hub 'cionly' @{ 'scripts/ci.sh' = "changed`n" }
$script:gh.Clear()
Invoke-Request 'r3' 'push cionly' | Out-Null
Assert (-not (Run-Git -C $origin branch --list 'cionly-ci')) 'ci-only: no split'
Assert (@($script:gh | Where-Object { $_ -like 'pr create*--head cionly *' }).Count -eq 1) 'ci-only: one PR'
Assert (@($script:gh | Where-Object { $_ -like 'pr merge*' }).Count -eq 0) 'ci-only: no auto-merge'

New-Hub 'cionly2' @{ 'scripts/ci.sh' = "changed`n" }
$script:gh.Clear()
$script:ghOpen = 'cionly'
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
Invoke-Request 'r6' 'push sqmixed' | Out-Null
Assert ((Origin-Files 'sqmixed-ci') -join ',' -eq '.github/workflows/ci.yml') 'squashed main, mixed: -ci holds only the CI file'
Assert ((Origin-Files 'sqmixed') -join ',' -eq 'src/c.go') 'squashed main, mixed: the rest holds only the new file'

New-Hub 'sqplain' @{ 'src/c.go' = "c`n" } 'claude/main'
$script:gh.Clear()
Invoke-Request 'r7' 'push sqplain' | Out-Null
Assert ((Origin-Files 'sqplain') -join ',' -eq 'src/c.go') 'squashed main, plain: one commit with only the new file'
Assert ((Run-Git -C $origin rev-list --count main..sqplain) -eq '1') 'squashed main, plain: one commit over main'

# f-037: a rename left both files on origin.
New-Hub 'sqrename' @{ 'src/one.go' = $null; 'src/one_integration.go' = "one`n"; '.github/workflows/ci.yml' = "rn`n" } 'claude/main'
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

$script:gh.Clear()
Invoke-Request 'r4' 'release 0.0.6' | Out-Null
Assert ($script:gh -join '|' -eq 'workflow run make-release.yml --repo fake/repo --ref main -f version=0.0.6') 'release: dispatches make-release'
$bad = try { Invoke-Request 'r5' 'release v0.0.6'; $false } catch { $true }
Assert $bad 'release: refuses a v-prefixed version'
'PASS'
