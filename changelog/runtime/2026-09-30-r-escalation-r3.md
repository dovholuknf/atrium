- **A turn running longer than 45 minutes is flagged and its launcher told once.** A running card whose turn began
  more than `escalate_turn_after` minutes ago (a room setting, empty for 45) gets a `long-turn` escalation, shown on
  every card, a person's own included. Where the card has a launcher, it is told once per turn how long the turn has
  run, how many tool calls the last ten minutes held and what the card is in now, so a long test run can be told from
  a runaway. A report, never an action. A silent stop and one long tool call still win over it. Room side.
  (r-new-held-message-escalation R3)
