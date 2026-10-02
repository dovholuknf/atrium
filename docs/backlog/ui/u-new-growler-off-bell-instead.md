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

## Stage 1 built (claude/u-growler-off)

### Design note

- **Where the setting lives: per browser.** `localStorage` key `atrium.growler` (`"1"` is on, absent is off), read on
  every ask, with a `storage` listener so the other windows of the browser redraw and repaint the checkbox. Reasons:
  the board already keeps its display prefs per browser (notify off, sound, density), the daemon refuses display
  expressions, and a daemon-wide switch needs a new endpoint and a store. No reason to go daemon-wide turned up,
  except the hub reminder ladder below, which only a daemon-side switch can reach.
- **Where the toggle is.** Settings dialog, under "turn notifications off": "show the floating growler".
- **What off does** (all in `js/growl.js`): `growlDrawn()` returns nothing, so the desktop stack, the phone strip, the
  favicon dot and the title flash all go, as does `growlHas`, which hands the permission nag back. `growlOfferUndo`
  and the phone undo line draw nothing, and switching off takes down any undo toast already up. `growlSay` skips the
  growler tone. Everything else is untouched: the set is still tracked, every raise, snooze, end and hub reminder is
  still logged (so the bell count rises), and the desktop notification still goes through `alerting.notify`, which
  follows the notify setting and focus rules unchanged. `growlRecheck` and `growlApply` redraw through `growlDrawn`,
  so no path draws one. On restores today's behaviour, drawing at once what is open.
- **A permission request with the growler off:** no growler. It is carried by the ordinary permission path that
  exists today: the keyed permission toast (which stays until answered), the permission tone, the nag backoff, the
  desktop notification, the perms tab badge and its dialog, and the bell. The hub's `growler: permission` line is
  also logged, so the bell can show a permission twice. Nothing in the perms tab, badge or dialog was touched.
  Covered by `growlOff`.
- **Not changed:** the ordinary toasts (a ready alert, a stuck card) are not growlers and behave as before; with the
  growler on they are deduped against it by `growlHas`. The phone page (`/m/`, `m/js/growl.js`) has its own growler
  and does not read this setting. It is a separate page, so it is left for the director to decide.

### For @fabric: stopping the hub's reminder ladder (not built, Go is not ours)

`internal/link/growl.go`: `growlBackoff` (:81-84) re-raises each open growler 1, 2, 5, 10, 30, 60 and 120 minutes after
`RaisedAt`. The tick at :499-511 adds the id to the event's `remind` list (the board turns that into a tone and a
desktop notification) and, for `permission` and `question` only, calls `g.phone` (the phone push). The hub does not know
the browser setting, since it is per browser and presence only says which tabs are visible. Options:

1. **Stop reminding questions (and blocked) altogether**, the u-new-no-question-reminders text: in the loop, skip
   the reminder when `r.Reason` is `ReasonQuestion` or `reasonBlocked` (`continue` before `due`), or give those a
   one-entry ladder. About 5 lines plus the test change in `growl_test.go`. Permissions keep their ladder. Best fit.
2. **A per-reason setting** (the same text wants one): a hub setting map reason to on or off, defaulting question and
   blocked off, read in the same loop. Needs a hub setting, a `GET/PUT` on `/_hub/` and a settings row. About 60 to
   100 lines with tests.
3. **A daemon-wide growler switch** the board writes (`PUT /_hub/growler`), and the loop skips non-permission
   reminders while off. Same size as 2, and it makes a per-browser choice daemon-wide, which is the wrong way round.
4. **The board tells the hub on the stream open** (`?growler=0`, like `?tab=`) and the hub skips reminders only when
   every connected tab said off. Small on the board side, but it needs per-tab state in the hub and a
   no-tabs-connected rule. About 40 lines.

Recommendation: option 1 now (it is also what the filed item asks for), option 2 only if clint wants questions
reminded on some boards. Until then the board still shows each reminder as a desktop notification (per the notify
setting) and logs it, and rings nothing.
