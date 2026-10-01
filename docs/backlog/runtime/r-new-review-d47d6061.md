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
