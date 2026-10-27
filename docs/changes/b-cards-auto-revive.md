## Test plan

## @LETTER@. Cards come back after a crash, not only after a clean stop

### @LETTER@1. A card started after the last clean stop

1. Stop and start the room with `atrium stop`, then launch two new cards.
2. Close one of the two with its exit action.
3. Kill the room and the pty host, for example with a reboot.
4. Start the room.

**Expected:** every card that was live before the kill comes back, the new card included. The card closed by hand
stays closed.
