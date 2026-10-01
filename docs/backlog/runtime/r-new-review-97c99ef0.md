# Review: r-pr-store 97c99ef0 (@runtime)

Range e502b215..97c99ef0, eight commits on claude/r-pr-store: the pulls-api.md contract (3341e351, 1b552fef),
migrations 0078_pr_review and 0079_pr_recipe, store/prs.go and its tests, the reviews_root setting, the /v1/prs routes
with the stub runner, and the drawer routes (0a71c77a). Read against docs/rnd/pulls-api.md as committed in the range,
and docs/rnd/pulls-view-design.md.

The branch is NOT on claude/main a3fc844f, as the note said. Its base is e502b215, the r-pr-render-2 landing. Nothing
claude/main gained since touches internal/store, internal/api Go code or the schema, so it merges clean. The verdict
range starts at e502b215.

Tests, in a detached worktree at 97c99ef0, with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared: `go vet` of store,
api and daemon is clean. store passes, and api passes except my two scratch tests, which fail as stated below
(D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-97c99ef0/zz_scratch_review_test.go, recipe
test-97c99ef0.ps1 beside it). I did not run the daemon package. Its change is the default reviews root and one
export line, and TestGlobalAutoSurvivesAReopen is the known flake.

## What holds

- Both migrations sit at the end of the slice and tolerate a rerun (`IF NOT EXISTS`, `INSERT OR IGNORE`). The seeded
  panel and brief carry no quote mark, which the splice into a SQL literal needs, and the constant's comment says so.
  CHECKs, not enums, and text times, as the schema rules ask.
- A row is an index over its folder. `findings` and `walk` are read from the folder on every answer and are zero until
  `ready`. The dedupe key is the run folder, so the same PR at the same head is one row, and a failed or aborted one is
  reset and started again, as the contract says.
- Folder names: `folderSeg` reduces each part to `[A-Za-z0-9._-]`, and a name made only of dots becomes `_`, so a
  captured org or repo cannot climb out of the root.
- Abort deletes only a folder `safepath.Contained` puts inside the reviews root, and never the root itself.
- Every drawer route resolves the folder through safepath against the reviews root, then each file inside it. No
  request carries a path, and a symlink out of `findings/` is skipped. The PUT is behind a sha256 precondition and the
  2 MiB limit, and the walk mark replaces one line under a lock and renames a temp file into place.
- The walker launch carries the contract's tags, is refused for a row that is not ready, and returns the live walker
  instead of launching a second one.
- Error bodies are `{error, code}` with the contract's codes, and the routes are on the human listener only.

## Findings

### 1. MEDIUM: a BLOCKING finding is invisible to every pulls route (proven)

The renderer (`render.word`, landed at 58461561 after my nit) names a blocking finding `NN-blocking-<file>-L<n>.txt`
and labels it `BLOCKING`. `findingName` in prsfolder.go accepts only `high|medium|med|low|nit`, and so does
`labelLine` in prsdrawer.go. So a blocking finding is not counted, not in `GET findings`, and cannot be marked. A
review whose only finding is blocking shows 0 findings and every count 0. The most severe kind of finding is the one
the drawer drops. Proof: `TestScratchBlockingCounted` (counts all 0, 0 findings).

Fix: accept `blocking` in both expressions, count it under `high`, and report `sev: "high"` so the contract's four
values hold. If the board should tell blocking apart, add a field: the contract may grow one, never retype one. A
test with a blocking file in the folder.

### 2. LOW: walk.txt is parsed by whitespace, so a spaced file name never takes a mark (proven)

`parseWalkLine` and `replaceWalkLine` split on `strings.Fields`. The renderer can now write a file name holding a
space (git quotes the path, 3a908c38 reads it), and its own `started` reads past the first `.txt` for exactly this
reason. Here such a line never matches: a mark appends a second line instead of replacing the first, and the read
back finds neither, so the finding stays `open`. Proof: `TestScratchSpacedWalk` (two lines, done 0).

Fix: one walk.txt line parser for both packages. Export the renderer's from internal/prreview/render, or move it into
a small shared package, and use it in `parseWalkLine` and `replaceWalkLine`. Rare, since a path with a space is rare
in a PR, but it is the same bug fixed once already.

### 3. LOW: check then act, with no condition on the update

retry, start, abort and the POST reset all read the state, check it, and then write without a condition. Two retry
clicks both pass the check and call `Start` twice. An abort that reads `running` while the runner writes `ready`
deletes a folder that was just finished. Harmless with the stub runner, and it becomes a real double run, or a deleted
review, when the runner lands. Fix in the store: `UPDATE ... WHERE id = ? AND state IN (...)`, and treat 0 rows as the
409.

### 4. LOW: a PR pasted twice without a head starts two reviews

With no head, the folder is `pr-<n>-pending`, and the fetch step moves the run to `pr-<n>-<head7>`. A second paste of
the same URL after the move finds no row at `pending` and makes a new one, so the same PR at the same head is
reviewed twice and paid for twice. The contract says that ask finds the first row. Not reachable with the stub
runner, since nothing moves the folder. Fix when the fetch step lands: before creating a pending row, look for a live
row with the same host, org, repo and number.

### 5. LOWS AND NITS

- `postPR` answers 400 `bad_request` for any error from `Recognise` other than no-recogniser. A halted store there
  should be the 503 halted body, as `CreatePR`'s error path already checks.
- `tailLog` reads `run.log` without safepath, while the drawer routes go through it. A walker works in that folder and
  could make run.log a link to any file, which `GET /v1/prs/{id}` would then serve. Resolve it the way the drawer does.
- The seeded walker brief says `gh pr view <n>` with `<n>` left in. Substitute the number at launch, or say "the PR
  number".
- Changing `reviews_root` puts every existing row outside it: the drawer answers 403 and abort leaves the folder. Say
  so beside the setting.
- `RunFolderOn` makes the folder before `CreatePR` validates the row, so a refused create leaves an empty folder.

## Verdict

HOLD e502b215..97c99ef0 for finding 1. Re-read e502b215..tip. The verdict will be room-ok.

Quality: after the Sonnet switch, no drop seen in what it set out to do. The contract is followed route by route,
containment is careful, and the tests are broad. The misses are at the edges of what it built against: the renderer's
blocking word and spaced names, both of which changed after the contract was written.
