# Doc debt from features

Status: proposed, not built. Design draft, no code. Item `m-doc-debt-from-features`.

## 1. Today

A change ships its own notes: `docs/changes/<item>.md` for the test plan and `changelog/<dept>/<yyyy-mm-dd>-<item>.md`
for the user-facing line (`docs/changes/README.md`, `changelog/README.md`). Neither one is a reference doc. The pages a
reader opens, `README.md`, `FEATURES.md` and `website/docs/*`, are changed by whoever notices they are wrong. Nothing
records that a feature made one wrong, so the debt is invisible until someone audits. `docs/rnd/doc-overhaul-plan.md`
is that audit: `README.md`, the root `AGENTS.md`, `install.md`, `quick-start.md` and `cli.md` still described
`atrium daemon` while the code had moved to `atrium run` and `atrium room`, and the worker had to rediscover every
change from code. The backlog is on the hub (`internal/hubstore/backlog.go`, the `atrium_backlog` tool in
`internal/link`) and already has departments, ids by prefix (`deptPrefix` in `backlog_follow.go`), statuses (open,
held, in-progress, done, dropped) and an age. It has no `docs` department. The nearest finding pattern is the turn-cause
finding on the Terminals tab (`internal/api/turnlimits.go`, `web/js/turn-limits.js`, on `claude/turn-cost-limits` and
not yet on `claude/main`): a server decides, the page draws, a dismiss hides it for the day in `localStorage`. The
nearest report is `docs/rnd/daily-housekeeping-report.md`, a once-a-day section on top of the Terminals tab with
dismissals that return when the facts get worse. It is a design and is not built either.

## 2. Recommended design

### 2.1 What counts as a feature

No new judgment call. A change is a doc-debt feature when it writes a changelog fragment AND it adds, renames,
removes or changes the default of something a reader can name: a verb, a flag, an endpoint, a setting, a tab, a button,
a file format. A fix that makes the code match what the docs already say is not one. A refactor has no fragment and is
not one. The worker decides, and the changelog fragment is the evidence.

### 2.2 Who writes the summary, and when

The worker that built the feature, in its last step, after the changelog fragment and before it reports done. It holds
the context and nobody else will again. It does not write prose for the docs. It writes a short impact record.

### 2.3 What the branch does and what is left over

The branch updates the reference pages it can change without a merge fight: the page that is wrong because of this
change and that no other open branch is likely to touch (a single `website/docs/*.md` page, a package doc). It does NOT
edit shared hot pages, `README.md`, `FEATURES.md` and `docs/how-atrium-works.md`, because every branch that edits them
conflicts, which is the same reason `CHANGELOG.md` froze. So the doc item is only the leftover: the pages the worker
found wrong and did not fix. If the leftover is empty, no item is filed. A feature that fixed its own docs costs
nothing, and that is the incentive.

### 2.4 Where the summary lives

One place holds the state, and the existing changelog stays the only place that holds the user-facing sentence.

- The hub backlog item is the queue. Department `docs`, id `d-<n>` (add `"docs": "d"` to `deptPrefix`), status open
  until a doc worker closes it, filed with `atrium_backlog` so age and status come for free.
- Its body is a fixed block, never prose about the feature:

```
feature: <item id of the feature>
changelog: changelog/<dept>/<date>-<item>.md
shas: <branch tip when filed>, <merge sha once known>
new: <one line each, verb, flag, endpoint, setting or tab>
stale: <path>  <one line, what it says wrong>
stale: <path>  ...
updated: <paths the branch already fixed>
```

- The changelog entry is referenced by path and never copied, so nothing is written twice and `CHANGELOG.md` is not
  touched. `docs/changes/<item>.md` stays a test plan only, and gains one line, `Docs: d-<n>` or `Docs: none, <why>`.
  `scripts/fold-changes.ps1` WARNS for a file that has neither. It does not fail, see question 4.

The `stale:` lines are the machine-readable part. They give the meter its page count and give the doc worker its
starting list.

### 2.5 The meter

Computed on the hub, because the backlog is the hub's. Open `docs` items give three numbers: items, distinct stale
pages, and the age of the oldest. The threshold is 8 open items, OR 3 or more items with the oldest at 10 days. Below
that nothing is shown anywhere. Past it there is one line:

```
 Docs are behind: 9 features, 4 pages, oldest 12 days        [Start doc worker]  [Hide]
```

The button launches a doc worker through the ordinary launch flow with a brief generated from the open items, oldest
first, capped at 10, grouped by page, each with its `new:` and `stale:` lines and its changelog path. The worker closes
each item it finishes with `atrium_backlog`.

### 2.6 Where it shows: a housekeeping line, not its own strip

It belongs in the daily housekeeping report. Reasons: it is a suggestion nobody needs this minute, which is the line the
housekeeping design draws between its report and a growler. It already has the strip, the "as of" time, the zero-lines
means no-strip rule, and the dismissal that returns when the fingerprint changes (D5). A second strip on the Terminals
tab would be the very noise the report was invented to avoid. The fingerprint here is the item count in steps of 4, so a
dismissed line comes back at 12 items, or after 7 days, and not tomorrow. The line sits under the Hub group.

Housekeeping is a design and is not built. So phase 3 below ships the hub endpoint `GET /_hub/doc-debt` and the
housekeeping line only once housekeeping phase 2 has landed. Until then the meter exists as data (`atrium backlog` with
`--dept docs` and the endpoint) and is not drawn. A standalone strip was rejected because it would be thrown away when
housekeeping lands.

### 2.7 Closing

The doc worker closes an item when it has fixed every `stale:` page, and says so in the close note. The hub does not
close anything by itself. From phase 4 it adds a hint on the item, "page changed since": a `git log` of each stale path
since the filed sha, run by the hub on the daily build over its own repository. A hint is shown in the brief and on the
line, it closes nothing, see question 3.

## 3. Rejected alternatives

- **Generate the summary from the diff.** A diff says which files changed and not which page is now wrong. The worker
  knows that, the diff does not.
- **Each feature must edit every page it touches.** Shared pages (`README.md`, `FEATURES.md`) conflict on every merge.
  `CHANGELOG.md` and `docs/test-plan.md` were frozen for exactly this reason.
- **A merge gate that refuses a feature without a doc item.** It would stop merges over a missing line when the fix is
  one backlog filing. A warning from the fold gets the same effect without a blocked batch.
- **Write the summary into `changelog/` or into `docs/changes/`.** The changelog is the reader's sentence and is
  1 to 5 lines. `docs/changes/` is deleted by the fold, so the queue would lose the item at the moment it matters.
  A second copy of either is the duplication the brief rules out.
- **A repo file queue, `docs/debt/<item>.md`.** Age, status, ownership and a launch button already exist on the backlog,
  and a file would need a second reader and a second close rule.
- **Auto-close when the page changes.** A typo fix on `cli.md` would close a feature that left `cli.md` three verbs
  short. Debt would vanish while still owed.
- **A growler or a stand-alone strip.** Section 2.6.
- **Page age by mtime as the meter.** Old does not mean wrong. It would flag every stable page.

## 4. Phases

Each phase is one worker.

1. **d-1-docs-department-and-filing.** Add `docs` with prefix `d` to `deptPrefix` and check the dept name against the
   backlog filters and the `atrium_backlog` tool text. Write the impact block format into the tool's filing help and
   into `docs/changes/README.md` (the "Docs:" line and the "what counts as a feature" rule). The root `AGENTS.md` rule
   "a change ships its own notes" is in the dotagents repository, not here, so the new wording goes to @dotfiles as a
   note, as the doc overhaul did. Done when: a worker can file a `d-` item with the block,
   `atrium backlog ls --dept docs` lists it, and a test covers the prefix and the block's parse.
2. **d-2-fold-warns.** `scripts/fold-changes.ps1` warns, and does not fail, for a `docs/changes/` file with no `Docs:`
   line. Done when: a dry run names the file, a file with `Docs: none, reason` is quiet, and the existing fold tests
   pass.
3. **d-3-meter-and-line.** The hub computes items, pages and oldest from open `docs` items and serves
   `GET /_hub/doc-debt`. Threshold in a hub setting (`doc_debt.items`, default 8, `doc_debt.days`, default 10). The
   housekeeping line with the count-in-4s fingerprint and the Start doc worker button that launches with a generated
   brief. Done when: below threshold nothing renders, past it one line renders, Hide holds until 12 items or 7 days, and
   the button's brief lists at most 10 items grouped by page. Waits on housekeeping phase 2 for the drawing, and the
   endpoint and its tests can land first.
4. **d-4-page-changed-hint.** The hub's daily build adds "page changed since" for each stale path, from `git log` on
   its repository since the item's sha. Done when: touching a stale page shows the hint on the item and its line, and
   the item stays open.

## 5. Open questions for clint

1. A worker finishes a feature, fixes `website/docs/cli.md` in its branch, and leaves `README.md` wrong. Then?
   Recommended: it files ONE doc item that names only `README.md` under `stale:` and `cli.md` under
   `updated:`. A feature that fixed all its pages files nothing.
2. Seven features sit open for 12 days and the meter is under 8 items. Is that too quiet? Recommended: no. The rule is 8
   items, or 3 items with the oldest at 10 days, so this fires on day 10. Tell me if you want it sooner.
3. Somebody fixes a typo on `cli.md`, which a doc item says is three verbs behind. Then? Recommended: the hub
   shows "page changed since" and leaves the item open, and only a doc worker closes it. This departs from your item 3,
   doc items close when the page changes. I did it because a typo would hide real debt. Do you want automatic close
   anyway, with reopen as one click?
4. A worker forgets to file a doc item and the branch merges. Then? Recommended: the fold prints a warning naming the
   item, nothing fails, and the merger's one report to the orchestrator lists the warnings. A refusal gets a round trip
   per forgotten line.
5. The board shows 25 open doc items and you press Start doc worker. Then? Recommended: it takes the 10 oldest, grouped
   by page, and the next press takes the next 10. One worker with 25 items rewrites faster than it can check.
6. This ships and the meter says 0 while the docs are behind, because 365 changelog fragments since 2026-09-29 have no
   doc item. Then? Recommended: no backfill. `docs/rnd/doc-overhaul-plan.md` is the reset, the meter starts at zero
   after its batch 9 lands, and from then on only new features count.
