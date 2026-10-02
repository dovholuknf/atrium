# Review: r-scrub-sid 2ec9a7c5

`claude/r-scrub-sid`, one commit on 87f53640. It is a pause exception asked for by the orchestrator. One line of
`internal/daemon/testdata/frame-settled-statusline.bin` has sg4's machine SID, which this commit replaces with a fake
of the same length, `S-1-5-21-1111111111-2222222222-3333333333-1006`.

Verdict: **OK.**

## How it was checked

- **Same length.** The file is 21394 bytes before and after. `cmp -l` shows 26 bytes changed, which are the SID's
  digits, so no offset in the recorded frame moves.
- **The tree.** At 2ec9a7c5 there is no `S-1-5-21-` with the real machine prefix anywhere. The only SID-shaped string
  left is the fake.
- **The tests.** `go test -count=1 -run 'LooksIdle|Settled' ./internal/daemon/` passes in a detached worktree at the
  tip.
- **History.** The real SID is still in older commits, as stated, and that is the orchestrator's decision. Only the
  tree is scrubbed.
- **`C:\Users\claude`.** It is a generic account name. Leaving it is fine.

One note for the orchestrator's scrub: the same SID also sat in @fabric's `claude/f-room-accounts` (commit 2c4ddfb8, a
whoami fixture). That branch has not landed, and I have held it until it is squashed (review abca26e6).

Atrium-Verdict: room-ok 87f53640..2ec9a7c5
Atrium-Verdict: hub-ok 87f53640..2ec9a7c5
Quality: minimal and exact. A same-length replacement is the right way to edit a recorded frame.
