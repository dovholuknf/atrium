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
