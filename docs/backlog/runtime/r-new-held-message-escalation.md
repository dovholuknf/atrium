# r-new-held-message-escalation. A message or a card that has waited too long escalates on its own

Status: not started. Owned by @runtime, board half @ui. Filed by the orchestrator 2026-09-30 on clint's word: "we
need some sort of escalation policy if it's been waiting forever like this". Design first, reviewed by @rnd.

## What happened

@ui ran one turn for about 1.5 hours and reached 253k context. Four messages sent to it `when: done` (three from
@runtime, one from the orchestrator) sat held for 1h29m, because a held message waits for the turn to end and the turn
never ended. The context cycle (`POST /new-context`, waiting for a status that is not `running`) never fired either,
for the same reason, and the job waiting on it timed out after an hour. Nothing told anyone. clint found it by hovering
a card (screenshot `.atrium/incoming/20260930-105832-pasted.png`).

## Wanted: an escalation ladder, each step once, each step recorded on the card

1. **A held message older than a limit (default 15 minutes) stops waiting for the turn.** It is delivered at the
   session's next tool call, the way a queued message already is in the permission chain (step 2), framed as "this
   waited N minutes for your turn to end". `when: done` means "do not interrupt a thought", not "wait forever".
2. **A card past its context threshold that is still mid-turn** gets one line at its next tool call: "you are at
   253k, finish the step you are on, commit, write your handoff, and end your turn". r-029 (auto new-context) only
   acts at a turn end, which a runaway turn never reaches.
3. **A turn running longer than a limit (default 45 minutes)** is flagged on the card and notifies its launcher once.
   The launcher decides. For a card tagged `atrium:hold-notices` the notice is held like the rest (r-hold-notices).
4. **The board shows it without hovering.** A held-message count and its age, and "over context, mid-turn", on the
   card face, never hover-only (the u-032 rule).

## Second case, seen the same hour

@ui was blocked for 10 minutes inside ONE foreground Bash call (a wait script on its own background UI suite), 1h47m
into its turn. The orchestrator's immediate message was typed into its input line but not sent, because Claude Code
only takes input between tool calls. Steps 1 and 2 ride tool calls, so they would not have fired either. The design
must cover a single long tool call: at least say so on the card ("one tool call running 10m"), and tell the launcher.

## Open for the design

- The limits: settings, per card or daemon-wide, and their defaults.
- A long legitimate turn (a 40-minute test run): how the ladder tells it from a runaway. Tool calls still arriving
  means it is working, and step 1 and 2 ride those calls, so they cost nothing when it is fine.
- Whether step 2 repeats, and how often, if the card ignores it.
