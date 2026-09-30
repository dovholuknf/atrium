A ready alert rings once per wait. It is keyed on the card and its `waiting_since`, so a card that drops out of the
waiting set for a moment and returns no longer rings again.
It also waits until the card has been quiet for 5 seconds and is still waiting. Any activity restarts the quiet.
Permission alerts are unchanged.
