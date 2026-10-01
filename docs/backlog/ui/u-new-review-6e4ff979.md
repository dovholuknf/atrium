# Review: live412-changes 6e4ff979 (@ui)

Range e502b215..6e4ff979, one commit on sg3/claude/live412-changes. Live-412 items 4i, 5b, 5d, 5f, 5g and 5l: an empty
file in the /m viewer says so, a reply to a message that edited nothing loses the files-edited chip, the shell-command
caveat and the cut note are said once with the right plural, bars run on a square-root scale with a floor, and a second
tap on a quoted line unquotes it. Unsigned (sg3 has no key), noted as asked.

Board checks are @ui's. I read the mViewer and mChanges cases and did not run them. Go: vet clean, and every daemon
test matching Changes, Turn, Replies or Prompt passes at 6e4ff979, including the two new ones.

## The question asked: does a non-human origin line now wrongly start a turn?

No, on the evidence available. I read every user line in this machine's Claude Code transcripts (about 15,500 lines
carry an `origin`). The kinds are `human`, `task-notification`, `peer` and `coordinator`. Every `peer` and
`coordinator` line is `isMeta`, so it still returns early. Every non-meta line with a kind other than `human` is a
`<task-notification>`, which `promptOf` drops because it starts with `<`. Not one non-meta, non-human line has text
that `promptOf` would keep. So dropping the origin test changes nothing for any line written here.

The cost of that: the change also fixes nothing written here. See the medium.

## Findings

### MEDIUM: 4i is not fixed. A quote sent while the card is working is an attachment line, and feed skips it (proven)

The commit infers that a phone quote reaches the transcript as a user line with a non-human origin. On this machine it
does not. A message typed into the terminal while the card is idle is a user line tagged `human` (thousands of
`[atrium] ...` lines), which already started a turn before this change. A message typed while the card is mid-turn is
written as:

```
{"type":"attachment","attachment":{"type":"queued_command","commandMode":"prompt","origin":{"kind":"human"},
 "prompt":"..."},"timestamp":"..."}
```

`turnIndex.feed` only acts on `rec.Type` `user` and `assistant`, so the line is skipped, the next prompt is still the
one before the edit, and the reply to the quote claims the turn's edits. That is the 4i symptom: a quote is sent from
the changes sheet, which is opened while or right after the card edits, so it is likely to arrive mid-turn.

Proof: D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-6e4ff979/zz_scratch_review_test.go, run at 6e4ff979
in internal/daemon. It is the new test with the user line replaced by the attachment line. It fails:
`reply 0 edited 1`, `reply 1 edited 1`.

Fix: in `feed`, treat a main-chain `attachment` whose `attachment.type` is `queued_command` and whose `commandMode` is
`prompt` as a prompt at its timestamp, passing `attachment.prompt` through `promptOf`. `textScan.feed` in replies.go
should do the same, or the prompt list beside the replies will not show the quote the reply answers. Keep or drop the
origin change as you like, but the test should carry the attachment shape, since the `channel` origin it uses does not
occur in any transcript here. Read one live transcript of the 4i card before calling it fixed.

### LOW: turns.go and replies.go now disagree on what a prompt is

`textScan.feed` (replies.go:673) still drops a user line with a non-human origin, and `turnIndex.feed` no longer does.
Should such a line ever carry words, the turn would split there while the reply list shows no prompt at that point.
Make the two read the same rule, ideally one function.

### Holds

- changes.go: `cut.why` carries only the reason, and the sheet words the counts. The new test pins it.
- changes.js: `whySaysIt` matches the room's `shellNotCounted` sentence ("changes made by a shell command are not
  included"), so the caveat is said once. The singular "1 file shows counts only" is right. The square-root bar with a
  6% floor keeps a small file visible beside a huge one, and the numbers beside it stay exact.
- changes.js `comment`: reading `was` before `addComment` is right, since removing a chip fires `m-comment-removed`,
  whose handler clears the key and the mark first. The later delete and class removal are no-ops then.
- viewer.js: an empty, uncut file says "empty file (0 bytes)" and draws no box.

## Verdict

HOLD e502b215..6e4ff979 on the medium. Re-read e502b215..tip, hub-ok and room-ok (turns.go needs a room deploy).

Quality: after the Sonnet switch, the JS items are careful and complete. The Go fix rests on a cause the commit says was
inferred without reading a live transcript, and the test was written to the inferred shape, so it passes without
showing the bug was ever reproduced.

## Re-read: 23e23760 (2026-10-01)

Range e502b215..23e23760, one new commit on top of 6e4ff979. Unsigned, noted.

`queuedPromptOf` reads a main-chain `attachment` line whose `attachment.type` is `queued_command` and whose
`commandMode` is `prompt` (or empty), and passes `attachment.prompt` through `promptOf`. Both `turnIndex.feed` and
`textScan.feed` call it first. The origin filter in turns.go is back as it was, so the two files agree again and the
low is closed.

Checked against this machine's transcripts (about 4,400 queued_command lines):

- `commandMode` `task-notification` lines (about 1,250) are refused by the mode test. Their text also starts with `<`.
- `peer` prompts start with `<` and `promptOf` drops them, the same as a peer user line, which is `isMeta`.
- The `coordinator` ones are all sidechain and refused.
- A prompt can be an array (87 lines), which `promptOf` already reads.
- In 15 transcripts, no queued prompt also appears later as a user line, so a quote is not listed twice.

Tests at 23e23760: vet clean. Every daemon test matching Queued, Changes, Turn, Replies or Prompt passes, and so does my
proof from proof-6e4ff979. `gofmt -l` names internal/daemon/fyi_test.go, which this range does not touch.

### New

- NIT: `textScan.feed` returns before it sets `firstAt`, so when the first timed line of a tail read is a queued prompt,
  `firstAt` comes from the line after it. The window is then said to be complete from a little later than it is. That
  errs safe and costs at most one prompt at a page seam.

### Verdict

OK hub and room e502b215..23e23760.

Quality: after the Sonnet switch, the round-2 fix is the one asked for, in both readers, with a test of the real line
shape that fails on the old code. Good.
