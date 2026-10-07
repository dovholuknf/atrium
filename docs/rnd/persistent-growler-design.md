# The persistent growler: an alert that stays until you act on it or dismiss it

Status: built. Growlers on the board and the phone (`internal/api/web/js/growl.js`, `internal/api/web/m/js/growl.js`).

Origin: design, 2026-09-30, @rnd. Item `docs/backlog/ui/u-new-persistent-growler.md`, clint's priority. Built by @ui
(the board and the phone) and @runtime (the hub's state). Nothing here is built.

## 1. The answer in eight lines

1. **One alert model with three levels**, not a fourth surface. A toast is news and leaves. A growler is something
   waiting on you and stays. The toast log is the record of both. The modal is not an alert level at all (section 8).
2. **The hub decides what earns a growler**, from the card list every room already announces to it. It already works
   out, per card, why a human is wanted (`NotifyIdentity` in `internal/link/notify.go`). The growler reuses that.
3. **The hub holds the state.** One row per growler in the hub's store, keyed by the same identity the notify trigger
   uses. Dismissed on the laptop means dismissed on the desktop and the phone, because they all read the one row.
4. **It ends by itself when its reason ends.** A permission answered anywhere, questions replied to, a halt cleared:
   the growler resolves on every screen. Dismiss and snooze are for the ones you choose not to handle yet.
5. **A stack, not a pile.** One growler drawn in full, the rest as a count under it, most urgent and longest waiting
   on top.
6. **It lives in the toast host**, so it inherits what that host already solved: it draws over an open modal and
   stays clickable, and it moves off a terminal's input line.
7. **Unfocused, it asks for attention four ways**: the tab title, the favicon, one sticky desktop notification, and
   the card's sound. Reminders follow the operator backoff already used by the permission nag, and stop ringing after
   the two hour step while the growler stays on screen.
8. **Build in five stages**: two for @runtime, three for @ui. The first two together (R1 and U1) are usable alone.

## 2. What is there today, and what the growler builds on

Read before designing. Every piece below is reused, not rewritten.

| piece | where | what it does now | what the growler takes from it |
| --- | --- | --- | --- |
| toast | `js/toasts.js` `toast` | 9 s life, 30 s for a permission. Collapses repeats. Queues at 3 (1 on a phone). Keyed toasts turn "answered" when the subject leaves | the host, its placement over modals and terminals, `landOnAlert` for the click |
| sticky toast | `js/hubrestart.js` `hubToast` | the restart gate's two toasts. No timer, not counted in the cap, taken down by the hub | the precedent: a toast the hub owns, outside the cap |
| toast log | `js/toast-log.js` | every toast and every desktop notification, recorded per browser in `localStorage`, 200 entries | unchanged. A growler raised and a growler ended are each one log line |
| notify | `js/notify.js` `notify` | one alert per event across all atrium windows: focused window toasts, else another atrium window toasts, else one desktop notification | the form decision for a growler's raise and reminders |
| the nag | `js/notify.js` `nag` | re-alerts a waiting permission on the backoff 1, 2, 5, 10, 30, 60, 120, 240, 480, 1440 minutes | the reminder schedule. The nag's toasts stop where a growler exists (section 7) |
| desktop notification | `js/notify.js` `showNotification`, `sw.js` | sticky for a permission, approve and block buttons through the worker, closed by `reapNotifications` when the subject goes | one sticky notification per growler, tagged by the growler's id |
| seen | `js/seen.js`, `docs/runtime/seen-design.md` | the room records whether a turn was seen, and whether its Open Questions were answered or dismissed | the question reason, and the precedent for recording `via` |
| hub notify | `internal/link/notify.go`, `internal/hubstore/notify.go` | after each announcement: one reason per card (permission, question, input, finished), an identity `card \| reason \| since`, a row per card, a sink run when no desktop tab is visible | the reasons, the identity, and the presence count |
| phone bell | `js/phone-bell.js` | on a phone a toast nudges the bell instead of drawing a box | the phone board's growler is a strip, not a box (section 6) |
| modal | `js/browser-dialogs.js` `askUser`, `closeOpenDialogs` | one promise-based dialog for every question the board asks. Top layer, makes the rest inert | nothing. It stays what it is (section 8) |

Two things are missing, and they are the whole gap: **nothing on screen persists**, and **nothing about an alert is
shared between browsers**. The toast log is per browser on purpose (its header says why, and that reasoning still holds
for a log of what one screen was told). A growler is different: it is a fact about the WORK waiting on a human, not
about what a screen showed. So it belongs where the work is seen whole, which is the hub.

## 3. What earns one

A growler is for something that is blocked on a human and costs more the longer it waits. Everything else stays a
toast.

| reason | raised when | ends by itself when | urgency |
| --- | --- | --- | --- |
| `permission` | a request has waited `growl.perm_after` (default 2 min) | the request is answered, anywhere | 1 |
| `halt` | a room reports its store halted | the room reports healthy | 2 |
| `blocked` | a card reported `atrium_report` status `blocked` | the card leaves needs-input, or is replied to | 3 |
| `question` | a turn ended on Open Questions (hub reason `question`), or a report had status `question` | the questions are answered or dismissed on the card (the `?` chip's rule) | 4 |
| `deploy-hold` | a room deploy hold has outlived its expected window, or is waiting on the board's lift | the hold lifts | 5 |

**Why two minutes for a permission, and not at once.** Most permissions are answered inside a minute from the toast.
Growling at once would put a persistent box over every one of them and teach the operator to dismiss growlers on
reflex. Two minutes is the second step of the nag schedule, the point where the one toast has already been missed. The
setting is per board (hub setting), not per browser.

**Why a question growls at once.** A question has no toast that shouts: the turn ends, the `?` chip appears, and if the
operator is looking elsewhere, nothing else happens. That is the 2026-09-23 failure the seen feature was built for, and
the chip alone did not fix it.

**Not a growler, and why:**

- `input` without a report. Every worker that finishes a turn is in needs-input. A growler for each would be a pile.
  A card that needs a decision says so with a report status or an Open Questions block, and those growl.
- `finished`. News, not a block.
- Arrivals, stuck tools, silent stops, long tools, context size. News for a toast. The held launcher notices
  (`docs/changes/r-hold-notices.md`) ring the bell and stay toasts.
- **`origin:agent` cards.** Skipped for the same reason the hub notify trigger skips them: an agent launched it and the
  launcher is told. The one exception is `permission`, which the board never mutes (`quietDoer`), because a blocked
  agent is frozen whoever launched it.

`halt` and `deploy-hold` are not card reasons. The hub raises them from the room's health and its hold row, and they
name a room, not a card.

## 4. Its face and its actions

One line of title, one of body, then buttons. The buttons depend on the reason. Every growler has the last three.

| reason | primary actions | |
| --- | --- | --- |
| `permission` | **approve once**, **block** (opens the request for a reason), the command's first line | |
| `halt` | **open the room** (the rooms dashboard row with the cause) | |
| `blocked`, `question` | **reply** (a one-line field, sent as an operator message), **open** | |
| `deploy-hold` | **lift** (the hold's own lift, which asks first), **open** | |
| all | **open**, **snooze** (15 min, 1 h, until tomorrow 09:00), **dismiss** | |

**Open** is `landOnAlert`, the one function every alert click already goes through, so a growler cannot land somewhere a
toast would not.

**Approve and block** call the endpoints the perms view calls. They do not touch the growler: it resolves because the
request left, which is the same path as answering from anywhere else. So there is exactly one way a growler for a
permission ends when handled, and it cannot disagree with the perms view.

**Reply** sends the text through the existing operator message path, which counts as `message` for seen and so answers
the questions. It does not type into a terminal the operator may be typing in. For a longer reply, open.

**Dismiss is not "dismiss the questions".** Dismissing a growler says "stop showing me this". It leaves the `?` chip on
the card, because the chip is a fact about the card and the questions really are unanswered. The chip's own dismiss
stays where it is. Two buttons with one word would be one meaning per word broken, so the growler's is labelled
**dismiss this** and the tip says the chip stays.

**Dismiss has an undo** (clint, 2026-09-30). For ten seconds after a dismiss, the stack shows one line where the
growler was, "dismissed. undo", on the screen that dismissed it. Undo sets the row back to `open` and records it
(`changed_via`, as any action). After the ten seconds, the toast log's line for the dismissal carries the same undo for
as long as the reason still holds, so a growler dismissed by mistake an hour ago can still be put back. Undo is
`POST /_hub/growls/{id}` with `{"do": "reopen"}`. It answers `409` once the reason has ended, and the log row says so.

**Snooze** hides it everywhere until the time, then raises it again as if new, with a reminder. A snooze does not
survive the reason ending: if the request is answered while snoozed, it simply resolves.

**Every action is recorded** on the row: what (`acted`, `dismissed`, `snoozed`, `resolved`), when, and `via`
(`board`, `phone`, `notification`, `hub`). Not a person: the hub has no login, and a room board's login does not reach
the hub. `via` plus the tab id is what seen records too, and it answers "where did this go" without inventing an
identity.

## 5. The stack

- **One drawn in full, the rest as a count.** Under the top growler: a strip reading `+3 more: 2 permissions, 1
  question`. Clicking the strip opens the stack in place, all growlers as compact rows with their primary action.
  Clicking again folds it.
- **Order: urgency, then longest waiting.** Urgency is the column in section 3. Inside one urgency, the oldest first,
  because the agent frozen longest is the one costing most. Newest-first was considered and lost: it rewards the
  latest arrival and buries the one somebody already missed twice.
- **Above the toasts in the same host, outside the cap.** The toast cap (3, 1 on a phone) is about announcements, and a
  growler is not one. It takes the sticky exemption the restart gate's toasts already have. The toasts still queue under
  it.
- **Max height, then it scrolls.** Expanded, the stack is at most 50 percent of the window high.
- **No growler for a subject that already has one.** The identity is the key, so a republished card is the same
  growler, and a second waiting spell on the same card (new identity) replaces the first rather than stacking beside
  it.

## 6. One state, every screen

### Where it lives

The hub. A new table in `internal/hubstore`, added at the END of the migration slice:

```sql
CREATE TABLE IF NOT EXISTS growl (
  id          TEXT PRIMARY KEY,               -- the notify identity: room|card|reason|since, or room|halt|since
  room_id     TEXT NOT NULL,
  card_id     TEXT NOT NULL DEFAULT '',       -- empty for halt and deploy-hold
  reason      TEXT NOT NULL CHECK (reason IN ('permission','halt','blocked','question','deploy-hold')),
  title       TEXT NOT NULL,
  body        TEXT NOT NULL DEFAULT '',
  subject     TEXT NOT NULL DEFAULT '',       -- the permission id, for approve and block
  raised_at   TEXT NOT NULL,
  state       TEXT NOT NULL CHECK (state IN ('open','snoozed','dismissed','acted','resolved')),
  until       TEXT NOT NULL DEFAULT '',       -- snooze end
  reminders   INTEGER NOT NULL DEFAULT 0,
  changed_at  TEXT NOT NULL,
  changed_via TEXT NOT NULL DEFAULT '',
  changed_tab TEXT NOT NULL DEFAULT ''
);
```

Postgres portable like every other table: text keys, RFC3339 text times, `CHECK` for the enum. Rows not `open` or
`snoozed` are pruned after seven days, the `notify_sent` rule.

### Who writes it

The hub, from the same `Announced` call the notify trigger already runs after each room announcement, plus a 30 second
ticker. The ticker is needed because announcements happen on change: a permission that has waited 1 min 59 s and then
nothing changes would never be promoted to a growler without one. The ticker also ends snoozes and drives reminders.

The derivation is `NotifyIdentity` extended, not a second function beside it. It gains the `blocked` reason (from a
report status the room must first publish, stage R2) and the permission age threshold. `halt` comes from the health the
hub already merges pessimistically in `fanout.go`. `deploy-hold` comes from the hold row once the room deploy hold is
live (`docs/rnd/room-deploy-hold-design.md`).

### How screens hear it

- `GET /_hub/growls` answers the open and snoozed set.
- `POST /_hub/growls/{id}` with `{"do": "dismiss" | "snooze" | "acted" | "reopen", "minutes": n, "via": "...",
  "tab": "..."}`.
  A stale id (the reason already ended) answers `409` with the row, so a click on an old screen says "already
  handled" rather than nothing.
- The hub event stream gains a `growls` event carrying **the whole open set**. This bends the stream's deltas-only rule
  the way `rooms` already does, for the same reason written in `events.go`: the set is small and bounded (a growler per
  blocked thing, not per card), and sending the set means a screen that missed an event heals on the next one rather
  than on a reconnect. Every tab, every browser and the phone converge on the same list inside one event.

### The phone

`/m` reads the same endpoint and the same event through `mStore`, which gains a `growls` subscription. The phone home
shows growlers as a strip above the card list, with the same actions. On the phone board (not `/m`) the growler is the
one exception to "a phone nudges the bell instead of drawing a box": a growler is a one-line strip pinned under the
header, because the bell's count is exactly the signal that did not work. It never covers the terminal's key bar.

The phone push is the hub notify sink, which already fires for `permission` and `question` when no desktop tab is
visible. It fires when a growler is raised and again on each reminder (section 7), on the same backoff, while no
desktop tab is visible (clint, 2026-09-30). The sink's `Notice` gains no field for it: a reminder is the same four
fields, and the reason reads `permission` or `question` as it did. The sink is the operator's own command, so this
reaches a phone only where one is configured. Web Push would need HTTPS and belongs to the security design.

### A board with no hub

A room served alone has no hub store. It shows no growlers and behaves as today. The live setup is a hub in front of
every room, so this costs nothing clint uses. Building the same table in the room store is named, not promised.

## 7. Getting attention while the tab is not in front

All four are driven by the hub's `growls` event, so every screen agrees on when a reminder happened.

1. **Tab title.** Today `retitle` shows `(n) atrium` for waiting plus permissions. With an open growler and the tab not
   focused, the title alternates every 1.5 s between `(n) atrium` and `! <top growler's title>`. It stops on focus.
2. **Favicon.** The atrium mark with an amber dot and the growler count, drawn on the canvas `atriumMarkURL` already
   uses. Restored when the count is zero.
3. **Desktop notification.** One per growler, through `notify` case 3 so the one-alert-per-event rule across windows
   holds. Tagged `atrium-growl:<id>`, `requireInteraction` so it stays in the action centre, logged by
   `logNotification` as every desktop path must be (the d322f71 gap). `reapNotifications` learns the growler ids and
   closes the notification when the growler leaves `open`, on every browser.
4. **Sound.** The card's tone on raise and on each reminder, through `alerting.play`, so mute and the master switch
   (item 79) still hold. The master switch holds back sound and notifications, never the growler on screen: it is
   state, not an interruption.

**Reminders.** The hub counts them on the row and emits `growls` with a `remind` id on the operator backoff already
used by the nag and `EscalationBackoff` (1, 2, 5, 10, 30, 60, 120 minutes after raise). After the 120 minute step it
stops ringing and stays on screen. Each reminder re-sends the desktop notification under the same tag (`renotify`) and
plays the tone once, in the one window `notify` picks.

**The nag steps aside.** Where a permission has a growler, `nag` no longer toasts: the growler is on screen, and the
reminder is the growler's. Before two minutes the nag's first toast at one minute still happens, as today.

**Keyed toasts step aside.** A toast whose key names a subject that has an open growler is not drawn. It is still
logged. Otherwise the growler and a toast would say the same thing in two boxes.

### A popped-out card

Added 2026-09-30 after u-popout-notify (1a1f9362), which gave a pop-out its own switch and mute, kept per card in
`localStorage` (`atrium.notify.off.card:<id>`, `atrium.sound.card:<id>`). Those are about the interruption in one
window of one browser. The growler's state is on the hub. They do not conflict, and three rules keep it that way:

1. **Drawing: the board draws every growler, a pop-out draws only its own card's.** A growler is state, like the
   card's badges and counts, which the board keeps for a popped-out card (the header of `js/notify.js`). So the board's
   stack includes growlers for popped-out cards, and the pop-out's stack holds only the growlers for the card it shows.
   Neither switch hides a growler anywhere (u-popout-notify, point 4).
2. **Ringing: a popped-out card's reminders belong to its pop-out**, the same one-owner rule `notify` keeps for its
   alerts. The pop-out plays the tone and raises the desktop notification, so its switch and mute hold them back, as
   the master switch does on the board. The board does not ring them in its place. Where the pop-out's switch or mute
   is on, the board's growler for that card says so on its face ("muted in its window"), so a reminder held back is
   visible rather than simply absent. The pop-out writes nothing new for this: the board reads the two per-card keys
   in the same `localStorage`.
3. **The phone is not a window.** The hub's phone reminders (section 6) never see a browser's switches, so a pop-out
   switched off does not stop them. Dismiss and snooze are the controls that quiet a growler everywhere, because they
   are on the hub.

The title and favicon (section 7) are per window: a pop-out's own title alternates for its card's growler unless its
switch is off.

## 8. The toast, the toast log, the modal: one model, recommended

**Recommended: one alert model, three levels of persistence, the modal outside it.**

| level | what it means | lasts | shared | surface |
| --- | --- | --- | --- | --- |
| toast | something happened | seconds | no, per screen | `#toasts` |
| growler | something waits on you | until handled, dismissed, or its reason ends | yes, hub | `#toasts`, pinned above |
| log | what you were told | 200 entries | no, per browser | the bell's tray |

The growler is the toast host's pinned section, not a new layer. That keeps the one piece of hard-won placement
(`raiseToasts` putting the host inside the topmost modal, because nothing else can draw over the top layer and stay
clickable) instead of solving it twice. The log records both levels, so "what was I told" still has one answer.

**The modal is not an alert and must not become one.** A modal is the board asking you something to finish what you
started: a confirmation, a form, a choice. It makes everything else inert, which is right when you clicked and the
board needs one answer to go on, and wrong for anything that arrived on its own. An arriving alert as a modal would
block the perms view it is asking you to use, and on the phone it would block the whole screen. So: nothing that
arrives by itself ever opens a modal. A growler's **block** and **lift** buttons may open one, because then you
clicked.

**Rejected: a separate growler surface** (its own corner, its own layer). It would need its own answer to the modal top
layer, its own phone rule and its own placement over terminals, all already solved in the toast host, and two corners
of alerts is one more place to look.

**Rejected: a growler as a toast with no timer.** That is what the restart gate does, and it works because the hub owns
that state. Without the hub row, a toast with no timer stays on one screen and nowhere else, which is the half of the
request (dismiss once, cleared everywhere) it cannot meet.

## 9. Failure and what it must not break

- **The hub store failing is the hub's existing halt.** Growlers stop with everything else. Toasts and the log still
  work in the browser, so the operator is no worse off than today.
- **A growler action never blocks the thing it acts on.** Approve and block go to the room the way the perms view does.
  The growler row is not in that path, so a hub store problem cannot stop a permission being answered.
- **The derivation must not slow `Announced`.** It is one more pass over the same cards in the same transaction the
  notify trigger opens. The ticker does no work when no row is due.
- **A room offline is not a reason ended.** A card that drops out of the cache keeps its growler, in `open`, marked
  "room offline" in its body. Resolving it would clear a real block because a network blinked. This is the
  `notify_sent` rule: a row is not deleted because its card left the cache.

## 10. The build

Sizes: S is up to half a day for one worker, M is a day, L is two or more.

| stage | owner | size | what | depends on |
| --- | --- | --- | --- | --- |
| R1 | @runtime | M | hub `growl` table (migration at the end of the hubstore slice), derivation from `NotifyIdentity` for `permission` and `question`, the 30 s ticker (perm age, snooze end, reminders), `GET` and `POST /_hub/growls`, the `growls` event with the whole set, `halt` from the merged health. Go tests for the identity, the 409, snooze, prune, and a room going offline | nothing |
| U1 | @ui | M | the pinned stack in `#toasts`: face, actions (approve, block, reply, open, snooze, dismiss this, and undo on the stack and in the log), expand and fold, order, the `409` message, keyed toast and nag suppression, log lines for raise and end. Headless Playwright with mocked `/_hub/growls` and a mocked event | R1's API shape (can start on the mock the day R1's shape is agreed) |
| U2 | @ui | S | unfocused attention: title alternation, favicon dot and count, sticky desktop notification per growler, `reapNotifications` by growler id, tone on raise and remind | U1 |
| R2 | @runtime | S | the room publishes a card's last report status in its state payload, the hub derives `blocked` and report `question`. Room side, so it needs a room deploy | R1 |
| U3 | @ui | S | `/m`: `mStore.growls`, the strip above the card list, same actions. The phone board's pinned strip under the header | U1 |
| R3 | @runtime | S | `deploy-hold` from the hold row. Only once the room deploy hold is live | R1, the deploy hold |

**R1 then U1 is usable alone**: permissions and questions persist and dismiss everywhere on a desktop. U2 is what makes
it get attention with the tab behind something, which is most of clint's ask, so it goes straight after. R2 is the one
stage that needs a room restart, and it can wait for the next room deploy rather than asking for one.

Test plan: a new section under the most recent letter in `docs/test-plan.md`, by the stage that lands it. The scenarios
that must pass: dismiss on one browser clears a second browser and the phone inside one event, a permission answered
from the perms view resolves its growler everywhere, a snooze comes back, a room going offline does not resolve, the
desktop notification closes on the other browser when handled on one, and a growler draws over an open settings dialog
and its buttons work there.

## 11. Answers, and questions for later

clint answered the four questions on 2026-09-30, and the sections above carry the answers.

1. **The permission threshold: 2 minutes** ("just guess").
2. **Reminders to the phone: yes, on the backoff** ("would be sick"). Section 6.
3. **`input` without a report: no growler** ("if it's done it's done").
4. **Keep the `?` chip apart from dismiss**, and a dismissed growler offers an undo ("oh fuck, put it back"). Section 4.

For later: the phone path is the operator's own notify command, and none is confirmed configured on the hub. Is one set
up, and does he want ntfy? Web Push goes to the security design, since it needs HTTPS.
