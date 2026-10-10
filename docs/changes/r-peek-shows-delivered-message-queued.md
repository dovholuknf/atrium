## Test plan

## @LETTER@. A delivered message stops reading as queued

### @LETTER@1. The tip names the escalation

1. Say to a working card with `when: done`.
2. Hover the envelope on its terminals row.

**Expected:** the tip says the message goes in when the turn ends, or after 15 minutes (the escalate_held_after
setting) if the turn runs that long.

### @LETTER@2. Delivery clears the envelope

1. Leave the message until it has waited past the setting while the turn runs.

**Expected:** the message is typed in, and the envelope and its tip disappear at once, with no reload.
