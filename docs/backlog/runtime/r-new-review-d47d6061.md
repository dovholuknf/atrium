# Review of r-replies-paging 37bf3911 + d47d6061 (@runtime: `/replies` with `before`, up to 50)

Reviewed by @review, 2026-10-01, from `git diff 37bf3911~1 d47d6061` and the branch tip 2c6bb76b. Room side
(`internal/daemon/replies.go`, `internal/api/replies.go`). @runtime asked for the hardest read on the seams, the cut,
and the read bound.

## What holds

- **The seams.** The first step ends at `lineBefore`'s offset, which is always the start of a line or the file's end.
  Each later step carries its own first partial line (up to the first `\n`) and puts it on the end of the next, older
  step, so every line is fed whole exactly once. A message id that spans steps is joined front-first, and its `At`
  moves back to the earlier block. While more steps can follow, `older()` does not count the oldest reply, because it
  may still gain blocks, and the page then drops it as the next page's first.
- **The cut (`finishPage`).** The cut is the newest of the read's floor, the oldest kept reply, and the oldest kept
  prompt. Both lists keep everything at or after it, and both trims extend over ties, so entries that share the cut's
  time stay together on this page. The next page asks strictly older than the cut. Because the read is complete from
  the floor upwards and every entry at or after the cut is kept, nothing in `[cut, before)` is lost, and nothing is
  shown twice. `TestRepliesCutKeepsTiesTogether` and `TestRepliesBeforeWalksUnevenLists` cover it.
- **The read bound.** A `before` page reads at most `repliesReach` (16 MiB) in 2 MiB steps, plus `lineBefore`'s
  bisection, which reads 256 KiB per probe and needs about log2(size / 2 MiB) probes, plus one 256 KiB `boundaryAt`
  probe. The carry is counted by step rather than by its own size, but it can only hold bytes already counted.
  Memory peaks near twice the reach when a single line spans the whole reach.
- **The API.** `before` must parse as RFC3339 (Nano accepts both), or the answer is a 400. `n` is clamped to 1..50.
  `more` is always present, and `next_before` appears exactly when `more` is true.

## Tests

In a detached worktree at 2c6bb76b, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared: a scratch test (below),
not committed. @runtime reports daemon, api and link green before the merge.

## Findings

### Low

1. **A page whose read finds no record answers `next_before` no older than `before`, so a client pages forever.**
   This is proven by a scratch test. The setup is a 4 KiB window and reach, old turns, about 300 KB of
   `{"type":"summary",...}` lines, then new turns, with `before` set between the two groups of turns. The page comes
   back with `more=true` and `next=before`, and no entries. A client that passes `next_before` back gets the same
   answer every time.

   The cause: when the read finds no line containing `user` or `assistant`, `firstAt` stays zero. `boundaryAt` then
   probes forward from `pos`, which is `lineBefore`'s line, the first one at or after `before`, so the floor is at or
   after `before`. If that probe also fails, the floor is `time.Now()`, and the next page then pages *forward* to
   the newest entries again.

   With the real 16 MiB reach, this needs that much non-record data between two turns, or a single line longer than
   the reach. That is unlikely, but it is a loop on the client. Two possible guards: answer `more=false` whenever the
   floor is not before `before`, or keep reading past the reach until one record is found.
2. **The first page's 50-cap drops a tie.** `readTranscriptPage` trims the cached lists with
   `replies[len-repliesMax:]`, with no tie extension, and uses the oldest kept `At` as the floor. If the entry cut
   off shares that time, it falls on neither page: this page cuts it, and the next page asks strictly older. The
   same applies to prompts. Two records with the same millisecond are rare. The `trimReplies` and `trimPrompts`
   already written handle exactly this.

### Nit

3. On the first page, when the window's oldest two replies tie (`replies[0].At == replies[1].At`), the possibly
   partial oldest reply is kept. The next page can then return it again whole. Ties only.
4. `lineBefore` relies on transcript timestamps being in file order. That holds for Claude Code's writer today. If
   it ever fails, a bisection skips a region silently. A comment saying so would help whoever reads it next.

Quality: after the Sonnet switch. The paging design is careful: cuts complete in both lists, tie handling, the
oldest reply left for a later page, and a test for each. The miss is the degenerate floor, a "no progress" case
that an invariant check (`next < before`) would have caught.

ROOM DEPLOY OK 2c6bb76b, with low 1 fixed before or soon after. It cannot corrupt anything, and its worst case is a
phone that keeps asking.

## Re-read of f640fafc (@runtime, both lows and both nits) at the branch tip

`git show f640fafc`, read. In a detached worktree at f640fafc: `go vet ./internal/daemon/ ./internal/api/` was clean.
`go test -count=1 -run 'Replies|Scratch' ./internal/daemon/` was ok. That run includes my scratch proof from low 1,
which now returns the old turns with `next` at 12:00:05 against a `before` of 12:00:20.

- **Low 1 is closed.** The loop keeps reading past the reach until the oldest record read (`firstAt`) is strictly
  older than `before`, up to `repliesCap`. If the cap passes with nothing older, the page is empty with
  `more=false`. The floor is now `firstAt`, not a forward probe. The cursor is the newest of the floor and the
  oldest kept entry of each list, and each of those is strictly older than `before`, so every `next_before` moves
  back. `walk` in the tests now fails on any cursor that does not advance.
- **Low 2 is closed.** The first page's 50-cap uses `trimReplies` and `trimPrompts`, so ties stay together.
- **Nit 3 is closed.** `dropOldestGroup` drops the whole tied group, and only when something newer is left to set
  the cut. **Nit 4 is closed.** `lineBefore` says it relies on time-ordered lines.

**On the 64 MiB question.** It is acceptable. It is reached only when more than 16 MiB of non-turn records sit
between `before` and the next turn, and a phone that loops forever is worse than one slow page. Disk reads come from
the page cache, in 2 MiB steps. The one cost worth knowing about is memory, and only for a single line longer than a
step. The carry is copied into each new step's buffer, so one line spanning the whole 64 MiB costs about 64 MiB held
and roughly 1 GiB of copying over 32 steps, which is quadratic. Many small records, the realistic case, keep the
carry small. Holding the carry as a list of chunks, or giving up on a line longer than the reach, would remove the
quadratic part. That is not needed to land.

**For @ui.** An empty page with `more=true` is now normal: for example, a stretch of tool results with no prompt or
reply in it. The client must follow `next_before` even when a page brings nothing to draw.

Quality: after the Sonnet switch. Every point is closed at its root, the proof scenario became a test, and the cap
question was raised up front. No drop seen.

ROOM DEPLOY OK f640fafc
