# Where atrium assumes it is building atrium

Backlog item `r-spike-atrium-specificity`, high. Status: spike, no code.

clint, 2026-10-19 18:14: "i've been USING atrium to BUILD atrium. so i have been allowing a lot of 'atrium' to leak into
atrium's design ... i need all the 'atrium specificity'
sorted out and we need to come up with generic alternatives instead of hardcoding shit into atrium."

This is the inventory, read at `1e1371d6b`. Every entry says where (file:line), what it hardcodes, who it hurts when
the repository on the hub is not `dovholuknf/atrium`, and what replaces it. Entries are grouped:

- **Keep**: genuinely atrium's own (its module path, its own CI, its own build), or harmless on another project.
- **Make configurable**: a policy every project needs, with atrium's value today as the default.
- **Turn into an event**: a hardcoded notification or a step derived from atrium's own deploy, which becomes a hub
  event kind that cards subscribe to, through the mechanism of `docs/fabric/land-webhook-design.md`.
- **Delete**: a name, an example or a seed that should not be in the product at all.

Not covered: `scripts/` that only atrium's own repository runs (`ci.sh`, `test-touched.sh`, `fold-changes.ps1`, the
board suites). They are atrium's project tooling, not the product. What matters is where the PRODUCT names them.

## The two mechanisms everything below lands on

### A. The repository's own config file

Today a repository on the hub is a row: name, checkout, branch (`internal/gitsync/repos.go:30`). Everything else about
how a project works is a constant in Go. The proposal is one tracked file in the project, read by the hub off the tip of
the integration branch, the same way the hub already reads `docs/known-failing-tests.md` and `changelog/` off a commit:

```yaml
# .atrium/project.yaml
integration_branch: claude/main      # where merges land. default claude/main, else the forge's default branch
work_prefix: claude/                 # branches agents make, and the only ones the hub takes a push or a merge of
gate: scripts/test-touched.sh        # what the merge queue and the shepherd run on a candidate, with --base <sha>
known_failing: docs/known-failing-tests.md
landed_record: changelog/{dept}/{date}-{id}.md   # what proves an item landed. empty: the item's sha on the branch
change_notes: docs/changes/{id}.md   # the doc-debt check reads it. empty: no check
worker_rules: .atrium/worker-rules.md            # appended to every worker brief, in place of the atrium lines
ships: [internal/, cmd/, scripts/, go.mod, go.sum]  # paths that need a deploy. empty: nothing deploys
```

Why a file in the repository and not a hub setting: these describe the project and change with it. A branch that renames
its gate script changes the file in the same commit, and a room that clones the project reads what the hub reads.
Operator taste (colors, the commit identity, VAPID contact) stays a hub setting, because it is about the person, not the
project.

A project without the file gets atrium's values for the branch names and nothing for the rest: no gate means the merge
queue merges on a clean rebase and says it ran no test, no landed record means landed is read off the sha. The Go
constants stay as the defaults, renamed `default...`, so atrium's own behavior is unchanged with no file.

### B. Hub events, as `docs/fabric/land-webhook-design.md` built them

E1 of that design is built: `POST /_hub/events` (`internal/link/eventhook.go:105`), a closed kind list with one kind
`land` (`internal/link/eventhook.go:49`), `event_sub` per card, the shared delivery loop, `atrium_subscribe`,
`atrium subscribe` (`internal/cli/subscribe.go`), and `scripts/atrium-event.ps1`/`.sh`. Its section 7 already names the
next kinds: `deploy`, `ci`, `merge`.

That is the general mechanism for everything in "Turn into an event" below. Two extensions it needs:

1. **Kinds the project owns.** The closed list stays closed for kinds the hub reasons about (`land`, `deploy`, `merge`,
   `ci`). A project's own happenings (a release cut, a nightly that broke) go through one generic kind, `note`, with
   `repo`, `branch`, `topic` (a slug, filterable) and a bounded `text`, so a project needs no atrium release to tell
   its cards something.
2. **Events that move an item's steps.** The item lifecycle's `deployed` and `pushed` steps are derived today from
   atrium's own hub and room builds and from the hub's `main` (`internal/link/lifecycle_git.go:96`). For any other
   project those steps should be set by a `deploy` event (`repo`, `sha`, `target`) and a `push` event, which the
   project's own deploy and release scripts send. The hub records the event, and the step reads "the newest
   `deploy` event for this repo whose sha contains the item's sha".

## Keep

**K1. The Go module path.** `github.com/dovholuknf/atrium/...` in every import (for example `internal/api/api.go:24`)
and the version ldflag (`internal/cli/version.go:29`). The product's own name. No project sees it.

**K2. The fingerprint of room code.** `internal/base/codeid/codeid.go:51` skips `internal/api/web`, `docs`,
`build.claude` when it hashes the room binary's source. It is about atrium's own binary, not the project being built.

**K3. atrium's own CI.** `.github/workflows/ci.yml:83` (`ghcr.io/dovholuknf/atrium-ci:1`), `scripts/ci.sh`,
`scripts/ci-notify.ps1`/`.sh`. These are atrium's repository tooling. `ci-notify` already takes `--url` and a secret,
so another project can copy it. The CI hook it feeds (`internal/link/cihook.go`) is generic: routes are hub settings.

**K4. The deploy verdict for atrium's own hub.** `internal/deployready/deployready.go:9` (`Atrium-Verdict:` trailers),
`:129` (`needsPrefix`, `needsFile`, `testOnly`, `hubOnly`). This answers "is the tip safe to put on THIS hub", which is
only ever atrium's code. Keep it, but stop using it for any other repository's item steps (see E2).

**K5. The `build.claude/` path in atrium's own scripts.** `scripts/ci.sh:549`, `scripts/atrium-autostart.ps1:144`,
`internal/base/testdiag/testdiag.go:10`. atrium's build output. The worker rule that names it is
configurable, see C6.

**K6. The rule globs in comments.** `internal/store/rules.go:22`, `:76` use `build.claude` as an example in a comment.
Harmless.

**K7. The `claude/` namespace as a DEFAULT.** The work prefix and integration branch keep `claude/` and `claude/main`
as defaults (C1, C2). A project that never writes the config file still works as atrium does today.

## Make configurable

**C1. The integration branch, `claude/main`.** The largest one. It is a constant in
`internal/gitsync/sync.go:25` (`IntegrationBranch`), `internal/daemon/cull.go:64` (`DefaultCullInto`), and a literal in
`internal/shepherd/steps.go:15`, `internal/link/review.go:52`, `internal/daemon/changes.go:184`,
`internal/api/closehub.go:73`, `internal/api/close.go:322`, `internal/gitsync/repos.go:39` (which REFUSES any branch
but `claude/main` or `main`), and in user-facing text: `internal/cli/merge.go:29`, `internal/link/merge_mcp.go:21`,
`internal/cli/atrium_rooms_git.go:81`, `internal/cli/launch.go:112`, `internal/cli/control_peers.go:654`, the board's
`internal/api/web/js/merge.js:70` and `internal/api/web/js/items.js:49`.
Hurts: a project whose agents integrate on `develop` or `next` cannot use the merge queue, the cull, review's base, the
changes view or the hub's repo mirror without renaming its branches to atrium's.
Instead: `integration_branch` in `.atrium/project.yaml`, read once per repository by the hub and handed to the room
with the repository row. `IntegrationBranch` becomes `gitsync.Integration(repo)`, the constant becomes its default.
The board and CLI text say "the integration branch" or print the name they were given. `ValidateRepo` keeps its rule
(integration branch or `main`, never a department branch), with the configured name in place of the literal.

**C2. The work branch prefix, `claude/`.** Agents' branches are `claude/<slug>`
(`internal/daemon/cardhome.go:226`, `internal/shepherd/steps.go:68` and `:86`, `internal/link/merge.go:886`), and the
hub only takes pushes, merges and close rows for that prefix: `internal/gitsync/hubpush.go:180`,
`internal/gitsync/mergerun.go:518`, `internal/hubstore/merge.go:118`, `internal/gitsync/receive_rules.go:240`,
`internal/daemon/closehub.go:23`, `internal/daemon/interview.go:52`, `internal/gitsync/branchstatus.go:183`. The land
scrub strips it (`internal/base/land/scrub.go:16`). `internal/link/changerequest.go:680` trims it for a title.
Hurts: a team whose convention is `agent/` or `bot/<user>/` gets refused pushes and no merge queue.
Instead: `work_prefix` in the project file, default `claude/`. One helper, `gitsync.IsWorkBranch(repo, name)`, replaces
the nine `HasPrefix` checks.

**C3. The protected-branch lists.** Five copies: `internal/link/work_record.go:39`, `internal/worktrees/audit.go:52`,
`internal/gitsync/served.go:56`, `internal/api/closeadopt.go:62`, `internal/daemon/cull.go:440`, plus
`scripts/branch-sweep.ps1:25` (`claude/orch`, `claude/qa`, `claude/rnd`, `claude/orch-premerge`, which are atrium's own
department branches).
Hurts: a project's `release/*` or `develop` is not protected from the cull and the close sweep.
Instead: one function, `gitsync.Protected(repo, name)`, which is `main`, `master`, `HEAD`, `hub-main`, the integration
branch, plus a `protected` list in the project file. The department branches in `branch-sweep.ps1` move to atrium's own
project file.

**C4. The gate script, `scripts/test-touched.sh`.** `internal/gitsync/mergerun.go:76` (`MergeScript`), run with
`bash ... --base` (`:385`), and refused when the candidate lacks it (`:372`). The shepherd runs it too
(`internal/shepherd/stage.go:127`). Text names it: `internal/cli/merge.go:31`, `internal/link/merge.go:940`,
`internal/link/merge_mcp.go:21`, `internal/shepherd/http.go:184`.
Hurts: every other project. The merge queue refuses a candidate with no `scripts/test-touched.sh`, so the queue is
unusable without copying atrium's script.
Instead: `gate` in the project file: a path, run with `bash` when it ends `.sh`, else directly, with `--base <sha>`
appended. Empty means no gate, and the report says "no gate configured" so a green is never read as tested.

**C5. The red rules: 20ms budget rerun and the known-failing file.** `internal/gitsync/mergetext.go:63`
(`docs/known-failing-tests.md`), `:66` (the budget-miss regexp is atrium's harness wording), `:70` (`RedRerun`),
`internal/shepherd/stage.go:142`.
Hurts: harmless when the file is absent, but the rerun regexp matches only atrium's harness, and a project that keeps
its known failures elsewhere cannot point at them.
Instead: `known_failing` (path) and `rerun_on` (one regexp) in the project file, atrium's values as its own file's
values, not as code defaults. The `--- FAIL: (\w+)` reader stays: it is Go's own output, and other languages get the
"no test named" answer, which already means "not known".

**C6. The worker rules typed into every brief.** `internal/daemon/lean.go:164` (`leanSystemPrompt`) and `:177`
(`superleanSystemPrompt`) name `claude/main`, `build.claude/`, the two known-failing tests by name and
`docs/known-failing-tests.md`. `internal/shepherd/http.go:179` (`DefaultRules`) names Clint Dovholuk as the committer,
`gofmt -l internal/ cmd/`, `scripts/check-test-globals.go`, the 10ms budget, `docs/changes/<id>.md` and
`scripts/test-touched.sh`.
Hurts: every worker on another project is told to put Go builds in `build.claude/`, to ignore two atrium tests, and
to commit as clint. A Python project's worker reads Go rules.
Instead: split each prompt in two. The product half stays in code and names no path: launched by atrium, end with
done or blocked, commit on your own branch never on the integration branch (by its configured name), one-line
subjects, never restart or deploy, one foreground test call. The project half is `worker_rules` from the project
file, appended verbatim and bounded (4 KiB). atrium's own repository moves its lines (`build.claude/`, the known
failures, gofmt, the budgets) into its `.atrium/worker-rules.md`. The known-failing line is generated from
`known_failing`, never typed.

**C7. The landed record, `changelog/<dept>/<date>-<id>.md`.** An item has landed when the integration branch holds its
changelog entry: `internal/gitsync/landfacts.go:102`, `internal/gitsync/landindex.go:248`,
`internal/base/itemgate/itemgate.go:151`, `internal/deployready/queue.go:59`, `internal/link/deps.go:289` and `:320`,
`internal/link/deps_mcp.go:21`. `internal/gitsync/branchstatus.go:128` treats `changelog/` as docs.
Hurts: a project that keeps no per-item changelog file never sees an item land, so `landed`, `deployed`, `pushed`,
item dependencies and the pool's gates never move.
Instead: `landed_record` in the project file, a pattern with `{dept}`, `{date}` and `{id}`. Empty means landed is "the
item's recorded sha, or its branch tip, is an ancestor of the integration branch", which `ItemFacts.Merged`
(`internal/gitsync/flowfacts.go:27`) already computes. The land index keeps its one `git log` per tip, over the
configured directory.

**C8. The change notes, `docs/changes/<id>.md`.** The doc-debt check reads it off the landing commit
(`internal/link/docdebt_landed.go:72`) and looks for a `Docs:` line that `scripts/fold-changes.ps1` defines.
Hurts: every feature on another project is flagged "docs: not said" because the file does not exist. The check is
silent on a missing file (`docdebt_landed.go:21`), so it is quiet today, but the docs department and its `d-<n>` items
are atrium's own process.
Instead: `change_notes` in the project file. Empty turns the check off for that repository. The `docs` department and
`Docs:` convention stay atrium's (see D3).

**C9. Departments and their id prefixes.** `internal/hubstore/backlog_follow.go:16` maps `fabric`, `runtime`, `ui`,
`release`, `rnd`, `terminal`, `review`, `docs` to `f`, `r`, `u`, `m`, `rnd`, `t`, `review`, `d`. The doc-debt meter
reads department `docs` (`internal/hubstore/docdebtmeter.go:120`).
Hurts: another team's departments (`backend`, `infra`) get no id prefix, so an item filed without an id is refused or
takes no prefix. The departments themselves are free text, which is right.
Instead: a hub setting `backlog.departments`, a list of `{dept, prefix}`, seeded once with today's map, edited on the
board. The backlog is on the hub, not in one repository, so it is a hub setting and not the project file.

**C10. Owner roles: `orchestrator`, `director`, `merger`, `tester`.** `internal/daemon/item_hub.go:57` and
`internal/hubstore/flow.go:38`. These are atrium's roles, not the project's. They are not atrium-the-project specific,
they are atrium-the-product's model of a team. Keep the names, and keep this entry here only so the open question about
renaming roles (question 6) has a place.

**C11. The commit identity on landing.** `internal/base/land/identity.go:10` (`AuthorEmail`, clint's GitHub noreply
address, and `CheckEmail` refuses any other), and `internal/shepherd/steps.go:148` writes `user.name Clint Dovholuk`
and the same address into every shepherd worktree.
Hurts: anyone else who lands through atrium has their push refused or their commits authored as clint.
Instead: a hub setting `land.identity` (name and email), with no default: landing refuses with "set land.identity" until
it is set, and the shepherd uses the same setting. The GH007 check stays and compares against the setting.

**C12. The land scrub rules.** `internal/base/land/scrub.go:16` to `:30` rewrite `claude`, `Claude Code`, `CLAUDE.md`,
`build.claude`, `claudeconf`, `CLAUDEVM` in commit messages, and `Banned` (`:37`) refuses a landing with `claude` or
`anthropic` in it.
Hurts: this is clint's policy for what reaches GitHub from atrium. Another project may want its commits to say who
wrote them, and a repository whose own code has `claude` in a path cannot land at all.
Instead: a per-repository hub setting `land.scrub`, on by default only for repositories that turn it on. The rule list
stays in code as the one named profile `agent-neutral`, with `claudeconf`, `CLAUDEVM`, `claudeAuthCheck` (atrium's own
identifiers) moved to atrium's project file as extra pairs.

**C13. The `deployed` step's scope.** `internal/link/lifecycle_git.go:70` (`Scope`) asks `deployready.Needs` and
`RoomSide`, which are atrium's own paths (`internal/`, `cmd/`, `scripts/`, `internal/hubstore/` as hub-only). The step
then waits on atrium's hub and every room to run a build containing the sha (`:105`).
Hurts: an item on another project waits forever on "deployed: waiting on hub, sg3, ..." because atrium's rooms will
never run that project's code.
Instead: for atrium's own repository, unchanged. For any other: `ships` in the project file decides whether a deploy
is needed at all (empty: `deployed` is skipped with "this project does not deploy through atrium"), and when it is,
the step is set by a `deploy` event (E2).

## Turn into an event

**E1. The land notice.** Done for the hub side: the land script sends `land` through `scripts/atrium-event.ps1`
instead of `atrium tell`. Left: the script itself (`scripts/land-claude-main.ps1`, the human's own, untracked) still
names `dovholuknf/atrium` and `claude/main`. Instead: it reads both from the project file. This is the template every
other entry here follows.

**E2. `deployed`, from a `deploy` event.** `internal/link/lifecycle_git.go:105` to `:140` derive `deployed` from builds
the hub has SEEN on itself and on rooms. Instead: kind `deploy` (`repo`, `sha`, `target`, `outcome`), sent by the
project's deploy script. atrium's own `scripts/live/atrium-deploy.ps1` sends it too, which also replaces its
`Send-CardSay` to `-ReportTo` (`scripts/live/atrium-deploy.ps1:52`, `:94`, `scripts/live/live-common.ps1:101`), the same
move the land script made. For atrium's repository the hub can keep the build check as a second source until the event
is proven, then retire it.

**E3. `pushed`, and `pushed-by-clint`.** `internal/gitsync/flowfacts.go:28` (`PushedByClint`, "main, the mirror of
origin/main that only the operator moves"), `internal/hubstore/flow.go:45` (stage `pushed-by-clint`),
`internal/hubstore/schema.go:564` (a `CHECK` in a shipped migration), `internal/link/flow_git.go:114`, and the item step
`pushed` (`internal/hubstore/workfact.go:28`, board text `internal/api/web/js/items.js:49` "origin/main").
Hurts: the name, and the assumption that the operator pushes by hand to the forge's `main`. A project that releases by
tag, or whose CI pushes, never reaches `pushed`.
Instead: kind `push` (`repo`, `branch`, `sha`), sent by whoever pushes (the land script, a release script, the forge's
own webhook through the CI hook as kind `ci`). The step reads the event, and the git check of `main` stays as atrium's
default source. The stage name `pushed-by-clint` is in a shipped migration's `CHECK`, so it cannot be edited: a new
migration rebuilds `flow_item` with `pushed`, or the code maps the label at the edge and leaves the stored value.

**E4. The merge queue's outcome.** `internal/link/merge.go:349`, `:807`, `:835`, `:859`, `:967` raise a board
`backlogEvent("merge", ...)`, which only the board hears. Instead: also publish kind `merge` (`repo`, `branch`, `n`,
`state`, `sha`), so the card that asked for the merge, or any card subscribed to the repository, hears it without
polling `atrium merge --list`. Named in the land-webhook design's section 7.

**E5. CI for one repository and branch.** The CI hook routes by hand-written rules (`internal/link/cihook.go:265`). Its
section 7 item: publish each recorded run as kind `ci` too, so a card subscribes to CI for its own project without the
human writing a route.

**E6. The "ask clint" in tool text.** `internal/cli/control_own.go:55` and `internal/api/cardoverlay.go:503` tell an
agent to "stop and ask clint to run" the tunneler. Instead: "ask the operator", and where the operator should be told
at once, a `note` event with topic `needs-admin`, which the operator's card subscribes to. The rest of the text stays.

## Delete

**D1. The seeded color table.** `internal/base/cardcolors/cardcolors.go:31` to about `:50` seed clint's own repositories
(`dovholuknf/atrium`, `openziti/*`, `netfoundry/*`) with themes on first read. Another user gets clint's repositories in
their settings. Delete the seed. clint's table is one setting write on his hub, which the migration can do once if the
setting is unset and the hub already holds `github/dovholuknf/atrium` (so only clint's hub keeps it).

**D2. The VAPID default contact.** `internal/link/push.go:53` (`https://github.com/dovholuknf/atrium`). The push
service sees it as the sender's contact. Instead: no default, and web push asks for the contact on first enable. Or the
hub's own URL, which identifies the sender better than atrium's repository does.

**D3. Examples that name atrium's repository.** `internal/hubstore/flow.go:31`, `internal/hubstore/merge.go:117`,
`internal/link/flow_mcp.go:35`, `internal/cli/atrium_rooms_git.go:204`, `internal/cli/subscribe.go:105`, the settings
placeholder `internal/api/web/index.html:2061`, `internal/gitsync/repos.go:15`. Replace with `github.com/owner/repo`.
Comments that use atrium's path as an example
(`internal/api/sessions.go:61`, `internal/api/web/js/terminal-list.js:672`)
can stay: they are not shown.

**D4. Board mocks.** `internal/api/web/js/items-mock.js:67`, `:92`, `:106`, `:110` hold atrium's own items and facts
(`scripts/test-touched.sh passed`). They are test data, shown only in mock mode. Replace the facts with ones built from
the configured gate and branch, or leave them, since they never reach a real board. Lowest priority.

**D5. `hub-main`.** `internal/gitsync/sync.go:227` keeps `hub-main` "for worktrees and briefs that name it". It is a
second name for the integration branch on a room. Once C1 is in, nothing generic needs it. Delete after the briefs and
scripts that name it are gone.

## How the land-webhook design generalizes

The land event is the shape every entry in "Turn into an event" takes:

1. A script the project owns (land, deploy, release, CI) knows something happened. It posts one bounded event to the
   hub with `scripts/atrium-event.ps1`. It never fails because the hub is down.
2. The hub parses it by kind into fields, never copies the body, records it, and fans it out to `event_sub` rows.
3. A card hears only what it subscribed to, per card, surviving restarts, gone when the card goes.

Two things this spike adds to it:

- **Events are facts, not only notices.** `deploy` and `push` events are read by the item lifecycle as the source of
  `deployed` and `pushed`. That turns the two steps that today only make sense for atrium into steps every project can
  move, with no new mechanism, only a reader of `hub_event`.
- **The `note` kind** lets a project tell its cards something atrium has no parser for, bounded and filtered by
  `topic`, so the closed kind list stays closed without becoming the bottleneck.

What events should NOT replace: the project file. A gate script, a branch name or a landed-record pattern is a fact
about the project that the hub needs before anything happens. An event is something that happened.

## Recommended order

1. **C1, C2, C3 together: the branch names.** Biggest blast radius, and every later step names "the integration
   branch". Add `.atrium/project.yaml` with `integration_branch`, `work_prefix`, `protected`, the reader on the hub,
   the field on the repo row the room gets, and the three helpers. Text changes ride along. atrium's own behavior is
   unchanged with no file.
2. **C4, C5: the gate and the red rules.** The merge queue is the first thing another project hits and it refuses
   outright. Small once (1) exists.
3. **C11, C12: identity and scrub.** Small, and today they make landing someone else's work impossible or wrong.
4. **C6: the worker rules split.** Needs (1) for the branch name and (2) for the known-failing line. atrium's own lines
   move into its project file in the same change, so atrium's workers read the same text.
5. **D1, D2, D3.** Cheap deletes, any time. Bundle with (3).
6. **C7, C8, C13 with E2, E3: the item lifecycle off atrium's conventions.** The landed record, the change notes, and
   `deployed` and `pushed` from events. Needs the `deploy` and `push` kinds (land-webhook E3), and the `flow_item`
   migration question (question 4).
7. **E4, E5, E6 and the `note` kind.** Notices only, nothing depends on them.
8. **C9, D4, D5.** Departments as a setting, the mocks, `hub-main`.

## Open questions for clint

1. Is `.atrium/project.yaml` (tracked in the project, read off the integration branch tip) the right home for project
   policy, or would you rather keep it all as per-repository hub settings on the board?
2. Should a project without the file get atrium's defaults (`claude/main`, `claude/`, no gate), or should the hub refuse
   to run the merge queue on it until the file exists?
3. The land scrub (C12): is "no `claude` and no `anthropic` in anything that reaches the forge" your rule for every
   repository you land, or only for atrium? It decides whether `agent-neutral` is on by default.
4. `pushed-by-clint` is in a shipped migration's `CHECK`. A new migration that rebuilds `flow_item` with `pushed`, or
   keep the stored value and only rename it at the edge?
5. Should `deployed` and `pushed` on atrium's own items move to events too (E2, E3), with the git and build checks
   retired, or keep both sources for atrium forever?
6. Roles (`orchestrator`, `director`, `merger`, `tester`) and departments (`fabric`, `rnd`, `docs`, ...): product
   vocabulary to keep, or does another team need to name its own?
7. The worker rules (C6): one free-form `worker-rules.md`, or a few structured fields (build dir, test command, known
   failures) the hub renders into the brief?
8. The seeded colors (D1): delete outright, or keep them as a one-time write on your hub only?
9. Should `land-claude-main.ps1` become a tracked, generic `scripts/land.ps1` that reads the project file, so another
   project can land the same way, or stay your own untracked script?
