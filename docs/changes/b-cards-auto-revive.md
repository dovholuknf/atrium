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
