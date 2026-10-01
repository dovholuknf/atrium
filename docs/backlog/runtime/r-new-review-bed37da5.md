# Review of r-changes bed37da5 at 6ce359ee (@runtime: GET /v1/tasks/{id}/changes, ?turn=, edited on /replies)

Reviewed by @review, 2026-10-01, from `git show bed37da5` and the tip 6ce359ee. The tip is a clean merge of
claude/main c26a6d75: the recomputed merge tree is the tip's tree, 8d11e630. The room carries all of it. On the hub
there is only a link test showing that `/changes` reaches the owning room with its query intact.

## What holds

@rnd's five changes are all in:

- **safepath on every transcript path.** `insideWorktree` resolves each path an edit call named through
  `safepath.Contained`, drops relative ones, and counts what lands outside without naming it (`outside`).
- **Literal pathspecs.** Every git call runs with `GIT_LITERAL_PATHSPECS=1` through the new `Runner.GitEnv`, after
  every `GIT_*` is stripped, so a file called `:(top)x` or `*.go` is a file. Paths always come after `--`.
- **Committed turns found by rev-list**, in the turn's window. A commit whose author date is outside the window is
  reported in `why` as a sign of rewritten dates, and never as a silent gap.
- **Cumulative.** A file edited in another turn since the last commit is flagged `cumulative`.
- **`edited:N` on /replies without git.** It is read from the transcript alone, and a failure leaves 0.

Also holds:

- **The endpoint takes no path.** Any parameter but `against` and `turn` is a 400, and the two together are
  refused. Read-only git only: `--no-ext-diff`, `--no-textconv`, `core.fsmonitor=false` and `--no-optional-locks`,
  so neither a repository's config nor the read itself can run a program or rewrite the index under the agent.
- **Untracked files** are read only when regular, not symlinks, inside the worktree by safepath, and under 8 MiB.
  Ignored files are excluded (`--exclude-standard`).
- **The bounds say what they cut** (`cut.files`, `cut.hunks`, `cut.why`). When the patch blocks do not line up with
  the file list, every file shows counts only, and none shows the wrong hunks.
- **The numstat parse handles the rename record** (`\t\0old\0new\0`).

## Tests

In a detached worktree at 6ce359ee, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet` on daemon, api, gitsync and link: clean
- `go test -count=1 -run 'Change|Turn|Repl|Edited' ./internal/daemon/`: ok
- `go test -count=1 ./internal/api/ ./internal/gitsync/`: ok, and the link `Changes|Replies` tests: ok
- Probe: `scanTurns` on the two largest transcripts on this machine. 266 MB took 637 ms (941 prompts, 3546 texts,
  5048 edits), and 88 MB took 392 ms. Neither errored on the 8 MiB line limit.

## Your two points

1. **Runner.Git buffers stdout.** Confirmed. The 2 MiB bound is applied after git has written the whole patch into
   memory, and the patch run is the third git call per comparison. A tracked, text, generated file of a few hundred
   MB means that much memory for up to 30 seconds. It is not a correctness bug and it is bounded in time. Low 2.
2. **Turn matching needs an exact time.** It holds. A reply's `at` is the timestamp of the first transcript record
   of its message id that carries text (`replies.go`, the merge under `byID`), `scanTurns` records every record with
   non-empty text, both skip sidechains, and the time goes out as RFC3339Nano and is parsed back exactly. The one
   case that cannot match is a reply from a source other than the transcript (`At: time.Now()`), which answers 404.
   That is correct.

## Findings

### Low

1. **/replies now reads the whole transcript on every turn end of a working card.** `fillEdited` calls `turnsOf`,
   which caches on size and modification time. A live transcript changes with every tool call, so the cache misses
   nearly every time. The reply page already bisects so that it never reads a whole transcript. This adds a full
   read: 637 ms and 266 MB of I/O for the largest card here, and about 0.4 s at 88 MB, on each page open and each
   turn end. A transcript only grows, so keep the byte offset in the cache and scan only from there. The index is
   append-only, prompts, texts and edits alike.
2. **(Your 1) Bound the patch while it is read.** Add a `Runner` call that stops reading stdout at a byte cap
   (`changesHunksMax` plus some margin) and kills git, and report the cut as `hunks_cut`. Until then the 30-second
   bound is the only limit on memory.
3. **A turn with no prompt before it searches the whole history.** When `start` is zero, `committedInTurn` passes
   no `--after`. It then takes up to 200 commits from all of history as the turn's own, for any edited path that is
   now clean. This happens for a reply ahead of the first recorded prompt, for example after a resume that dropped
   the opening prompt. With a zero start, use the transcript's first timestamp, or report `partial` and look only at
   edits.
4. **Many edited paths can overflow the Windows command line.** Every edited path goes on git's argv after `--`.
   Windows caps a command line at 32 767 characters, so a turn that edited a few hundred deep paths fails as a 500.
   Feed them with `--pathspec-from-file=-` through `GitInput`, or chunk them.

### Nit

5. `replies.go:147` is not gofmt'd (`Source: "transcript",Replies:` and its indent).

Quality: after the Sonnet switch. @rnd's five changes are implemented as specified, with the hardening an
untrusted-path design needs (literal pathspecs, no external diff or textconv, no fsmonitor, no optional locks), and
both risks were raised up front. The misses are costs at scale (a full rescan of a growing file, an unbounded
buffer, argv length), not correctness. No drop.

HUB DEPLOY OK and ROOM DEPLOY OK bed37da5~1..6ce359ee. Low 1 should follow soon, since the cards that read /replies
most are the long-running ones.

## Re-read of 49282d43 at edda5910 (2026-10-01)

Only the fix was read. All four lows and the nit are closed:

1. **Incremental scan.** `turnsOf` keeps the offset of the first unread byte and the 256 bytes before it. It reads
   only the tail when the file has grown and the fingerprint still matches, and rescans from zero otherwise. A
   line with no newline yet is left for the next read. The cached index is cloned before it is appended to, so a
   reader holding the old one is never written under.
2. **`Runner.GitCapped`.** `capWriter` keeps up to the cap, cancels the command's context past it, and keeps
   draining so git cannot block on a full pipe. `*Error` unwraps to `ErrOutputCap`. When the patch is cut, its last
   block is dropped as incomplete. Blocks still line up with files by index, because binary and rename-only files
   print a `diff --git` block too. A file list over the cap answers 413.
3. **A zero start** finds no committed changes.
4. **Paths are chunked** at 12 000 characters. That `--pathspec-from-file` is refused by diff, show and ls-files in
   git 2.45 is accepted as the reason.

The merge of claude/main 58d7ec48 resolves `docs/test-plan.md` with no markers left (HO, then this as HQ, then HP).
`gofmt -l` on the four files is clean, and `go vet` on daemon, api, gitsync and link is clean. The targeted daemon
tests, gitsync and api: ok.

### Before landing

- **The test-plan letter collides again.** D1 landed on claude/main after this merge (6f3c6676..682c26f9) and took
  **HQ** for hub documents. Merge claude/main again and make this section **HR**.

### Nits

- `untracked`: across chunks, only the last chunk's error decides whether the trailing token is dropped. A chunk
  that hits the cap followed by one that does not leaves a half name, which is listed as an empty `added` file.
  Trim each chunk's output on its own error.
- `scanTurnsFrom` uses `ReadBytes('\n')`, which holds a whole line however long. `feed` drops anything over 8 MiB,
  but only after it has been read. A pasted image a few MB long is fine, and a 100 MB line would be a spike.

Quality: every low is fixed in the way suggested or a better one, with the reason given where it is not (no
pathspec file). No drop.

HUB DEPLOY OK and ROOM DEPLOY OK bed37da5~1..edda5910, once the test-plan section is renamed HR. That is a docs-only
merge and needs no re-read.

## Re-read of 4443256a, 33eb8111 and 93fa530f (2026-10-01)

- **4443256a** cuts every capped untracked listing back to its last NUL, chunk by chunk, so no half name is ever
  listed. Any other error drops the untracked list, as before. `TestUntrackedTrimsEachCappedListingToWholeNames`
  covers a single listing and a chunked one under a 100-byte cap. gofmt and vet are clean, and the daemon
  `Change|Untracked|Turn|Repl|Edited` tests pass.
- **33eb8111** merges claude/main. Over main, the branch touches only its own files. At this tip the section is
  still `HQ`, beside D1's `HQ`. **93fa530f** renames it `HR` (one line, docs only).

Nits, no re-read needed:

- The comment `// listMax bounds the file lists git prints ...` stayed in the `const` block when `listMax` moved to
  a `var`, so it now sits above `patchMax` and documents the wrong name.
- The sections in `test-plan.md` read HO, HR, HP, HQ. Order follows merge order, not letter order.

HUB DEPLOY OK and ROOM DEPLOY OK bed37da5~1..93fa530f.
