## Test plan

## @LETTER@. The persistent growler

Needs a hub running a build with `/_hub/growls` and the `growls` event (R1), and a room with a permission request
left unanswered for over two minutes. Desktop board only: the phone strip is a later stage.

### @LETTER@1. One growler, the rest as a strip

1. Leave two permissions and one question waiting until they are growlers.

**Expected:** one growler drawn in full above any toast, with its command's first line and its buttons. Under it a
strip reads `+2 more: 1 permission, 1 question`. The oldest permission is on top.

### @LETTER@2. Expand, fold and scroll

1. Click the strip. Click it again.

**Expected:** the first click shows every growler as a compact row with its primary action. The second folds back.
With many growlers the expanded stack stops at half the window height and scrolls.

### @LETTER@3. Approve and block

1. Click **approve once** on a permission growler. On another, click **block** and give a reason.

**Expected:** the request is answered as it would be from the perms view and the growler leaves on the next hub
event. Neither button posts to `/_hub/growls`.

### @LETTER@4. Reply, open, snooze

1. On a question growler type a reply and press Enter. Click **open**. Click **snooze**, then 15 min.

**Expected:** the reply is queued on the card and never typed into its terminal. Open lands where a toast click
would. The snoozed growler is gone from every board and comes back when the snooze ends.

### @LETTER@5. Dismiss this, and undo

1. Click **dismiss this**. Click **undo** in the toast before it leaves.

**Expected:** the growler goes, then returns at once with its original age. The `?` chip on the card stays
throughout. If the reason ended meanwhile, undo says "already handled".

### @LETTER@6. Over an open dialog

1. Open settings. Wait for, or raise, a growler.

**Expected:** the growler draws over the dialog and its buttons work. The dialog stays open.

### @LETTER@7. The nag and toasts step aside

1. With a growler open for a permission, leave that permission waiting past one minute.

**Expected:** no "STUCK on a permission" toast for it, and no toast for the same permission. The toast log has one
line when the growler appeared and one when it left.

### @LETTER@8. A board with no hub

1. Open a room served alone.

**Expected:** no growler, and no request to `/_hub/growls` at load or later.

### @LETTER@9. The tab asks for attention

1. Raise a growler, then click away so the board tab is not focused.

**Expected:** the tab title alternates every 1.5 s between `(n) atrium` and `! ` plus the growler's title. The favicon
wears an amber dot with the growler count. Focusing the board stops the alternation. The favicon returns to the plain
mark when no growler is open.

### @LETTER@10. One notification and one tone per growler

1. With two board windows open and neither focused, raise a growler.

**Expected:** one sticky desktop notification tagged for that growler and one tone, not one per window. The card's own
tone plays if it has one.

### @LETTER@11. Reminders ring again

1. Leave a growler open until the hub sends a reminder.

**Expected:** the tone plays once and the same notification is shown again. The toast log gains one "growler
reminder" line.

### @LETTER@12. The notification leaves with its growler

1. Let a growler notify, then answer the request from the perms view or another browser.

**Expected:** the notification closes in every browser that showed one.

### @LETTER@13. Mute and the master switch

1. Mute sound and raise a growler unfocused. Unmute, turn notifications off, and raise another.

**Expected:** no tone and no desktop notification in either case. Both growlers are still on screen.

### @LETTER@14. The board on a phone

1. Open the board on a phone (width 480 or less) with two growlers open.

**Expected:** one line under the header with the top growler's title and `+1`. The bell does not stand in for it.
Tapping the line opens the stack in place, at most half the window, and the terminal's key bar and composer stay
clear of it. Approve, block, reply, open, snooze and dismiss this all work from it. After a dismiss the strip shows an
undo line for 10 s.

### @LETTER@15. A popped-out card's growler

1. Pop out a card that has a growler. Open a second pop-out for a different card.

**Expected:** the board draws the growler, the card's own pop-out draws it, and the other pop-out does not. A reminder
rings in the card's pop-out only.

2. Turn the pop-out's notifications off, or mute it, and wait for a reminder.

**Expected:** nothing rings anywhere for that card. The board's growler says "muted in its window". The growler stays
on screen in both.

3. Close the pop-out.

**Expected:** the label goes and the next reminder rings on the board.

### @LETTER@16. The phone home

1. Open `/m` with a growler open.

**Expected:** a strip above the card list with one growler in full and the rest as a count that opens in place. Approve
once, block (asking its reason in the strip), reply, open (the card), snooze, dismiss this and the 10 s undo all work.
Hub calls say `via: "phone"`. Without a hub, no strip.
