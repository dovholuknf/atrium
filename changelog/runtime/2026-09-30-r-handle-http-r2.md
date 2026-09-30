- **A room's own port takes a card by its handle too.** `/v1/tasks/<who>/...` on a room accepts an alias, a wire name
  (qualified or not, `%2F` for the `/`) or `name@thisroom` wherever it took an id, answers with `X-Atrium-Card` and
  `X-Atrium-Handle`, and a miss is a 404 listing the live handles. A name for another room is a 404 saying to ask the
  hub. Ids go straight through, unread. The hub also matches a wire name without its atrium prefix. Room side, live at
  the next room restart. (r-new-handle-addressed-http R2)
