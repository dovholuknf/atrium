# Live phone test of /m, item 3: the card view (live412-b)

Run against the running hub at http://127.0.0.1:7778, build e7bc886 or later. Chromium emulating a Samsung phone: 412x891,
deviceScaleFactor 2.625, isMobile, hasTouch, Android Chrome user agent. Scripts are kept in D:/tmp/live412/live412-b/
(lib.js, t2.js to t14.js). Screenshots are in docs/backlog/ui/img/live-412/. The card under test is live412-scratch for
anything that sends. The long read-only card for load older is the finished rnd director card.

Counts: 19 PASS, 1 FAIL (3b-4, medium), 3 SKIPPED (3b-5, 3e-2, 3i-3), plus 3 low notes and one harness note.

## 3a Compact one-line headers, full-width bubbles

PASS 3a-1 message headers: every `.rh` header on the scratch card is 14.3 px tall at a 14.3 px line height, so name and
time share one line (5 of 5 bubbles). Screenshot img/live-412/01-card-view-headers.jpg, 02-card-top-14px.jpg.

PASS 3a-2 full-width bubbles: all five `.reply` bubbles measure x=6, width=400 on a 412 px viewport, so 6 px gutters and
nothing narrower. Same screenshots.

Note (low): the card's own header block (`.chead`: name 19 px, sub line, state pills) is three lines and about 63 px. It
scrolls away with the thread. If "compact one-line header" meant this block, it is not one line.

## 3b Pinch text size

PASS 3b-1 pinch changes the size and the thread follows: two-finger CDP pinch out sets `--m-fs` to 24px (the maximum), a
pinch in sets 11px (the minimum). Message text measures 24 and 11 px. img/live-412/03-pinch-out.jpg, 04-pinch-in.jpg.

PASS 3b-2 persists: a pinch to 19.8 px writes `atrium.mfs` = 19.8, a full reload comes back at `--m-fs` 19.8px and the
message text measures 19.8 px. img/live-412/05-after-reload-size.jpg.

PASS 3b-3 composer and switcher follow: at 19.8 px the composer box measures 19.8 px, the switcher rows 19.8 px and its
chips 17.8 px (0.9 of the size). img/live-412/06-switcher-at-size.jpg.

FAIL 3b-4 header (top bar) at the larger sizes: the top bar buttons follow the size, but they wrap and then run off the
screen. Measured (button right edges, viewport 412): 14 px term right=404, 17 px pick wraps to 51 px tall, 19.8 px pick
59 px tall and top bar 63 px, 22 px "open terminal" right=422, 24 px right=446 with the bar 76 px tall. "all cards"
wraps to two lines and "open terminal" is cut off at 24 px. Repro: open any card on /m, pinch out to the maximum,
look at the top bar. Cause: the `.sheet-top` buttons take `font-size: var(--m-fs)` in cardurl.css line 36 with no
`white-space: nowrap`, no shrink and no overflow handling. Severity: medium, "open terminal" and "changes" become hard
or impossible to tap at the top sizes, usable at the default. img/live-412/07-topbar-19_8.jpg, 07-topbar-24.jpg, 03-pinch-out.jpg.
Not flaky, measured on five sizes in one run and seen again in the 03 and 06 runs.

Note (low): the card name (`.c-name` 19 px), the sub line (12.5 px) and the message header names and times (11 px) do not
follow the pinch. Only message text, composer, switcher and the top bar buttons do. working.css says "nothing else
scales", so this may be intended, but the brief says header ALL follow.

SKIPPED 3b-5 "the message under the fingers stays where it is" during a pinch: the pinch code anchors a bubble, but
my scripted pinch moves the scroll programmatically before measuring and I did not isolate it. Not tested.

## 3c Thread stays still with output arriving

PASS 3c-1: scrolled up 385 px with a finger drag so the gap to the end was 425 px, then sampled once a second for 70 s
while live412-scratch ran a shell loop (working row "running PowerShell") and then replied. scrollTop was 988 in all 70
samples. scrollHeight grew 2148 to 3135 to 3430 and the reply count went 8, 9, 11, so output did arrive and the thread did
not move. The jump arrow stayed visible throughout. img/live-412/21-scrolled-up-jump-arrow.jpg, 22-scrolled-up-after-wait.jpg.

## 3d Jump arrow

PASS 3d-1 appears when scrolled up: the arrow (`#m-jump`, 40x40 at x=370 y=800) is hidden at the newest message and
visible once the thread is more than 24 px up (gap 425 and 540 px in two runs), img/live-412/21-scrolled-up-jump-arrow.jpg.

PASS 3d-2 returns to newest: after a wheel scroll up (gap 540) a touch tap on the arrow gave gap 0 and the arrow hid
(clicks=1). A mouse click on it also gave gap 0 every time I tried (4 of 4).

Harness note, not a board FAIL: after a scripted one-finger drag (CDP touchStart/touchMove/touchEnd), the NEXT synthetic
touch tap produces no click event on any element. It reproduced 3 of 3 on the jump arrow and also on the "cards"
button, while the same touch tap works with no drag before it, and after a wheel scroll. Even an 8 s wait did not clear it.
Event log: pointerdown, touchstart, pointerup, touchend, and no click, even on `#m-card-pick`. Since it hits unrelated
buttons I treat it as the emulation, not a defect in the arrow. A real phone is the check if this worries anyone.
img/live-412/24-jump-after-tap.jpg shows the arrow still up after the dragged-then-tapped attempt.

## 3e Load older

PASS 3e-1: on the finished rnd director card (143 replies on the API, read only) the first read returned 50, the page
showed 69 entries. Two older requests followed, `before=2026-09-30T03:17:05.604Z` then `before=2026-09-30T02:42:16.484Z`,
the same cursors the API handed back when I paged it with curl (50, 50, 43 replies, `more` false on the third). The thread
ended at 171 entries, all 171 `data-k` keys unique, in ascending time order, the first entry
`2026-09-30T02:00:23.782Z`, which equals the oldest reply the API returns. The load older button is gone at the start
and nothing repeated or was skipped. img/live-412/10-older-open.jpg, 11-older-page1.jpg, 12-older-start-of-card.jpg.
Two of the three requests came from reaching the top by scroll (auto load) and the tap on the button, so I did not
count taps separately. Scroll position hold across a prepend was not isolated, see SKIPPED below.

SKIPPED 3e-2 "a prepend does not move the reader": my script loaded two pages in one go (auto load plus the tap), so
the before and after position of one bubble was not measured for a single page.

## 3f Switcher

PASS 3f-1 chips: the five chips (running, needs you, ready, done, all) are 40 px tall. Default shows running and needs
you (30 rows). Tapping `all` gives 196 rows and writes `atrium.mswitch` = ["all"], and the choice is still there after
closing and reopening. `running` alone gives only running cards. img/live-412/60-switcher-default.jpg, 61-switcher-all.jpg.

PASS 3f-2 search: the box shows when the list is long (more than 12), "live412" leaves the 5 live412 cards, "zzzqq" shows
"no cards here. try another chip". img/live-412/62-switcher-search.jpg.

PASS 3f-3 sheet: the menu is 396x624 at y=46, scrolls inside, a tap on the backdrop closes it (aria-expanded false), and a tap on a row opens
that card (URL went /m/alias/live412-scratch to /m/alias/live412-b, menu closed). img/live-412/63-switcher-after-goto.jpg.

## 3g Composer

PASS 3g-1 Enter: on this touch-only device (`any-pointer: fine` is false) Enter inserts a newline and does not send,
as the source says. After one Enter the box held "b1\n". Nine more Enters gave 10 lines, nothing sent. A hardware
keyboard (Enter sends, Shift+Enter newline) cannot be emulated by this context, so that half is SKIPPED.

PASS 3g-2 send button states: empty is disabled, with text it is enabled, while sending the composer has class
`sending` and the button is disabled with the note "sending", after the send it is disabled with the note "sent". img/live-412/30-composer-empty.jpg,
31-composer-text.jpg, 34-send-sending.jpg, 35-send-sent.jpg.

PASS 3g-3 failed: with the POST aborted (route abort, nothing reached the card) the bubble reads "not sent, back in the
box", the note reads "not sent: Failed to fetch", the 10 lines are back in the box intact, and the button is enabled
for a retry. img/live-412/33-send-failed.jpg.

PASS 3g-4 a 10 line message does not move the thread or the composer: with the thread scrolled up (scrollTop 2034), I added
nine lines. scrollTop stayed 2034 on every step (max drift 0) and the composer bottom stayed at y=891 (max drift 0). The box
grows from 40 to 138 px and then scrolls inside, the thread viewport shrinks by the same amount. img/live-412/32-composer-ten-lines.jpg.
Sending itself does take the thread to the end (scrollTop 2034 to 3001), which is the documented "own message going out".

## 3h Attach

PASS 3h-1: a 640x400 PNG and a 40 line text file attached with the file input on the scratch card. With the upload
delayed 3.5 s both chips show the state "uploading" (`data-state="up"`, the PNG with a thumbnail), then "ok"
(`data-state="ok"`). One POST to /v1/tasks/<id>/files carried them. The box gained the two incoming paths
(`.../live412-scratch/.atrium/incoming/20261001-071555-b-pic.png` and `...-2-b-notes.txt`). Send is enabled while
uploading, by design ("sending when the upload finishes"). A send with "b attach test, just say ok" emptied the chips
and the box and showed "sent", and a POST to /message went out. img/live-412/44-attach-progress-chips.jpg, 45-attach-progress-done.jpg,
40-attach-uploading.jpg, 41-attach-chips-done.jpg, 42-attach-ready-to-send.jpg, 43-attach-sent.jpg.

## 3i Sound pill and bell

PASS 3i-1 bell: on /m the bell is at x=129 y=6, 44x44. A tap opens the Notifications sheet (empty, "nothing yet"),
the mute button reads "sound on", a tap makes it "sound off", the bell gets class `muted` and `atrium.sound` = {"muted":true},
a second tap restores it. img/live-412/50-home-pill-bell.jpg, 51-bell-log.jpg, 52-bell-muted.jpg.

PASS 3i-2 sound pill: "tap to enable sound" shows fixed at x=133 y=56, 145x36, z 40, on the home page and on the card, and a tap
hides it. img/live-412/53-card-pill.jpg, 54-card-pill-after-tap.jpg.

SKIPPED 3i-3 a bell count and a log row for a new waiting card: I kept /m open for 2 minutes and had the scratch card finish a
turn, and the bell stayed at 0 with an empty log. The scratch card is agent-launched, which `bell.js` skips on purpose
(`isDoer`), and I may not make any other card wait. Not reached, so not PASS.

Note (low): on the card view the sound pill sits at y=56 to 92, over the top of the thread, and covers the first message
header line until it is tapped (visible in img/live-412/53-card-pill.jpg). It does not overlap any top bar button (0 px).
