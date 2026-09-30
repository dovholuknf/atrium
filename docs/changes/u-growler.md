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
