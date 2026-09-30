- **A new context no longer waits forever on a card that never leaves running.** The capture prompt is still typed
  only between turns. When a step has waited a minute on a running card, the room types one labelled line mid-turn,
  "a new context is waiting. Finish the step you are on, commit, and end your turn.", and once more at half the
  step's limit. The line is the cycle's own typing, so the hold that queues every other message does not stop it. The
  chip says the card was asked and when, and a step that still fails names both requests. Covers the capture, the
  wake, the automatic threshold cycle and idle parking, which share the typing path. Room side.
  (r-new-new-context-mid-turn)
