<#
.SYNOPSIS
  Move docs/ into one folder per owning area, and fix every path that names a moved doc.

.DESCRIPTION
  One-off, kept so the move can be re-run on a newer base: other branches keep adding and editing docs, so the move
  is made again at the moment it lands rather than rebased through conflicts.

  1. `git mv` every file in $Map. A file already moved is skipped. A file missing is reported.
  2. Report any top-level docs/*.md that is neither in $Map nor in $Keep, so a doc added since is placed by hand.
  3. Rewrite `docs/<old>` to `docs/<new>` in every tracked text file, except the ones in $Skip. The match refuses a
     preceding letter, digit, dot, slash or dash, so `website/docs/x.md` and `../docs/x.md` are never touched.
  4. Print every line of the dotagents CLAUDE.md files ($ClaudeDir) that names a moved path, for clint to change by
     hand. Every worktree's CLAUDE.md is a symlink to one of those, and none is ever edited from here.

  Nothing is committed. Read `git status` and the report, then commit.

.PARAMETER Root
  The worktree to act on. Defaults to the repository this script is in.

.PARAMETER DryRun
  Report what would move and change, and change nothing.
#>
param(
  [string]$Root = (Split-Path -Parent $PSScriptRoot),
  # Where the atrium CLAUDE.md files really live. Every worktree links to these.
  [string]$ClaudeDir = 'D:/git/github/dovholuknf/dotagents/github/dovholuknf/atrium',
  [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
Set-Location $Root

# old path under docs/ -> new path under docs/. The rule: a design not yet built, research and spikes are rnd's.
# A built design or a reference for built code belongs to the area that owns that code. Shared entry points stay
# at the top ($Keep).
$Map = [ordered]@{
  # rnd: designs not yet built, research, spikes, reads of other tools
  'agent-lineage-design.md'         = 'rnd/agent-lineage-design.md'
  'ai-platform-fit.md'              = 'rnd/ai-platform-fit.md'
  'background-hold-design.md'       = 'rnd/background-hold-design.md'
  'bb.md'                           = 'rnd/bb.md'
  'charon.md'                       = 'rnd/charon.md'
  'competitors.md'                  = 'rnd/competitors.md'
  'event-sink-stage3-design.md'     = 'rnd/event-sink-stage3-design.md'
  'everywhere-card-design.md'       = 'rnd/everywhere-card-design.md'
  'federation-design.md'            = 'rnd/federation-design.md'
  'federation-design-v2.md'         = 'rnd/federation-design-v2.md'
  'forum-implementation.md'         = 'rnd/forum-implementation.md'
  'hook-coverage-spike.md'          = 'rnd/hook-coverage-spike.md'
  'keepalive-marked-spec.md'        = 'rnd/keepalive-marked-spec.md'
  'keepalive-policy-design.md'      = 'rnd/keepalive-policy-design.md'
  'multi-room-design.md'            = 'rnd/multi-room-design.md'
  'multi-tenant-decision.md'        = 'rnd/multi-tenant-decision.md'
  'orca.md'                         = 'rnd/orca.md'
  'owed-report-design.md'           = 'rnd/owed-report-design.md'
  'postgres-probe.md'               = 'rnd/postgres-probe.md'
  'process-registry-design.md'      = 'rnd/process-registry-design.md'
  'restart-idle-spec.md'            = 'rnd/restart-idle-spec.md'
  'review-tab-design.md'            = 'rnd/review-tab-design.md'
  'spike-mcp-gateway.md'            = 'rnd/spike-mcp-gateway.md'
  'spike-nested-subagents.md'       = 'rnd/spike-nested-subagents.md'
  'state-of-the-art.md'             = 'rnd/state-of-the-art.md'
  'transparent-rooms.md'            = 'rnd/transparent-rooms.md'
  'turn-end-spike.md'               = 'rnd/turn-end-spike.md'
  # runtime: the daemon, the store, hooks, messaging, launching, keep-alive, intake
  'a2a-reliability-design.md'       = 'runtime/a2a-reliability-design.md'
  'activity-design.md'              = 'runtime/activity-design.md'
  'agent-messaging.md'              = 'runtime/agent-messaging.md'
  'auto-mode.md'                    = 'runtime/auto-mode.md'
  'cache-keepalive-design.md'       = 'runtime/cache-keepalive-design.md'
  'codex-update-design.md'          = 'runtime/codex-update-design.md'
  'file-transfer-design.md'         = 'runtime/file-transfer-design.md'
  'hooks.md'                        = 'runtime/hooks.md'
  'intake-design.md'                = 'runtime/intake-design.md'
  'keepalive-fork-args-design.md'   = 'runtime/keepalive-fork-args-design.md'
  'launch-options-design.md'        = 'runtime/launch-options-design.md'
  'lean-workers-design.md'          = 'runtime/lean-workers-design.md'
  'other-runners.md'                = 'runtime/other-runners.md'
  'providers-design.md'             = 'runtime/providers-design.md'
  'reload-design.md'                = 'runtime/reload-design.md'
  'restart-wake.md'                 = 'runtime/restart-wake.md'
  'runner-setup-design.md'          = 'runtime/runner-setup-design.md'
  'say-lifecycle-design.md'         = 'runtime/say-lifecycle-design.md'
  'scm-design.md'                   = 'runtime/scm-design.md'
  'seen-design.md'                  = 'runtime/seen-design.md'
  'statusline-telemetry.md'         = 'runtime/statusline-telemetry.md'
  'unexpected-exit-wake.md'         = 'runtime/unexpected-exit-wake.md'
  'work-ledger-design.md'           = 'runtime/work-ledger-design.md'
  'work-ledger-plan.md'             = 'runtime/work-ledger-plan.md'
  'worktree-gone-design.md'         = 'runtime/worktree-gone-design.md'
  # terminal: the pty, attach, input and resize
  'input-lag-logging.md'            = 'terminal/input-lag-logging.md'
  'multi-pane-input-design.md'      = 'terminal/multi-pane-input-design.md'
  'supervision-design.md'           = 'terminal/supervision-design.md'
  'terminal-resize-decoupling-design.md' = 'terminal/terminal-resize-decoupling-design.md'
  'typing-race.md'                  = 'terminal/typing-race.md'
  # ui: the board
  'board-repaint.md'                = 'ui/board-repaint.md'
  'css-nits.md'                     = 'ui/css-nits.md'
  'preview-design.md'               = 'ui/preview-design.md'
  'switcher-design.md'              = 'ui/switcher-design.md'
  # fabric: the hub, rooms, overlays, reaching atrium from elsewhere
  'audit-design.md'                 = 'fabric/audit-design.md'
  'card-room-routing.md'            = 'fabric/card-room-routing.md'
  'cross-room-say-design.md'        = 'fabric/cross-room-say-design.md'
  'hub-restart-gate.md'             = 'fabric/hub-restart-gate.md'
  'hub-room-plan.md'                = 'fabric/hub-room-plan.md'
  'hub-room-requirements.md'        = 'fabric/hub-room-requirements.md'
  'room-requirements-design.md'     = 'fabric/room-requirements-design.md'
  'one-atrium-cutover.md'           = 'fabric/one-atrium-cutover.md'
  'one-atrium-plan.md'              = 'fabric/one-atrium-plan.md'
  'overlays.md'                     = 'fabric/overlays.md'
  'pin-order-rooms-design.md'       = 'fabric/pin-order-rooms-design.md'
  'remote-launch.md'                = 'fabric/remote-launch.md'
  'runner-scoping-design.md'        = 'fabric/runner-scoping-design.md'
  'ziti-zrok-flow-design.md'        = 'fabric/ziti-zrok-flow-design.md'
  'ziti-zrok-flow-design.mercurius-synopsis.md' = 'fabric/ziti-zrok-flow-design.mercurius-synopsis.md'
  'zrok-share-500.md'               = 'fabric/zrok-share-500.md'
  # review: code review, the director and its memory
  'peer-review-brief.md'            = 'review/peer-review-brief.md'
  'research/item16-notes.md'        = 'review/item16-notes.md'
  'review-memory-design.md'         = 'review/review-memory-design.md'
  'review-pr-start-design.md'       = 'review/review-pr-start-design.md'
  # release: packaging, landing work, and the one test plan that is its own file
  'merge-hygiene.md'                = 'release/merge-hygiene.md'
  'packaging.md'                    = 'release/packaging.md'
  'test-plan-z-providers.md'        = 'release/test-plan-z-providers.md'
  # orchestrator: its boot, its wrap-up, and what it keeps between waves
  'cold-start.md'                   = 'orchestrator/cold-start.md'
  'dispatch-queue.md'               = 'orchestrator/dispatch-queue.md'
  'overnight-brief.md'              = 'orchestrator/overnight-brief.md'
  'status.md'                       = 'orchestrator/status.md'
  'wrapup.md'                       = 'orchestrator/wrapup.md'
}

# Shared entry points, and the two paths every brief, DIRECTOR.md and CLAUDE.md name (test-plan.md, changes/).
$Keep = @('README.md', 'architecture-v2.md', 'atrium-for-agents.md', 'backlog.md', 'backlog-2.md', 'decisions.md',
  'decisions-log.md', 'how-atrium-works.md', 'test-plan.md', 'user-guide.md')

# Never rewritten. CHANGELOG.md is history, written against the paths of its day, and only @merge writes it. The
# scrollback captures are byte-exact test input. website/ has a docs/ of its own (website/docs/hooks.md,
# overlays.md, intake.md), and build-docs.ps1 runs from inside website/, so its `docs/hooks.md` is THAT file.
$Skip = @('CHANGELOG.md', 'scripts/build-docs.ps1')
$SkipPrefix = @('internal/daemon/testdata/', 'website/')
$TextExt = @('.md', '.go', '.js', '.css', '.html', '.sh', '.ps1', '.yaml', '.yml', '.json', '.txt', '.toml', '.cjs', '.mjs')
$TextName = @('Makefile', '.gitignore')

# 1. move
$moved = @()
foreach ($old in $Map.Keys) {
  $from = "docs/$old"
  $to = "docs/$($Map[$old])"
  if (-not (git ls-files -- $from)) {
    if (git ls-files -- $to) { continue }
    Write-Host "MISSING  $from"
    continue
  }
  $moved += $old
  if ($DryRun) { Write-Host "mv  $from -> $to"; continue }
  New-Item -ItemType Directory -Force (Split-Path -Parent $to) | Out-Null
  git mv -- $from $to
  if ($LASTEXITCODE -ne 0) { throw "git mv $from failed" }
}

# 2. anything at the top of docs/ nobody placed
foreach ($f in (git ls-files -- 'docs/*.md')) {
  $rel = $f.Substring(5)
  if ($rel.Contains('/')) { continue }
  if ($Keep -contains $rel) { continue }
  if ($Map.Contains($rel) -and -not $DryRun) { continue }
  if ($Map.Contains($rel)) { continue }
  Write-Host "UNPLACED docs/$rel (add it to `$Map or `$Keep)"
}

# 3. rewrite paths. Every mapping, not only the ones moved this run, so a re-run also fixes a link added since.
$patterns = foreach ($old in $Map.Keys) {
  [pscustomobject]@{
    Rx  = [regex]('(?<![A-Za-z0-9_./-])docs/' + [regex]::Escape($old) + '(?![A-Za-z0-9_-])')
    New = "docs/$($Map[$old])"
  }
}
$changed = @()
foreach ($f in (git ls-files)) {
  if ($Skip -contains $f) { continue }
  if ($SkipPrefix | Where-Object { $f.StartsWith($_) }) { continue }
  $name = Split-Path -Leaf $f
  if ($name -eq 'CLAUDE.md') { continue }
  $ext = [IO.Path]::GetExtension($f)
  if (-not ($TextExt -contains $ext -or $TextName -contains $name)) { continue }
  if (-not (Test-Path -LiteralPath $f -PathType Leaf)) { continue }
  $bytes = [IO.File]::ReadAllBytes((Resolve-Path -LiteralPath $f))
  $bom = $bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF
  $text = [Text.Encoding]::UTF8.GetString($bytes, $(if ($bom) { 3 } else { 0 }), $bytes.Length - $(if ($bom) { 3 } else { 0 }))
  if (-not $text.Contains('docs/')) { continue }
  $new = $text
  foreach ($p in $patterns) { $new = $p.Rx.Replace($new, $p.New) }
  if ($new -ceq $text) { continue }
  $changed += $f
  if ($DryRun) { continue }
  $out = [Text.Encoding]::UTF8.GetBytes($new)
  if ($bom) { $out = [byte[]](0xEF, 0xBB, 0xBF) + $out }
  [IO.File]::WriteAllBytes((Resolve-Path -LiteralPath $f), $out)
}
Write-Host ""
Write-Host "moved $($moved.Count) files, rewrote paths in $($changed.Count) files"
$changed | ForEach-Object { Write-Host "  $_" }

# 4. CLAUDE.md lines naming a moved path, read from dotagents through the links
Write-Host ""
Write-Host "CLAUDE.md lines naming a moved path (not edited, for clint):"
$claudes = Get-ChildItem -Path $ClaudeDir -Recurse -Filter CLAUDE.md -ErrorAction SilentlyContinue |
  Where-Object { $_.FullName -notmatch '[\\/]\.mercurius[\\/]' }
foreach ($c in $claudes) {
  $target = $c.FullName.Substring($ClaudeDir.Length).TrimStart('\', '/')
  $n = 0
  foreach ($line in (Get-Content -LiteralPath $c.FullName -Encoding utf8)) {
    $n++
    foreach ($p in $patterns) {
      if ($p.Rx.IsMatch($line)) { Write-Host "  ${target}:${n}: $($line.Trim())  ->  $($p.New)" }
    }
  }
}
