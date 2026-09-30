# The board and the terminal on a phone (mobile design workshop)

Status: DRAFT for review, led by @ui with @rnd consulting, run through mercurius rounds. It goes to clint to pick from
before anything past the quick fixes is built. Asked for by clint on 2026-09-29, through the orchestrator.

## Why this exists

clint used the board from his phone all day on 2026-09-29 (Brave on Android, 1080x2340, about 412 CSS px wide, over
the zrok share) and reported, in order: the terminal bouncing to the top on every keystroke, a long press pasting the
clipboard, panning left and right fighting him, no way to put the cursor where he tapped, the on-screen keyboard
covering the key bar, both title bars eating a quarter of the screen each, and no notification once the browser was
minimized. Each was fixed on its own as it came in. This doc stops fixing phone problems one screenshot at a time and
says what the phone experience should be, screen by screen, what that costs, and what ships first.

Inputs: the two screenshots in `.atrium/incoming/` (`20260929-201050` the popped-out terminal, `20260929-201158` the
board), `docs/backlog/terminal/t-003.md` (the phone never resizes the pty, option b, shipped as t-003b), @rnd's mobile
research (see "What @rnd's research says"), and @fabric's hub-side estimate for push.

## What is already shipped or shipping

These are in, or in the second phone deploy, and the design below builds on them rather than replacing them.

| What | Where | State |
| --- | --- | --- |
| The phone never resizes the pty. xterm at the pty's size, pinch zoom by font size, native pan | t-003b | live |
| The key bar: Esc, arrows, Tab, Shift+Tab, Enter, held ^C, attach | t-003b, u-016 | live |
| Focus bounce fixed: the helper textarea sits at the cursor, the page held at 0 | u-017 | live |
| Long press: held ^C with pointer capture, no paste on a long press in the grid | u-016 | live |
| Upload from the key bar paperclip, full screen terminal | u-016 | live |
| A manual pan wins over the cursor follow, a "follow" chip to go back | u-017b | deploy 2 |
| Tap on the input to move the cursor there (arrow keys, like altClickMovesCursor) | u-017c | deploy 2 |
| "✉ N messages waiting, clear the line to deliver" in an attached terminal | u-018 | deploy 2 |
| Sized to the visual viewport, so the keyboard never covers the key bar | u-019 | deploy 2 |
| The terminal header is one slim row (alias, chevron, full screen), in the pop-out too | u-020 | deploy 2 |
| The board header is one slim row (tabs, status dot, bell, chevron, AUTO when on) | u-021 | deploy 2 |

## The screens, as clint would see them

Each screen says what he sees today (after deploy 2), what is wrong with it, and the proposal. The proposals are
lettered so clint can pick them one by one.

### 0. Getting there

**Today.** He opens a zrok share URL in Brave. The browser's own address bar and bottom toolbar stay on screen: in the
screenshots they take about 20% of the height between them, before atrium draws anything.

**Proposal A, installable board.** A web app manifest (`/manifest.webmanifest`, name, icons from `icon.go`,
`display: standalone`, `start_url: /`, a theme colour per skin) and the two meta tags iOS wants. "Add to home screen"
then opens the board with no browser chrome at all. It is also the precondition for push on iOS (16.4 and later only
deliver Web Push to an installed app). Board only, hub side, small.

Caveat: the installed app is bound to the share's origin. A new zrok share name means a new install. A reserved share
name (`overlay_reserve.go` already does this) keeps it stable, so the doc recommends a reserved name for a phone.

### 1. The top of every screen

**Today (deploy 2).** One slim row: the tabs scrolling sideways, a status dot with the working count, the bell, and a
chevron that opens the rest. The tabs do not fit: at 412px the row shows "stack", "board" and "te", and everything
else is behind a sideways swipe nobody knows is there.

**Proposal B, a bottom tab bar on a phone.** The four places he goes on a phone, as icons with a label and a badge,
along the bottom where the thumb is: **needs you** (the stack filtered to waiting on him, badge = how many),
**terminals** (badge = how many working), **perms** (badge = requests), and **more** (board, history, usage, rooms,
settings as a sheet). The top row keeps only the status dot, AUTO when it is on, and the bell. The top row then has
room to name the room he is on when there are several.

The key bar and the tab bar never show at once: inside an attached terminal the key bar replaces the tab bar, and the
terminal's slim header has the way back. That keeps one bar at the bottom at all times.

Alternative B2: keep the tabs on top and shorten them to icons. It is cheaper, but it keeps the controls at the
far end of the screen from the thumb, and the icons need learning.

### 2. Needs you (the phone's home)

**Today.** The stack tab, as on a desktop. The SHOW, SORT BY and GROUP chip rows take about 40% of the first screen
before the first card, and the cards are desktop cards: a star, a mark, an age, then the title.

**Proposal C, a phone home that is the queue.** Opening the board on a phone lands on "needs you": every card whose
status is needs-input or needs-permission, with an open question, or with an unseen finished turn, oldest wait first.
The filters collapse to one line ("waiting on you · 3 · by age ▾") that opens them as a sheet. A card is two lines:
the name (alias first) and why it needs him ("asks 2 questions", "wants to run `go test`", "finished 4m ago, not
read"), with the room as a small tag only when there are several rooms.

A tap on a card opens a **card sheet** from the bottom: the recap or the last report, the open questions, and the
three things he does from a phone: **attach** (the terminal), **say** (a message box, see I), and **answer**
(the permission or the questions). The long press menu (u-016) stays as the full menu.

### 3. Permissions

**Today.** `.row.perm` is already laid out for a phone (the 900px and 480px blocks in `phone.css`): the question above
the answer, 44px buttons, the command readable. This is the best phone screen on the board.

**Proposal D, keep it, and make it reachable.** No redesign. It gets the tab bar badge (B), and a push for a
permission lands on this row (G). The approve and block buttons on a notification stay local-notification only, see G.

### 4. The terminal

**Today (deploy 2).** Opened from a card or a pop-out link. One slim header row (alias, chevron, full screen), then
the pty-sized grid with native pan and pinch zoom, the follow chip after a manual pan, the key bar at the bottom,
sized above the on-screen keyboard. A tap on the prompt moves the cursor. The waiting-messages notice at the top
right when his own typed line is holding something.

What is still wrong, from today's reports and the screenshots:

1. **Typing into xterm with a phone keyboard.** xterm reads a hidden textarea. Autocorrect, predictive text, swipe
   typing and dictation all work by composing and replacing text in a field, and xterm sees that as a stream of
   keystrokes and backspaces. Corrections land as garbage, and dictation of a sentence is unreliable.
2. **Reading a long reply.** At a readable font the pty's 120+ columns are two or three screens wide in portrait, so
   a reply is read by panning back and forth per line. Landscape is fine.
3. **Where the cursor is.** After a pan he has to find the prompt again. The follow chip helps, and a tap on the
   prompt now works, but only once the prompt is on screen.

**Proposal E, a compose box.** A native text field above the key bar, one line growing to four, with a send button.
It is where he types on a phone. Autocorrect, dictation, swipe and paste work, because it is a real field. Send
delivers the text as ONE input frame (one paste, the rule in `web/CLAUDE.md`), then Enter, exactly what typing it
would have done. Line breaks inside the text are sent as they are, inside that one paste, so a pasted block arrives
as a block and only the final Enter submits it. The key bar keys still go straight to the terminal, so menus, Esc and ^C are unchanged. A toggle on
the key bar switches between "compose" (the default on a phone) and "keys" (the xterm textarea, today's behaviour),
for vim, a password prompt, or anything that needs raw keys. Board only.

It must not fight the typed-line gate that holds peer messages (u-018): text in the compose box is not on the pty's
line, so it holds nothing until it is sent. That is better than today, where a half-typed line on a phone blocks
every queued message.

**Proposal F, a reading view.** t-003 option (a), filed but not built: a "read" toggle in the terminal header that
swaps the grid for the reply as the transcript says it (`sessionexport.go` already makes Markdown), reflowed at the
phone's width, with the live bottom rows of the screen and the compose box under it. It shares the no-resize attach,
so it never moves the pty. It is the fix for 2. It costs the most of anything here, see the table.

**3 needs nothing new** once E is in: typing happens in the compose box, and the grid follows the cursor on send.

### 5. Saying something to a session without attaching

**Today.** Only by attaching and typing, or from a desktop dialog.

**Proposal I (part of C's card sheet), say from the card.** The card sheet's "say" is the same compose box, sent
through `POST /v1/tasks/{id}/message`, the path the board already uses. That path TYPES into a terminal atrium owns,
which is right here because it is clint speaking, not a peer. It is the fastest thing to do from a lock
screen notification: tap, read the question, answer, done, with no terminal drawn at all.

### 6. Notifications while the browser is minimized

**Today.** Notifications come from the page (`notify.js`, through `sw.js`). A phone browser suspends a page in the
background within a minute or so, and then nothing arrives.

**Proposal G, Web Push from the hub.** The installed board (A) subscribes through `sw.js` with the hub's public VAPID
key. The hub generates and keeps the key pair, stores one subscription per device, and sends an encrypted push when a
card enters needs-input or needs-permission, gets new open questions, or finishes a turn nobody has seen. The payload
is the card id, its name, and why, nothing more. A tap opens that card through the path `sw.js` already has
(`openBoard`, `?land=`). Cards tagged `origin:agent` do not push by default (the item 44 rule).

@fabric's estimate: about 2 worker-days for the hub half, written on the Go standard library (crypto/ecdh, hkdf,
ecdsa, with RFC 8291's test vector pinning the bytes). The trigger lives on the hub and needs no room change: rooms
already send the hub their card list within 2 seconds of a change, and the hub diffs it against the stored copy, so a
hub restart does not push everything again. What it needs from @ui is which fields mark "question" and "finished":
`questions_at` with a non-empty `open_questions`, and `turn_ended_at` with `unseen`. A push is identified by the card
id, the reason (permission, input, question, finished) and that reason's own timestamp (`questions_at`,
`turn_ended_at`, or when the status changed). The hub stores the last identity it pushed per card and sends only
when it changes, so a room republishing the same card never buzzes twice. The board half (subscribe,
unsubscribe, a per-device switch in settings, the test notification) is about half a day.

**The costs clint pays:**

- **An outbound call.** The hub has never called out. A push goes to the browser vendor's push service (FCM for
  Brave and Chrome, Mozilla's for Firefox, Apple's for Safari). The payload is encrypted end to end, so the service
  sees only that a push happened and its size. @rnd to rule on whether this crosses the line in `docs/overlays.md`. The
  argument that it does not: atrium holds its OWN key, never somebody else's credential, and nothing comes back in.
- **Brave's toggle.** Brave for Android ships with "Use Google services for push messaging" OFF. Without it, no push
  reaches Brave at all, and the page cannot switch it on. The settings row has to say so.
- **A per-device opt-in.** Each phone allows notifications once, from the installed app.
- **iOS** needs the installed app (A) and 16.4 or later.
- **Care at rest.** The VAPID private key and the subscriptions (an endpoint plus its auth key works like a bearer
  token) are the first secrets atrium generates. They belong to atrium, so the credential rule holds, but they need
  file permissions and never leave the hub.

**Proposal G-ntfy, the fallback.** The hub POSTs the same one-line event to an ntfy topic URL set in the hub's
settings. About half a worker-day on the same trigger. No key, no subscription table, no crypto. It needs the ntfy app
on the phone. It is NOT end-to-end encrypted: the card name and why go to ntfy.sh in the clear unless clint
self-hosts. And a protected topic needs an access token, which is clint's own credential stored by atrium. That is
exactly what the line in `CLAUDE.md` rules out ("atrium may hold the NAME of a command that has a credential, and
never somebody else's credential"). So ntfy is only acceptable on an unprotected topic with an unguessable name, or a
self-hosted server that needs no token. @rnd to confirm. This doc controls on that point: @fabric's estimate says
storing clint's ntfy token is allowed but new, and it is not allowed. An ntfy access token is never stored by atrium.

@fabric recommends building the trigger once with ntfy as the first sink (about 1 day in all), which proves the diff
and the coalescing on clint's phone right away, then Web Push behind the same sink interface (about 1.5 days more).
@ui agrees with the order, IF @rnd clears the unprotected-topic form of ntfy. Otherwise Web Push alone.

### 7. Landscape

**Today.** The board header auto-collapsed in landscape before u-021, and u-021 removed that because the slim row is
already short. The terminal in landscape shows about 125 columns at a readable font, which is the whole pty.

**Proposal H, landscape is the terminal's.** In landscape the tab bar hides and an attached terminal takes the whole
screen with the key bar only, as full screen does today. Portrait is for the queue, landscape is for reading a
terminal. Almost free once B exists.

## What @rnd's research says

(To be filled from @rnd's reply: the path and sha of its mobile research, and its top findings. Asked 2026-09-29
~20:35.)

## Cost and ship order

A worker here is one Sonnet worker on a board-only branch unless it says hub. "Deploy" is a hub deploy, and none of
this needs a room restart.

| # | Proposal | Cost | Needs | Ship |
| --- | --- | --- | --- | --- |
| 1 | A, installable board (manifest, icons, iOS meta) | 0.5 worker-day | nothing | wave 1 |
| 2 | E, compose box in the terminal | 1 worker-day | nothing | wave 1 |
| 3 | B, bottom tab bar, top row trimmed | 1 worker-day | nothing | wave 1 |
| 4 | G trigger + ntfy sink (hub) | 1 worker-day, @fabric | @rnd on the line, a migration cleared with the orchestrator | wave 1 |
| 5 | C, the "needs you" home and the card sheet, with I, say | 1.5 worker-days | B | wave 2 |
| 6 | G, Web Push sink (hub) + subscribe (board) | 1.5 + 0.5 worker-days | A, 4, one real-phone proof by clint | wave 2 |
| 7 | H, landscape is the terminal's | 0.25 worker-day | B | wave 2 |
| 8 | F, reading view (t-003 option a) | 2 to 3 worker-days | E | wave 3, only if panning replies is still the complaint |

Wave 1 is four workers in parallel (three board, one hub), about a day, one hub deploy. Wave 2 is three, one more hub
deploy. Wave 3 is decided from use.

Why this order: A and E fix what he hits every time he picks up the phone (browser chrome, typing). B unblocks C
and H. The ntfy sink is the cheapest way to prove notifications on his actual phone before anyone writes RFC 8291.

## Open questions for clint

1. Bottom tab bar (B), or icon tabs on top (B2)?
2. The compose box (E) as the default on a phone, with "keys" as the toggle? Or keys by default?
3. Notifications: Web Push (G), ntfy (G-ntfy) first, or both in the order above? And is turning on Brave's "Use
   Google services for push messaging" acceptable to him?
4. The "needs you" home (C): should a finished turn nobody read count as needing him, or only questions and
   permissions?
5. The reading view (F): build it now, or wait for the compose box and landscape to be used for a while first?

## Out of scope

- A native app. The installed web app covers the home screen icon, full screen and push.
- Approving a permission from the push itself. The payload would have to carry the command, and the push service
  would then hold what atrium was asked to run, even if encrypted. The local notification keeps its approve and block
  buttons, and the push opens the row.
- Any login. The phone reaches the board over the overlay, and the published board's optional OIDC sign-in
  (`docs/overlays.md`) is the only gate.
