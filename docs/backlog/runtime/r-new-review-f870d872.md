# Review: r-pr-render f870d872 (@runtime)

Range 66694076..f870d872, one commit on claude/r-pr-render. The pulls-view P1 step 8 renderer, a pure package
`internal/prreview/render`. It is not wired to anything, so no deploy is needed. Read against
docs/rnd/pulls-view-design.md 5.4 and 5.6 (at 54b54fe6) and the rules in docs/review/review-memory-design.md.

Tests: `go vet` clean. The package's 13 tests pass. My three scratch tests, in
D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-f870d872/zz_scratch_review_test.go (copied into the package
in a detached worktree, since removed), fail as stated below.

## What holds

- The order is 5.6's: left for clint, then band, then rank, then diff position and line. File name is never a key,
  and `NN` is the walk position.
- The hard rules (shape, 5, 8/35, 26) fail the run only after the merge fork has had its one resend. The soft ones
  (34, 36, 40, 43) act on the second pass the way the design's table says. Each resend quotes the rule.
- Rule 43: "Suggested fix:" only for `proven: code`. `no` needs the "Could we / What if we / Should we" form with one
  question mark, and any other `proven` value is refused. There is no `test`.
- Rule 5: a leak raised before the merge must reach the final list by path and line, or by path and code.
- The diff parser reads `+++` only outside a hunk, counts context and added lines, and skips `/dev/null`.

## Findings

### 1. MEDIUM: a partial path passes rule 8 and gets a wrong deep link (proven)

`diffInfo.file` matches a finding path to a diff name by suffix (`tls_engine.c` to `src/tls_engine.c`), so the line
check passes. But `link` and the label line use the finding's own `Path`, so the link hashes `tls_engine.c` and lands
nowhere on GitHub. Rule 28 says a link is checked against the diff and never guessed. Proof: `TestScratchShortPathLink`.

The same suffix match picks the first of two files with one base name (`a/util.c`, `b/util.c`) for a finding that
says `util.c`, which can put the finding on the wrong file.

Fix: after the checks, rewrite each finding's `Path` to the diff's name `file` returned, before the label, link and
file name are made. Treat a suffix that matches more than one diff file as a rule 8 failure, so it goes back to merge.

### 2. MEDIUM: a second `Write` loses walk progress and leaves stale files (proven)

5.5 retries rerun step 6 and step 7 over the same folder, which renders again. `Write` overwrites `walk.txt` with every
line `open` and writes the new files beside the old ones. A walk clint had started loses its `done`, `skipped` and
`deferred` states, and finding files he edited in place stay beside new ones with other `NN`, so `ls findings/` is no
longer the walk. Proof: `TestScratchRewrite` (3 files after a rewrite to 1, and the `done` gone).

Fix in the package, since `Write` is its contract: refuse when `walk.txt` already has a line that is not `open`
(a typed error the runner can show as "walk started, retry would lose it"), and otherwise remove the
`findings/*.txt` this render did not produce before writing.

### 3. LOW: a finding with no rank walks first in its band (proven)

`Rank` 0 sorts ahead of 1. The merge fork is asked for a rank, but a missing one should not put a finding at the top.
Sort 0 last, or refuse it under rule 0. Proof: `TestScratchZeroRank`.

### 4. NITS

- `blocking` and `critical` both render as `HIGH`. Rule 10 lists `BLOCKING` as a label of its own.
- The leak match by path and line, or path and code, refuses a leak that merge re-anchored onto a different PR line
  (rule 15). That is the safe direction. Say so in the comment, so nobody loosens it later.
- `+++ "b/a b.c"` (git quotes a path with spaces) is read with the quotes kept.

## Verdict

HOLD 66694076..f870d872 for findings 1 and 2. Both are small and both have a proof to turn into a test. Re-read
66694076..tip.

Quality: after the Sonnet switch, no drop seen in what it set out to do. Every rule has its test and the golden file
is exact. The misses are the second-order ones: the suffix match was written for the check and not followed into the
link, and `Write` was written for the first render only.

## Re-read 66694076..7fafc6a8

Two commits. 9aae9ba7 is f870d872 rebased (same patch-id). 7fafc6a8 is the fix.

Tests: `go vet` clean. The package's 19 tests pass, 6 of them new. My scratch tests, rewritten for the fixed API in
D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-7fafc6a8/zz_scratch_review_test.go (recipe
test-7fafc6a8.ps1 beside it), pass for every earlier finding. One new test fails, as stated below.

### Closed

- Finding 1: `Render` rewrites each path to the diff's name through `resolve` before the label, link and file name.
  `file` now counts matches, and a suffix that matches more than one file is a rule 8 failure. The leak match
  resolves both sides too, so `a.c` and `src/a.c` count as one file.
- Finding 2: `Write` returns `*WalkStartedError` when `walk.txt` has a line that is not `open`. Otherwise it removes
  the `findings/*.txt` this render did not make.
- Finding 3: an unset rank fails under the shape rule, and `rankKey` sorts it last for any caller that sorts on its
  own.
- Nits: `blocking` keeps its own word in the label and the file name, and sorts with HIGH. The leak-match comment
  says not to loosen it. A quoted `+++` name is unquoted, and a tab-ended one is cut at the tab.

### New

LOW (proven): `started` reads the state as `strings.Fields(l)[1]`. A file name with a space (now possible, since the
quoted `+++` fix reads `a b.c`) splits, so `b.c-L3.txt` is read as the state, and `Write` refuses a walk nobody has
touched. Refusing is the safe direction, and a path with a space is rare in a PR, so this is not a hold. Fix: read the
state from the last field. Proof: `TestScratchSpaceName`.

### Verdict

ROOM OK 66694076..7fafc6a8, 1 low. The package is not wired to anything, so no deploy follows.

Quality: after the Sonnet switch, no drop seen. Each fix has its own test, and the nits were taken without being
asked twice. The new low is a second-order one again: fixing the quoted name made a space reach a parser that
splits on whitespace.

## Re-read 58461561..3a908c38 (r-pr-render-2)

Landing check first: r-pr-render landed on claude/main as the squash 58461561. Its tree equals 7fafc6a8 for
internal/prreview, changelog/runtime and docs/test-plan.md, so it is the reviewed patch.

3a908c38 fixes the low. `started` reads the name up to the first `.txt` followed by whitespace, then the next token
as the state. The last field would be wrong, since a `done` line ends in a time and a URL. Its test covers an
untouched and a done line with a spaced name.

Tests: `go vet` clean, the package's 20 tests pass, and `TestScratchSpaceName` now passes. A new scratch test
(`TestScratchStartedLines`, same proof file) passes for a done line with a time and URL and for CRLF lines.

NIT (proven): a base name that itself holds `.txt ` (`x.txt y.c`) ends the name early, so `y.c-L5.txt` is read as
the state and the walk is refused. That is the safe direction and the name is contrived. If touched again, anchor on
the `-L<line>.txt` every file name ends in: `^(.*?-L\d+\.txt)\s+(\S+)`.

### Verdict

ROOM OK 58461561..3a908c38, 1 nit. The package is not wired to anything, so no deploy follows.

Quality: after the Sonnet switch, no drop seen. It rejected the fix I suggested for a reason that holds (the time
and URL on a done line) and tested both line shapes.
