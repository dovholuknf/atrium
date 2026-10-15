# A say that was typed is not a say that was read (r-say-typed-sits-in-claude-queue)

Status: design by r-say-typed-queue-design, 2026-10-15, against `claude/main` 91684fac. Nothing here is built. No code
was changed and no Claude Code was driven, so every claim about Claude Code's own behaviour below is marked
**unverified** until stage S-0 measures it.

## 0. The design in one paragraph

`delivered: terminal` has always meant "atrium wrote the bytes and Enter", and a sender reads it as "the worker has it".
For a Claude Code runner in the middle of a turn those are different: Claude Code puts the line in its own queued-message
box and reads it later. Main already stopped the commonest lie (commit d5325d08, 2026-10-09): a say **from a session** to
a claude runner that is mid-turn and has been heard on a hook is no longer typed, it is queued and the PreToolUse or Stop
hook carries it. Three holes are left. A session blocked inside one long tool call fires no hook, so the hook route can
wait the whole 45 minutes. Several typed paths still write mid-turn into the box (the operator's own channel, the aged
`done` escalation, the retry backoff, a runner with no hook yet). And nothing in atrium ever learns that a typed line was
read, so there is no honest state between "typed" and "delivered". The recommendation: report `typed-queued` for any
line typed into a mid-turn claude runner, promote it to `delivered` only on proof (the transcript's `queued_command`
attachment, the UserPromptSubmit hook, or a hook handing it over), and use Claude Code's force-send chord
(Ctrl+X Ctrl+S) as the escalation, after a spike proves which versions have it and that it does not interrupt a running
tool.

## 1. Problem and evidence

clint, 2026-10-09 about 07:45. A say to `ops-land-1009` (`claude-sg4`) at 11:32Z with `when: immediate`; atrium answered
`delivered: terminal`. The worker was in a 45 minute turn. Claude Code put the typed text in its own queued-message box
("Press up to edit queued messages") and did not read it at the next tool call. It was still queued after the blocking
test was killed and new tool calls ran. clint pressed Ctrl+X Ctrl+S on the card to force it in.

Two separate failures sit in that report:

1. **The answer was untrue.** `terminal` told the launcher the text was in. It was in a box.
2. **The text was not read when the model next acted.** The report says the box was not drained at the next tool call and
   not after the test was killed. That contradicts the assumption in `saywhen.go` and `runnerprofile` ("Claude Code
   queues typed input mid-turn and reads it at its next step"). **Unverified**: one observation, no Claude Code version
   recorded. S-0 has to reproduce it and record the version, because every option below depends on whether the box ever
   drains by itself.

The say tool's own text promises that an immediate say reaches a busy worker "EVEN MID-TURN". A launcher cannot stop a
worker with it, which is the one thing a launcher most needs.

## 2. What exists on main

Read for this: `internal/daemon/messages.go`, `saywhen.go`, `peers.go`, `seen.go`, `pendinginject.go`, `escalate.go`,
`supervisor.go` (`injectPeerIf`), `activity.go`, `replies.go`, `internal/store/say.go`, and commits 6b37779a, 701b83f5,
6e2b60ba, 0e0178da, 666c7a2e, 87e92c3b, d5325d08.

### 2.1 Already solved

| piece | what it does | where |
| --- | --- | --- |
| **A session's say to a mid-turn claude is queued, not typed** (d5325d08, same item, 2026-10-09 09:26, shortly after the report) | `carriedByHooks`: sender is a session, `when` is immediate, `act.midTurn`, runner is `claude`, a tool or stop hook has been heard. The typed branch is skipped, `deferPeerInjection` is skipped, the answer is `queued`, and the PreToolUse hook (permission chain step 2, `takeMessages`) or the Stop hook carries it | `saywhen.go:carriedByHooks`, `messages.go:~688` |
| Every delivery path clears the queued mark, and the tip names the escalation (6b37779a, merged by 666c7a2e) | a delivered message no longer reads as queued in the peek or on the card chip | `messages.go` (the mark helper near line 106), `keep.go`, `pendinginject.go` |
| `launcher_told` says queued or relayed, and a done is proven typed to an orchestrator (701b83f5) | the launcher is not told "told" for a message that is only queued | `finish.go`, `ctl_end.go` |
| Fyis and reports to holding cards are delivered at turn end, answered `queued` not `held` (6e2b60ba) | | `finish.go` |
| Queued says are not lost (0e0178da), typing queues during a new-context cycle (87e92c3b) | | `pendinginject.go`, `newcontext.go` |
| The typing gate | empty line, 2 s of quiet, no dialog, under the input lock; a closed gate queues and the injector retries on a backoff | `supervisor.go:injectPeerIf`, `pendinginject.go` |
| A launcher's say to its own worker is typed as plain keystrokes headed `your launcher @name instructs you:` (r-launcher-say-reads-as-untrusted, 2026-10-12) | applies when the typed branch is taken | changelog `2026-10-12-launcher-say-trusted.md` |
| The say ledger | a row per session-to-session say with states `queued`, `delivered`, `held`, `handed`, `unconfirmed`, ... and channel `terminal`, `hook`, `stop` | `store/say.go` |
| A transcript line for a consumed queue entry | `replies.go:queuedPromptOf` already parses `{"type":"attachment","attachment":{"type":"queued_command",...}}`, "a message typed while a turn was running". It is the only evidence atrium has that Claude Code drained the box | `replies.go:694` |
| A peer's submitted prompt is recognised when UserPromptSubmit fires | `promptWasPeer`, 30 s window | `seen.go`, `supervisor.go` |

### 2.2 What is still wrong

- **G1. One long tool call starves the hook route.** `carriedByHooks` hands the message to a hook that does not fire until
  the next tool boundary. The 45 minute blocking test in the report is exactly that case. d5325d08 made the answer honest
  (`queued`) and left the wait. A "stop now" still does not stop anything for the length of the tool.
- **G2. Typed-mid-turn paths remain.** `carriedByHooks` needs `from != ""`, so the operator's own channel and any caller with
  no `from` (a script, `atrium_say` from a non-session) still type mid-turn and answer `delivered: terminal`. The
  retry backoff (`deferPeerInjection`) and the **escalation** (`escalatesByTyping`: a `done` message aged past
  `escalate_held_after`, 15 minutes, is typed mid-turn) write into the same box with the same blind spot. `carriedByHooks`
  also needs a hook to have been heard, so a fresh runner is still typed at.
- **G3. No read state.** The ledger and the answer have `queued` and `delivered`. A typed line is recorded `delivered` with
  channel `terminal` the instant the bytes go out, for an idle runner (where that is nearly true) and for a busy one
  (where it is not).
- **G4. The say tool text** still says the text is typed "EVEN MID-TURN" and that `terminal` is an answer. It now misleads
  in the other direction for claude.
- **G5. `f-say-force`** (`atrium_say force:true`, in flight on sgg) has no defined meaning yet. It needs one that this
  design gives it (section 4, D4).

Not a gap: `queued` + `when: done` for an idle-gated runner, and codex, which steers typed input into the active turn
(profile note, codex-cli 0.156.1). The design does not touch codex.

## 3. Options

### A. Do nothing more than d5325d08

Hooks carry it. Cheap, already shipped. Leaves G1: a worker inside one long tool is unreachable for its whole length, and
G2. The honest answer helps a launcher decide to wait, it does not let it stop a worker.

### B. Type, then send the force-send chord (Ctrl+X Ctrl+S)

After typing into a mid-turn claude runner, write the two control bytes `0x18 0x13`. clint's report is the only
evidence the chord exists and does what is wanted. **Unverified**: which Claude Code versions have it, whether it is
configurable or a default binding, whether it injects the box text at the next step or interrupts the running tool, and
whether the model then reads it before the long tool returns (it cannot: a tool call is not interrupted by a message, so
the chord only helps if it cancels or abandons the tool, which is the opposite of the requirement). S-0 must answer the
last point first, because it decides whether the chord is useful at all for G1. For a worker between tool calls the box
already drains by itself on the observed-unverified path, so the chord's value is the "the box did not drain" case.
Trade-offs: terminal-protocol coupling to an undocumented chord, version drift, a stray `0x18` into a dialog or an
editor line (`escstate.go` already treats `0x18` as a cancel in escape parsing, so a misdirected one is not harmless),
and it must run under the same input lock and gate as the paste.

### C. Hook only mid-turn, type only when idle (this is d5325d08, widened)

Extend `carriedByHooks` to every sender (the operator's channel included), drop the "a hook has been heard" condition for
a claude runner that atrium started with hooks it wrote, and make the escalation use the hook route too. Never type into a
mid-turn claude. This closes G2 and keeps one mechanism. It does not fix G1. It does fit the repository's rule that a peer
is queued and delivered by a hook, and avoids the chord's coupling.

### D. Interrupt: Esc then the text

Esc stops the turn and the typed text becomes a fresh prompt. That does deliver, and it destroys the tool, the 45 minute
test the worker was waiting on. A "stop now" from a launcher might want it, a "when you can, note X" must not have it.
Reject as a default, keep as a possible separate explicit verb (out of scope here).

### E. Do both: C by default, B as the escalation

Hooks carry it. If the hook has not carried it within a short bound and the runner is provably between steps or the sender
asked for force, type it and send the chord. Proof of reading (D3) is what closes the loop in every case.

The recommendation is E, staged so C lands first, because C fixes the lie everywhere and B waits on a spike.

## 4. Decisions

Nothing here is decided. Each answer is recommended, not yet decided. "All as written" answers every one.

1. **Recommended, not yet decided: the sender hears `typed-queued`, never `terminal`, for a line typed into a claude
   runner that is mid-turn.** `delivered` is reserved for a line the model demonstrably read, or typed into an idle runner
   (its prompt submit is the read). The answer gains `read: false` and `state`. The old strings stay valid for every
   other path, so `launcher_told` and the CLI keep working (S-1 updates both).
2. **Recommended, not yet decided: C first.** A claude runner atrium owns is not typed into mid-turn by anyone's say but the
   operator's keystrokes, the operator's channel (`from == ""`) included, unless the sender passes `force`. Hook carry
   applies to every sender, and a runner whose hooks were written by atrium counts as hook-capable without waiting to be
   heard. The retry backoff and the escalation use the same rule.
3. **Recommended, not yet decided: read proof comes from three signals, in this order.** (a) a hook handing the message to
   the model (`takeMessages`: already marks it delivered, channel `hook` or `stop`), (b) the `queued_command` transcript
   attachment whose prompt text matches a typed message, already parsed by `queuedPromptOf`, and (c) a UserPromptSubmit
   whose text matches. A matching message moves `typed` to `delivered` and publishes the card. Matching is by the clean
   text, which the pending record already holds, never by position. A typed message with no proof after
   `typed_unread_after` (default 5 minutes, a setting like `escalate_held_after`) is shown as stuck on the card chip and in
   `atrium_peers`, not re-typed.
4. **Recommended, not yet decided: the force chord is the escalation and the meaning of `force: true`, gated three ways.**
   Only for a claude runner whose version S-0 proved good, only after the text is typed and the proof signals have not
   come within a bound (default 20 s), or at once for `force: true`, and only with the line empty and no dialog open
   under the same lock as the paste. Without a proven version `force` is refused with the reason rather than sent blind.
   `force` is allowed to a session's launcher for its own worker and to the operator, not to a peer, matching the
   exit-force rule in `control_mcp.go` that a force on a card that is not yours is recorded and refused for a director.
5. **Recommended, not yet decided: a worker inside one long tool call is reported as such instead of waited on.** Say answers
   gain `blocked_in_tool: <seconds since last hook>` from the activity tracker, so a launcher sees that nothing carries
   its text until the tool returns and can choose `force`, wait, or kill the tool itself. No automatic interrupt. Esc is
   not sent by atrium.
6. **Recommended, not yet decided: gate on a runner profile field, not the string `claude`.** `carriedByHooks` compares
   `t.Runner` to `"claude"` today. Add `QueuesMidTurn` to `runnerprofile` (claude true, codex false because it steers) and
   a per-row override like `mid_turn_input`, so a second runner that queues is a data change.
7. **Recommended, not yet decided: the Claude Code version is recorded per card and the chord table is data.** The version
   comes from the first hook payload or `claude --version` at launch, stored on the card, with a table of
   `version range -> has chord` in the runner profile. An unknown version means no chord. This is the answer to "which
   versions have it" staying current without a code change per release.
8. **Recommended, not yet decided: the escalation of an aged `done` message stops typing mid-turn for claude.** It is
   carried by the hook with its `waited N minutes` line (already done on the hook path in `takeMessages`), and the typed
   escalation survives only for runners where typing is read (codex). If the chord is proven (D4), the escalated typing
   may add it.
9. **Recommended, not yet decided: the say tool text and `docs/runtime/agent-messaging.md` are rewritten in the same stage
   as the answer change.** "Neither is a reply" stays. The new text says an immediate say to a busy claude is carried at the
   next tool call, that a long tool delays it, and that `typed-queued` means in the box, not read.
10. **Recommended, not yet decided: ship S-0 and S-1 together as one worker and hold S-3 until S-0's table exists.**

## 5. The staged plan

### S-0. Measure Claude Code (research, no atrium code)

- **What:** on one live claude runner, record: the Claude Code version; whether a line typed mid-turn is read at the next
  tool boundary, at the end of the turn, or only on a manual action (the incident says the last); whether Ctrl+X
  Ctrl+S is bound by default, in which versions, and whether it is user-rebindable; what it does when a tool is running
  (waits, or abandons the tool); what it does with an empty box and with a dialog open; whether a UserPromptSubmit hook
  and a `queued_command` transcript line are written when the box drains, and with which text and timestamp.
- **Where:** a scratch repo and a worker, results in `docs/terminal/queued-box-findings.md` with the version table.
- **Size:** half a day.
- **Acceptance:** the table exists with versions; one sentence for each of the five questions; a yes or no on "the chord does
  not interrupt a running tool". A "no" removes S-3 and the plan stops at S-2.

### S-1. Honest answer everywhere (D1, D2, D6, D8, D9)

- **What:** `QueuesMidTurn` on the profile and harness row; `carriedByHooks` generalised to every sender and to hook-capable
  runners not yet heard; the operator's channel, retry backoff and escalation never type mid-turn into such a runner;
  answers use `typed-queued` where something is typed and not yet read; say tool and the CLI text updated.
- **Where:** `saywhen.go`, `messages.go`, `escalate.go`, `pendinginject.go`, `internal/runnerprofile/profile.go`,
  `internal/link/control_mcp.go`, `internal/link/ctl_end.go`, `docs/runtime/agent-messaging.md`.
- **Size:** medium, about a day, mostly tests.
- **Acceptance:** an integration test per path (operator channel, escalated done, retry) shows nothing is written to the
  pty while the runner is mid-turn and the answer is `queued`; an idle runner still answers `terminal`/`delivered`; codex is
  unchanged; `launcher_told` and the existing say tests pass.

### S-2. A read state and the proof (D3, D5)

- **What:** a `typed` say state between `queued` and `delivered`; matching on the `queued_command` attachment and
  UserPromptSubmit text; the peek and the card chip show typed-unread and the age; `blocked_in_tool` on the answer;
  `typed_unread_after` setting.
- **Where:** `store/say.go` (a migration added at the END of `schema.go`, tolerant of rerun), `replies.go`, `seen.go`,
  `activity.go`, the board's `terminal-list.js` chip, `atrium_peers`.
- **Size:** medium-large, one to two days.
- **Acceptance:** a typed line shows `typed-queued` until its text appears in the transcript or a hook takes it, then flips
  to delivered with no reload; a typed line the transcript never shows is flagged after the setting; a restart loses nothing
  because the row is durable and the match is recomputed.

### S-3. The chord (D4, D7), only if S-0 says yes

- **What:** a `forceSend` write of the two control bytes under `pasteMu` with the gate re-checked; the version table as data;
  `atrium_say force:true` (f-say-force) wired to it; the bounded auto-escalation after typed-unread.
- **Where:** `supervisor.go` (next to `injectPeerIf`), `runnerprofile`, `internal/link/control_mcp.go`, the say handler.
- **Size:** medium, one day plus the spike's findings.
- **Acceptance:** with a known-good version a force say lands and flips to delivered; with an unknown version it is refused
  with a reason and nothing is written; a dialog on screen blocks it; the control bytes are never sent when the line is
  not empty.

### S-4. Notes

- **What:** `changelog/runtime/` and `docs/changes/` notes for each stage, `docs/runtime/agent-messaging.md`, a line in
  `docs/runtime/say-lifecycle-design.md` for the new state.
- **Size:** small, rides each stage.
- **Acceptance:** the docs list typed-queued and force, and say what a long tool does to a hook-carried say.

## 6. Risks

- **The box may never drain on its own.** If S-0 confirms the report (no read at the next tool call, none after), then the
  hook route has the same fault: the hook hands the text to the model, so it does not depend on the box, which is why C is
  the safe base. The unconfirmed part is only the typed path.
- **The chord is undocumented.** Claude Code can rebind or remove it in a release. Mitigation: the version table, no chord
  for an unknown version, and the hook route as the floor.
- **A control byte into the wrong place.** `0x18` is cancel in escape parsing and Ctrl+X is a prefix in some line editors. It
  is sent only on an empty line with no dialog, under the paste lock.
- **Matching a typed message to a transcript line by text** can miss when Claude Code rewraps or trims, or match a
  duplicate. A miss shows as typed-unread, never as a false delivered. Duplicate texts are matched oldest first.
- **Hook-capable without being heard** (D2) can queue a message for a runner whose hooks are not actually installed. The
  existing `reachability` warning still applies and the ledger shows the row queued.
- **A long tool still delays a hook-carried say.** This design reports it and offers `force`, it cannot fix it.
- **A `from == ""` say stops typing mid-turn.** A script that relied on immediate typing into a busy claude now waits for the
  hook. That is the intended trade and is called out in the change note.
- **Related items overlap.** r-peek-shows-delivered-message-queued is the chip fix already on main (6b37779a), S-2 builds on it.
  r-launcher-say-reads-as-untrusted changed the typed text, not the route. f-say-force needs the S-3 meaning and should not
  ship a different one first.
