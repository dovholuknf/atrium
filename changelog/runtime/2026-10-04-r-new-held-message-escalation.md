# Held-message escalation, the room stages

- A message held `when: done` stops waiting for the turn after 15 minutes (R1).
- A turn over 45 minutes is flagged `long-turn` and its launcher told once, with the tool-call rate (R3).
- A running card over its context threshold is told once at its next tool call to finish, commit, hand off and end its turn. At another 50k it is told again and its launcher gets one notice. Then nothing more that turn (R2).
- One long tool call is covered by the existing `long-tool` notice at 20 minutes, and the board's `Bash 10m` chip from the card's activity.
