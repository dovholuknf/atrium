# r-new-review-06b876bb. Growler R2 and R3: review

Status: open. Filed by @review 2026-09-30. Read-only review of 06b876bb (merge of `claude/r-growler`, R2 and R3):
deploy holds as a room growler, reported cards as `blocked` and `question`, and reminders to the phone. Owned by
@runtime.

`GrowlHalt` generalised into `GrowlRoom` without changing what a halt does, and a hold under `growl.hold_after` is
correctly neither raised nor ended. Phone reminders go through `Notifier.Remind`, which holds them back while a
desktop tab is visible, so the phone is not told what the screen already shows.

## 1. Low. The notifier changed too, and its comment says it did not

`cardReason` is shared by the growler and `NotifyIdentity`. The case at `internal/link/notify.go:244` now lets a
reported card that has never finished a turn through (`&& report == ""`), for both. So the desktop and phone
notifier now fires `input` for such a card, where before it said nothing. The field comment at `notify.go:151` says
"the notifier reads neither: a reported card notifies as input, as before". It is probably the right behaviour,
since a session that reported blocked is waiting on somebody. Say so in the changelog, and fix the comment.

## 2. Low. The ask is read out of free text

The ask is whatever follows the last `needs: ` in the card's recap (`notify.go:227-230`). A recap whose own prose
contains `needs: ` gives the wrong ask on the growler. The report's ask is a field when it arrives at `/finish`, so
carry it on the card as one rather than parsing it back out.

## 3. Low. `growl.since` moves when it cannot be written

`growlSince` (`internal/link/growl.go:235`, landed in 5edc1821 with the rest of R3's since rule) returns without
caching when `SetSetting` fails. Every later call picks a new `since` of "now", so questions asked in between are
read as older than growlers and never raise. Cache the value in memory either way, and try to write it again next
time.

## Tests

Same run as `r-new-review-5edc1821.md`.
