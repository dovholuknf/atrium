- **A write by name never lands past a room that did not answer.** A POST, PATCH or DELETE naming a card by a bare
  alias or wire name, while one attached room is not answering, is a 409 naming that room, since the card meant may be
  there. Named with its room it goes as before. A read still answers with what it found. A name no longer pays for an
  id lookup on every room first. Hub side. (r-new-review-54794900)
