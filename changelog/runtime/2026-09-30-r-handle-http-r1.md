- **An unknown card answers 404, not 500, on every room route.** A card id the room does not hold was a 500 with
  `sql: no rows in result set`, which a script could not tell from a broken room. It is now 404 `no such card`. Room
  side, live at the next room restart. (r-new-handle-addressed-http R1)
