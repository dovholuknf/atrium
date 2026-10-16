Docs: none, design only

## Design

`docs/fabric/branch-status-design.md` is the design: the four statuses and their rules, where the hub computes them and
when, the API, the repos tab view, what a human does with each status, and a six-phase build. Nothing is built.

## Phase 1: the computation and a read-only API

`internal/gitsync/branchstatus.go` is the one pass of section 4, and `GET /_hub/git/branches` reads its snapshot. Hub
branches only (`claude/*`, not `claude/main`): a branch is `landed` or `orphaned`, with a `kind` of `code` or `docs`.
`open` and `room-only` are in the vocabulary and are not computed until phases 3 and 4. No board change, no migration.

- A commit is carried by patch id, then by subject AND file set (decision 4), and a branch by its changelog entry.
  `how` on a landed branch is the weakest evidence it needed: `ancestry`, `patch-id`, `subject`, `changelog`.
- Main's patch ids are hashed once and extended by the commits main gained. Each branch commit is hashed once. The
  commit to facts map lives in memory (decision 1), so a pass with nothing moved costs a `rev-parse` and a
  `for-each-ref`. A diff past 32 MB halves its batch, and one commit past it has no patch id.
- The snapshot is rebuilt with the repos view (push, init, the 20 s timer) for as long as somebody asks, and only does
  work when a branch tip or main moved. A GET starts no git.
- The query is `repo` (empty means every repository, decision 8), `status` (repeatable), `kind` and `limit`. Counts are
  over every branch and ignore the filters. Rows sort orphaned first, code before docs, oldest first.
- Rule 3 carries a branch as a whole, as the design says, so a commit added after a changelog-landed branch landed is
  not seen as new. A commit of a branch landed by patch id or subject still flips it back (decision 2).
- Each pass logs one `[hub] branches` line with the counts, for the acceptance check against the live mirror.

## Phase 2: the Branches view on the repos tab

A fifth view beside Requests in the repos tab (`js/hubrepos.js`, `css/hubrepos.css`), fed by `GET /_hub/git/branches`.
Read-only: no button acts on a branch. There is no room-only and no live set yet, so a chip for `open` or `room-only`
appears only when the hub counts one (phases 3 and 4).

- One read of every repository the hub holds, on entry and on the refresh button, asked again while the hub is still
  `building`. A repo choice narrows what is drawn and asks nothing.
- A chip per status with its count, over every branch of the chosen repo whatever is folded. Landed is off by default
  and a "show landed N" line unfolds it, with how main carries them (patch-id, subject, changelog, ancestry).
- Each row: the branch name as text, its repo, the status with its `kind` (orphans) or `how` (landed), `ahead` as
  uncarried over commits, the last commit, who pushed it. The orphan kind filter narrows orphans only.
- Per-tab state. The chips, repo and kind live in memory, and the view itself is never written to localStorage or
  followed through the storage event, so another window never lands on Branches because this one did.
- Headless unit `hubReposBranches` in the `hubrepos` bucket, on the demo data in `scripts/hubrepos-fixture.js`. The
  switcher unit now counts five views.

Pictures: `docs/changes/img/f-branch-status-view/` (`branches-before-2000-dark.png` is the tab's Shelf before,
`branches-after-2000-dark.png` and `branches-after-390-paper.png` are the new view).

## Test plan

## @LETTER@. Branch statuses by content

### @LETTER@1. The read-only API

1. On a hub with a rewritten `claude/main`, call `GET /_hub/git/branches?repo=<name>` twice, a few seconds apart.

**Expected:** the first answer says `building`, the second has `computed`, counts and rows. Most branches are `landed`,
and the hub log has one `[hub] branches` line with the same counts.

### @LETTER@2. Filters

1. Call it with `status=orphaned&kind=code`, then with `limit=3`.

**Expected:** only those rows come back, and `counts` is unchanged.

### @LETTER@3. A branch that gains a commit

1. Push a new commit to a landed branch, and call it after the next view rebuild.

**Expected:** the branch is `orphaned` with `uncarried` 1.

### @LETTER@4. The Branches view

1. On a hub, open the repos tab and choose Branches.
2. Turn the landed chip on, narrow to one repository, then open the board in a second window.

**Expected:** orphaned rows show `code` or `docs`, landed is folded until asked and shows its `how`, and the second
window stays on its own view.
