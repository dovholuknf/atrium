- **A room restart brings every running card back again, and only an asked exit stays down.** The previous fix kept
  `done` cards down at boot, but a wind-down leaves every card `done`, so the 12:58 restart reopened nothing. An exit
  asked from the board, `atrium_exit`, a cull or a "leave" action is now recorded, and that alone keeps a card or a
  fixture down, until something launches it again. `POST /v1/tasks/{id}/resume` now starts a card that is neither
  parked nor running, where it answered ok and did nothing. Room side, live at the next room restart.
  (r-new-reopen-resumes-exited-card, follow-up)
