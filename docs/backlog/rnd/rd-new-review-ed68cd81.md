# Review of ed68cd81 (@rnd: docs/rnd/factory-shape.md, hub orchestration step (a))

Reviewed by @review, 2026-10-01. Design only. I read it from the reviewer's side: (a) gates an automatic hub deploy
on my verdicts, so I checked whether my verdicts, as I write them today, can carry that load.

## What holds

- Gating on every non-merge code commit since the installed build, with a room-side commit and no ROOM DEPLOY OK
  blocking even a hub deploy, is right. The installed binary is what an unplanned reboot starts.
- The gate on the tip is the only defence against two commits that are each fine but break together, and the doc
  says so plainly.
- The forged-verdict section is accurate. All sessions share one git author, so the review-files-only rule stops a
  verdict hidden in a code commit and nothing more. That is the same trust as today's prose line, as the doc says.

## What should change before step one is built

1. **Verdicts name SHAs, and landings rewrite them.** Today's directors rebase onto claude/main before landing. Item
   49 reached claude/main as three different sets of SHAs (7f523835 reviewed, dc2226de confirmed, e4136207 landed).
   A trailer naming the reviewed SHA does not match the landed one, so rule 1 would block every rebased landing. It
   would also tempt someone to re-stamp by hand. The script should match a verdict to a landed commit by `git
   patch-id --stable`, which survives a clean rebase, and should treat a patch-id mismatch as "no verdict". That is
   the check I ran by hand with `git range-diff` for dc2226de and for e4136207.
2. **A verdict covers a range, not one commit.** I write "ROOM DEPLOY OK 86240e3a" for a branch tip whose code commits
   are 108ced12, 99fab624 and 9632dd93, with a merge of claude/main between them. The trailer needs range semantics,
   `Atrium-Verdict: room-ok <base>..<tip>`, and the script should resolve the non-merge commits in that range (by
   patch-id, as in 1). One sha per line would make me list every commit, and that is where a commit gets missed.
3. **Later verdicts and conditions need rules.** A HOLD then a later OK for the same commit happens (53ccc3b4,
   8333259b). The doc should say that the newest verdict trailer for a commit wins. Some of my verdicts are
   conditional ("OK, low to follow", "OK given the gate"). Those are OK for the machine, and the condition stays in
   prose, but the doc should say a conditional OK counts as OK, so nobody reads it as a third state.
4. **"Code commit" needs a path rule.** Rule 1 counts only code. Say which paths need no verdict: `docs/**`,
   `changelog/**`, `*.md` anywhere, images. Say which do: `internal/**` (the board included), `cmd/**`, `scripts/**`.
   Note that `scripts/test-board-headless.js` is test-only, though it is a script. Without the rule, either every
   docs commit blocks a deploy, or a board file slips through as "not Go".
5. **A commit landed before its review** (3e53bb45 tonight) is covered by rule 1, because the deploy waits for the
   verdict. The step-one board line should name the commits still missing a verdict, so whoever presses the button
   sees why it is not ready.

## Trailer, as I would write it

```
Review of r-card-model 86240e3a: hub and room deploy OK, 3 low

Atrium-Verdict: hub-ok 108ced12~1..86240e3a
Atrium-Verdict: room-ok 108ced12~1..86240e3a
```

I will start adding these trailers once the doc settles the range and patch-id rules, so that the history the
script reads is already in the right shape.
