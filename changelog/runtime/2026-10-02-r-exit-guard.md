- **`atrium_exit` can no longer exit the wrong card.** A worker once took the `card` from an `atrium_say` reply, which
  named the recipient, as its own and exited the director that launched it. `card` on `atrium_exit` is now optional and
  leaving it out exits the caller. The room refuses, with a reason, a card that is neither the caller nor one the caller
  launched, unless `force: true` (the MCP tool, `atrium exit --force`), which is recorded on that card as a `forced_exit`
  notice. A director (`atrium:director` or `atrium:context-ceiling`) is never exited by an agent, force or not: only the
  operator, who sends no caller, can. The check is in the room's exit route, so the stdio tool, the hub tool, the
  `atrium exit` verb and a cross-room ask all reach it, the cross-room one with the asker as a name foreign to that room.
  `atrium_say` replies name the recipient as `to_card`; the old `card` is gone, and only two tests read it. A card launched from another room can be exited by its launcher there without force.
  A body that does not parse is refused with 400 and never read as the operator's. Deploy the hub and the rooms
  together: a room or hub on an older build sends no asker, and its cross-room exits arrive as the operator's, as they
  do today. Item r-exit-guard.
