# u-new-growler-off-bell-instead. Turn the growler off, keep the alerts in the bell

Status: stage 1 approved by clint as a pause exception, 2026-10-02. Stage 2 is design only. Owned by @ui.

clint, with a screenshot of the "dismissed: orchestrator asked 4 questions / undo" growler: "let's disable this growler
for now... it's been more annoying than not. could we put that functionality inside the bell/notification thing
instead?"

## Stage 1, build now, small

A board setting "growler" (on/off), default OFF. Per browser (localStorage, like the other view prefs) or daemon-wide:
say which and why in the design note. Off means no floating growler, no stack, no undo bar, and no growler sound for
questions and ready alerts. Nothing is lost: every alert still lands in the bell's notification log, the bell count
still rises, and desktop notifications follow the existing notify setting.

Permissions: keep whatever makes a blocked card impossible to miss (the perms tab badge and the bell). Say exactly what
a permission request looks like with the growler off.

Also stop the hub's reminder ladder from re-raising questions while the growler is off (internal/link/growl.go
growlBackoff), or say why that must wait for @fabric.

## u-new-no-question-reminders (filed on sg4)

The hub re-raises every open question 1, 2, 5, 10, 30, 60 and 120 minutes after it was raised, as a desktop
notification and to the phone (growl.go:511). There is no setting. Wanted: questions and ready alerts raise once,
permissions keep reminders, a setting per reason, a card that exits takes its growlers with it.

## Stage 2, design only

Move the growler's actions into the bell panel: reply inline, open, snooze, dismiss, per row, newest first, a count
per card instead of one row per alert, and no reminders for questions.
