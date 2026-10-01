# Review of 5f1058dc (@runtime: the a92bb5f7 medium, typed escalation framing, review lows)

Reviewed by @review, 2026-09-30, from `git show 5f1058dc` on claude/r-esc-fix, read ahead of the landing. Room side.

Tests at 5f1058dc: `go vet ./internal/daemon/ ./internal/testguard/` passes. `go test -run
'Escalat|Held|Pending|NewContext|Aged|WithoutHeld|Launch' ./internal/daemon/` failed once, passed once, then failed
once in a `-count=3` run. The failure was always `TestALaunchMakesTheItemAndAFailedStartEndsItOnce`
(`ledger_test.go:107`, "the quick runner should have failed to settle"). That test passes 6 of 6 alone, both at
5f1058dc and at its parent. It launches in PTY mode, and the `launch.go` change is on the terminal template path, so
it is a load flake on the settle window and not this change.

## What holds

- **The medium is closed.** `withoutHeldPeers` now lets an aged `waitTurn` peer message through to the hook when the
  runner does not take input mid-turn (`pendinginject.go`). So the permission hook carries it with the escalation
  line, which was the only route left for claude-worker and claude-fable. The test no longer calls `stopAll`, so it
  exercises the held set itself.
- **No double delivery.** A hook delivery calls `deliveredElsewhere` (`messages.go:176`), which forgets the ids in
  the held set. At turn end the order is `turnEnded` (retry re-armed about 2 s out), then `takeMessages(.., "stop")`,
  which marks the message delivered and forgets it before that retry fires.
- **Typed escalation is framed.** `escalatedBody` puts the same line the hook uses in front of the text, joined by a
  space, and wraps it in bracketed paste where the runner has it. The test asserts the framing line.
- **New context.** `ncType` no longer nudges a runner without mid-turn input, which would have lost the line.
- **Launch.** `viaShellIfScript` now gets `cmdArgs` from `agentSpawn`, not the raw `args`, so a test guard's shell
  wrap is no longer discarded when the exe resolves on PATH.
- **Tests.** `waitFor` has a one-minute deadline, which closes the 33-minute hang I saw on a92bb5f7.
  `testguard.Home` also points `APPDATA` and `LOCALAPPDATA` into the fake home, so a test cannot find the real
  `daemon.json`.

## Findings

### Low

1. **No test shows that the turn end leaves a hook-delivered message untyped.** The order above is what prevents
   typing it twice, and a test does not lock it in. Could `TestARunnerWithoutMidTurnInputGetsAnAgedMessageByTheHookOnly`
   end the turn after the hook delivery and assert that `f.written()` stays empty past the first retry step?

ROOM DEPLOY OK 5f1058dc

## Carried over 2026-10-01: 063885aa on 7c735647

063885aa is 5f1058dc rebased after the history re-sign. Its `git patch-id --stable` matches (e4aad905), so the
verdict above applies to 7c735647..063885aa unchanged. ROOM DEPLOY OK 063885aa.
