## Test plan

## @LETTER@. Cards come back after a crash, not only after a clean stop

### @LETTER@1. A card started after the last clean stop

1. Stop and start the room with `atrium stop`, then launch two new cards.
2. Close one of the two with its exit action.
3. Kill the room and the pty host, for example with a reboot.
4. Start the room.

**Expected:** every card that was live before the kill comes back, the new card included. The card closed by hand
stays closed.

### @LETTER@2. Ctrl-K brings back killed cards

1. Leave the room up and kill the pty host, so its cards stop but stay on the live list.
2. Open the board and press Ctrl-K with nothing typed.

**Expected:** a "revive all N" row sits above the killed cards, and each killed card shows a "killed" chip and its
room. Enter on that row resumes every killed card and a toast says so. A parked, paused or throwaway card is not
listed.

### @LETTER@3. A nested claude does not end its card

1. Restart the room while a card is live, so the card is revived.
2. Right after it comes back, run `claude` in that card's terminal, then quit it.

**Expected:** the card stays live and keeps its own pid. The nested session never shows as the card's session.

### @LETTER@4. A card killed by a failure is not archived

1. Kill the pty host while a card is live, then leave the room up past its dead sweep.

**Expected:** the card stays on the board as killed. A card whose runner quit with exit code 0 is the only one that
leaves the live list.

### @LETTER@5. A session started by hand finds its card

1. Let a card die, then in its folder run `claude --resume <its conversation id>` by hand.
2. In a folder whose only card is done, run `claude` by hand.

**Expected:** step 1 brings the dead card back with the new session's pid, and no second card appears. Step 2 makes a
new card named after the folder with the first 8 characters of the session id, and quitting it marks that card dead.
The done card is untouched.
