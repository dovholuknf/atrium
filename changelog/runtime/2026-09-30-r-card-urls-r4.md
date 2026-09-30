- **A lent session has a readable address that names that card and only it.** The address handed out for a lent card
  is `<frontend>/room/<room>/<handle>`, the handle because it never moves, where an alias can be renamed or taken over.
  The guest listener serves the card's readable page and the page's lookup only when the name is this card's, and
  every other name, found or not, gets the one 403 it gives everything else. `#term=<id>` still works on the same
  listener. Room side, live at the next room restart. (u-new-card-urls R4)
