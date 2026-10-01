- **No typed text ends its own bracketed paste.** Every path that wraps text in bracketed paste (a message, a peer
  message, a held peer message, `SayPasted`) now goes through one helper that removes both markers from the text first,
  repeating until none remain, and drops a lone trailing ESC. Text holding `ESC[201~` used to end its own paste and
  type the rest as keystrokes, where `ESC[Z` is Shift+Tab and cycles the permission mode. Room side, needs a room
  deploy.
