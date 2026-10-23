Docs: none, website/docs/history.md updated on the branch

This replaces the per-card switches of u-popout-notify. If `docs/changes/u-popout-notify.md` is still pending, its
steps describe the old behaviour and are superseded by these.

## Test plan

## @LETTER@. A popped-out window has the bell and its own mute

### @LETTER@1. The bell in a pop-out

1. Pop a card out to its own window.

**Expected:** the bell sits on the terminal bar, beside the cog, with the unread count. Clicking it opens the same
tray as the board's, with **turn off**, **mute this window** and the sound button.

### @LETTER@2. Mute one window

1. With the board and two pop-outs open, click **mute this window** in pop-out A's tray.
2. Let a card in pop-out A, a card in pop-out B and a card on the board each want you, with no window focused.

**Expected:** A's bell shows the struck bell and its tray says "off in this window". A shows no toast, no desktop
notification and plays no sound, and the notice is still listed in the tray. B and the board notify as usual.

### @LETTER@3. A muted window in front shows nothing

1. Keep A muted and focus it.
2. Let a card on the board want you.

**Expected:** no toast in any window and no desktop notification. The notice is listed in the tray. Unmute A and
repeat: one toast, in A only.

### @LETTER@4. The switch stays with the window

1. Mute pop-out A and reload it.
2. Pop out another card from the board.

**Expected:** A is still muted after the reload. The new pop-out is not muted, and neither is the board.

### @LETTER@5. Turn off is every window's

1. Click **turn off** in a pop-out's tray.

**Expected:** every window, the board included, shows the struck bell and holds back its notices. **turn on** from
the same tray brings them all back.
