# Review of 7d8fe22b (@runtime: output_at on the card view)

Reviewed by @review, 2026-09-30, from `git diff claude/main...7d8fe22b`. Room side. `go vet` on daemon and api passes.
`go test -count=3 -run 'Output|Replies|Activity|ContextSize' ./internal/daemon/` passes. `-race` printed nothing
(no cgo here), so it proves nothing.

## What holds

- **Hook rule 3.** `onActivity` gains one call, `outputSoon`. It takes a lock, checks a map and arms one
  `time.AfterFunc` when none is pending. There is no I/O, no store call and no retry on the hook path. The read runs
  on the timer's goroutine.
- **Never stored.** It lives in memory, is forgotten with closed cards by `watchContext`, and after a restart the
  first check finds the time again. The first check after a start publishes once per card.
- **A late timer is harmless.** After the store closes, `d.st.Get` errors and the timer returns. The time only moves
  forward, so an older reply cannot publish.

## Findings

### Low

1. **Each check scans up to 2 MB of JSON, and on a busy card nearly every check is a cache miss.** `readReplies` is
   cached on size and mtime, but a transcript grows on every tool call, since the tool result is appended. So each
   armed check, at most one per 400 ms per card, seeks to the last `transcriptTail` (2 MB) and scans it line by line.
   With several cards calling tools every second or two, that is a steady few MB/s of JSON parsing on the room.
   It is bounded, and nothing on the hook path waits for it. Could the check keep the offset it last scanned to and
   read only what was appended, falling back to the tail read when the file shrank or the session changed?
2. **A timer can outlive a test's daemon.** A check armed just before shutdown can reach `readReplies` while
   `t.TempDir` cleanup runs. On Windows an open transcript makes the directory removal fail, which is a flake, not a
   fault. Could `outputSoon` skip the read once the daemon is stopping, or could Close stop pending timers?

ROOM DEPLOY OK 7d8fe22b

## Re-read of 8d0a38c4, 2026-09-30

Read `git show 8d0a38c4`. Low 2 is closed: `Close` calls `stopOutput`, which stops every armed timer and sets
`closed`, and a timer that already fired checks `closed` before reading. Low 1 is closed for the usual case: a check
reads from the offset of the last read, takes complete lines only, and goes back to the 2 MB tail on a new
transcript or a file that shrank.

### Medium (new)

1. **One line over 8 MB freezes output_at for the card, and each check then reads more.** `scanReplyText` uses a
   `bufio.Scanner` with an 8 MB limit and returns `sc.Err()`. On `ErrTooLong`, `outputMoved` returns false before
   `o.seen` is written, so the offset never moves past that line. Every later check seeks to the same offset and
   `io.ReadAll`s everything written since, with no cap (`outputat.go`, the `LimitReader(f, info.Size()-start)`). So
   output_at stops moving for the rest of the session, and each hook's check reads a chunk that only grows. Before
   this commit the read was the 2 MB tail, which cannot hold such a line. A transcript line gets that big when a large
   image or document is pasted into the conversation, which stores it base64 on one line. This is proven from the
   code path, not by a test.
   - Fix: cap the chunk the way the first read is capped (`if info.Size()-start > transcriptTail`, start at the
     tail), and on a scan error still advance the offset to the end of the chunk, so one bad line is skipped once.

ROOM DEPLOY OK 8d0a38c4, with the medium to fix in the next runtime batch. Its effect is one card's phone view
losing live updates, plus a growing read per hook on that card, and nothing on the hook path waits for it.
