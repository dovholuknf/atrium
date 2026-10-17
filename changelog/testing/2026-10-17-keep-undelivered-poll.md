- The kept-then-typed daemon test polls the undelivered count down to zero instead of sleeping once for 60 ms,
  so a slow runner finishing the mark-delivered insert after that sleep no longer reads as a lost row.
  keep-undelivered-poll.
