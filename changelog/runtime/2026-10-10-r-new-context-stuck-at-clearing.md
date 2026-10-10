A new-context cycle no longer sits on "2/3: clearing the context" when the turn that ran `atrium ready` ended with
subagents or background work still out. `/clear` now goes once the turn has ended, every step logs, and a `/clear` that
cannot be typed within 5 minutes fails the cycle on the card saying what blocked it. Item r-new-context-stuck-at-clearing.
