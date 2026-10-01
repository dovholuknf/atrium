## Test plan

## @LETTER@. A joined session is live, not cold

### @LETTER@1. Joined row stays visible
Join a session from your own Windows Terminal with `atrium join`, then turn on "hide inactive agents" in the terminal
list gear. The joined row stays, is not grey, and carries a `joined` chip.

### @LETTER@2. Click does not pretend
Click the joined row. Nothing attaches. Hover it: the tooltip says atrium cannot attach to it.

### @LETTER@3. Exited sessions still hide
With the same toggle on, a done or dead session leaves the list and a supervised running one stays.
