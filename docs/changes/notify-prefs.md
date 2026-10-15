## Test plan

## @LETTER@. Each kind of alert has its own notification and sound switches

### @LETTER@1. The table and its defaults

1. Open the gear, then notifications. A table lists nine kinds, each with a notification box and a sound box.
2. Every box is ticked except notification on "finished its turn".

### @LETTER@2. A finished turn is heard, not shown

1. Background the board and let an agent finish its turn. The tone plays, no desktop notification appears, and the tray
   still gets the line.
2. Let an agent ask a question. The tone plays and a desktop notification appears.

### @LETTER@3. A box is obeyed and follows the browser

1. Untick notification on "asked a question". The next question sounds and shows only in the tray.
2. Untick sound on it too. The next question is silent and still in the tray.
3. Open the board in a second window of the same browser. The boxes match without a reload.
4. Turn off with the tray's button. Nothing alerts whatever the table says, and sound on stays the master for sound.

Before and after pictures of the section: `docs/changes/notify-prefs/before.png` and `after.png`.
