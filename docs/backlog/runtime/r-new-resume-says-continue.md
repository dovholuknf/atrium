# r-new-resume-says-continue. A card a room restart brings back mid-work is told to carry on

Status: not started. Owned by @runtime. Filed by the orchestrator 2026-09-30.

## What happened

The 13:22 claude-sg4 deploy (ff747683) brought every card back, as it should. Every one of them then sat at its
prompt: four directors and two workers that were mid-queue did nothing until the orchestrator typed each an order.
clint's view from the board: "why does the factory seem idle?"

A resumed conversation has its context but no turn. `atrium_wake_after_restart` covers a session that asked for it
before the restart. Nothing covers a session that was working and did not know a restart was coming.

## Wanted

- At reopen, a card whose runner was MID-TURN when the wind-down began (activity said running, or a turn had started
  and no Stop had arrived) gets one line typed once it is at its prompt: "atrium restarted at HH:MM while you were
  working. Continue from where you were. Check `git status` first."
- A card that was idle at its prompt gets nothing. It was waiting for a human, and it still is.
- Recorded on the card, so the board can show "resumed, told to continue".
- A setting to turn it off.

## Done when

A test stops a room with one card mid-turn and one idle, restarts it, and checks that only the first got the line.
