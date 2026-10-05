# u-new-no-question-reminders: a question alerts once, it does not nag

Status: HELD (pause). Owned by @ui, hub side with @fabric (`internal/link/growl.go`). Filed by the orchestrator
2026-10-02, from clint.

Screenshot: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261002-082338-pasted.png` on sg4. Three desktop
notifications stacked: a reviewer card, win32crypto-e2e and the orchestrator, each "asked a question".

clint: "these fucking nags are getting old. is this active work or some kind of 'reminder'? ... i don't need the
agent continually nagging me if that's the case"

## Why it happens

The agents are not asking again. The hub's growler re-raises every open question on `growlBackoff` in
`internal/link/growl.go:84`: 1, 2, 5, 10, 30, 60 and 120 minutes after it was raised, as a desktop notification and
to the phone (line 511, added 2026-09-30). There is no setting to turn it off. Only snooze or dismiss, one growler at a
time.

## Wanted

- A question (`ReasonQuestion`) and a ready/done alert raise ONCE: one notification, one sound, one phone push. No
  reminders. It stays in the growler list and the log until answered or dismissed.
- A permission (`ReasonPermission`) keeps its reminders, since it blocks the card.
- A setting for the reminder ladder per reason (questions off by default, permissions on), so this is a choice and
  not a constant.
- A card that ended or exited takes its growlers with it, so a finished reviewer cannot keep asking.
