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
