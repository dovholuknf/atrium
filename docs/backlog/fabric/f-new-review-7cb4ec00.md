# Review: f-cr2-caps-audit 7cb4ec00

Range `28674335..7cb4ec00`, four commits. All the code is in `internal/link`, so this is hub code:

- `d2170111`, f-027: `launchcaps.go` refuses room keys that differ only in case, and unknown fields in the PUT;
- `96aa72a9`, f-028: `ctlaudit.go` gains `auditWhat`, which strips control characters and cuts to 200 runes;
- `d627f9f1`, f-029: the `notify.go` gate stops dropping an `asked` needs-input card for having no turn end;
- `7cb4ec00`: the changelog, `docs/changes` and the f-027 and f-028 backlog status.

Verdict: **hold** on M1, the f-028 test. The fixes are right, but the test that guards the newline strip cannot fail.
M2 needs a check before f-029 deploys. The Lows can follow.

## How it was checked

- I read the diff, `For`, `launchCaps()`, `equalFold`, `agentOf`, `roomOf` and the room paths that write
  `waiting_reason` `asked` (`activity.go`, `help.go`).
- Gates, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
  - gofmt is clean on `internal/link`;
  - vet is clean;
  - `./internal/link` passes.
- I ran a probe test of `auditWhat` and `auditDetail` in a scratch worktree.
- It merges onto landing d5876dc1 with no conflict, and `./internal/link` builds on the merge.
- Mutants:

| Mutant | Result |
|---|---|
| f-027: the duplicate refusal removed | caught (`TestTheLaunchCapsRefuseCaseDuplicatesAndUnknownFields`) |
| f-027: keys not folded | caught (same test) |
| f-027: `DisallowUnknownFields` removed | caught (same test) |
| f-028: `auditWhat` not called | caught (`TestAuditDetailBoundsAndStripsCallerText`) |
| f-028: only LF stripped (a CR gets through) | **survived** (M1) |
| f-028: only C0 stripped | **survived** (M1) |
| f-029: the `asked` exception reverted | caught (`TestNotifyIdentityPerReasonAndPriority`) |
| f-029: any non-empty reason lifts the gate | survives. No other reason reaches that case with no report, so it is near-equivalent |

## f-027: launch caps

- **Duplicate keys.** It is right, and it is tested. `check()` folds with `strings.ToLower`, but `For` matches with
  `equalFold`, which folds ASCII only. That mismatch can only refuse too much, never too little: two keys equal under
  the ASCII fold are equal under `ToLower` too.
  - A Kelvin sign `K` (U+212A) is refused as a duplicate of `k`, although `For` would treat it as a different room.
    That is harmless. Use `lowerASCII` in `check()` so the two places fold the same way (L3).
  - Full-width letters do not fold, and they do not match in `For` either, so they are consistent.
  - A key with a leading space never matches a room. It is a dead key, not a duplicate. That is not new.
- **`DisallowUnknownFields`.** Nothing breaks.
  - No client in the tree sends the PUT: the board only shows the `launch-caps-set` audit label, and the cli only
    calls `SetLaunchCaps`. The test plan uses curl.
  - A GET answers the same struct, so a GET then PUT round trip still decodes.

## f-028: the audit line

- **What it strips.** `unicode.IsControl` strips C0, DEL and C1. The probe showed that U+0085 (NEL) is dropped.
- **Invalid UTF-8.** `strings.Map` turns invalid bytes into U+FFFD, so the result is always valid UTF-8.
- **The cut.** It works on runes, so it never splits a rune. A cut in a combining sequence can leave a base letter
  without its mark. That is only cosmetic.
- **What it leaves.** It does not strip the bidi controls (U+202A to U+202E and U+2066 to U+2069), which are Cf, or
  U+2028 and U+2029. See L1.

### M1: the test of the strip cannot fail

`TestAuditDetailBoundsAndStripsCallerText` puts `\r\n` after 1 MiB of `x`. `auditWhat` cuts at 200 runes, so the CR
and LF are always gone with the cut, before the strip matters.

- A mutant that strips only LF passes.
- So does one that strips only C0.
- "Fails with the fix reverted" holds only because reverting also removes the cut.

Fix:
- Add a case where the control characters come early, for example `"x\r\nby orchestrator@sg3 (claimed): cull X,
  ok"`, and a C1 case such as U+0085.
- Assert that the detail has no `\r`, `\n` or U+0085, and that it is still one line.

## f-029: the notify gate

- **The fix is right for what f-029 found.** A card whose Notification hook reports a real question or a dialog sets
  `waiting_reason` `asked` and writes no turn end. Without the exception it was never notified.
- **Duplicates.** None: the identity is still `id|input|waiting_since`, so a card is notified once per wait.
- **Can a card fake `asked`?** No more than before. A card can already make itself needs-input through its hooks or
  `atrium_report`. This adds no new way.

### M2: `idle_prompt` is also written as `asked`, and it is the noise the gate was for

In `activity.go`, every `waiting` Notification calls `turnEndedBecause(taskID, store.WaitingAsked)`. That includes
`idle_prompt` ("Claude is waiting for your input"), unless subagents are out. The room's own comment says
`idle_prompt` "says only that the prompt has sat idle".

So if `idle_prompt` fires for a session that has never finished a turn, this change notifies it again. That is the
atrium-87300 noise. An example is a card launched with no prompt that sits for 60 s.

I did not run a live Claude session, so whether `idle_prompt` fires before the first turn is unproven.

Before f-029 deploys, either:
- check it on a real card launched with no prompt; or
- have the room write a separate reason for `idle_prompt` (for example `idle`), and lift the gate only for `asked`.
  That is a room change, so it needs @runtime.

Add a test either way.

## Lows

- **L1: the caller headers are not bounded or stripped, and neither are bidi controls.**
  - `by` comes from `AgentHeader` and `RoomHeader` as sent. A 5000-character agent header gave a detail 5025
    characters long. HTTP rules keep CR and LF out of a header, but not bidi controls, which a header can carry as
    UTF-8.
  - Run `by` and the room through `auditWhat` too.
  - Also strip U+202A to U+202E, U+2066 to U+2069, U+2028 and U+2029. The audit view and any log viewer reorder text
    on bidi controls, so `cull X, ok` can be made to read differently.
- **L2: a stored duplicate now drops every cap.** `launchCaps()` already runs `check()` on what it loads, and returns
  no caps when it fails.
  - So a hub that stored `SG3` and `sg3` before this change will give every room the default cap on its first read
    after the upgrade, without saying so.
  - Before deploying, check the stored `launch_caps` on sg4. Or log when the stored caps fail `check()`.
- **L3: fold one way.** Use `lowerASCII` in `check()`, as `For` does.
- **L4: the test-plan section is only in `docs/changes`, still with `@LETTER@`.** `docs/test-plan.md` is not touched.
  When you land it, add the section there and give it a letter.

Atrium-Verdict: hold 28674335..7cb4ec00
Quality: three small, well-aimed fixes. Each of f-027 and f-029 has a test that a mutant fails. The newline guard has
a test that its own cut makes impossible to fail.
