Lifting a room deploy hold, at startup or because it was called off, ran out or was lifted from the board, now sends no
message to any card. Idle cards are no longer woken onto a cold prompt cache. A card whose call the hold refused is told
the call did not run and to run it again later, with no promise of a resume message
(bug-deploy-calloff-wakes-every-card).
