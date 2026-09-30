- **A `when: done` message stops waiting for the turn after 15 minutes.** A card in a long turn no longer holds a
  message back for an hour. Once a message is older than the room setting `escalate_held_after` (minutes, empty means
  15), the next tool call carries it, with `[atrium] this message waited N minutes for your turn to end, so it is
  delivered now. finish the step you are on, then read it.` in front. The typist lets an aged message through
  mid-turn too, but only for a runner that takes input mid-turn, since a line typed into any other is lost. A card
  whose deploy wake is still to be typed keeps waiting. The card gets an event, `held message escalated after 23m`,
  and the sender's say record gets the same note. A message younger than the setting still waits for the Stop hook as
  before. Room side: live at the next room restart. (held-message-escalation, stage R1)
