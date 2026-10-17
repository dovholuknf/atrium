A hub that answers 404 no longer closes the hub-health gate. On a board served by a plain daemon, `/_hub/rooms` and
`/_hub/inventory` answer 404, the reader throws "not a hub", and `cappedFetch` took that for a dead hub and held every
poller for a second, again on each poll. After a reload the Terminals view came back before the stack list ever
painted. Only a read that never got an answer trips the gate now. This also fixes seven headless units that had
failed since the gate landed (groupColor, themePreview, walkContext, u016, termDebug, pullsDrawer, termMinionHidden's
neighbours), and clearKeepsPage no longer trips xterm's WebGL renderer.
