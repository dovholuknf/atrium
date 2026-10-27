## Test plan

## @LETTER@. A card's processes say which card they belong to

### @LETTER@1. Process Explorer names the job after the card

1. Turn the `pty_host` setting on and launch a card on a Windows room.
2. Open Process Explorer, find the runner (for example `claude.exe`) and open its properties.

**Expected:** the Job tab names the job `atrium <card title>@<room>`, and lists the runner and everything it started.
A shell opened on the same card is in a job named `atrium <card title>@<room> shell`.

### @LETTER@2. The environment carries the card

1. On the same runner's properties, open the Environment tab.

**Expected:** `ATRIUM_CARD` is `<card title>@<room>`.

### @LETTER@3. The pty host heads its own tree

1. Start the room from a terminal that sits in a job object, so the pty host is started through WMI.
2. Find `atrium-pty.exe` in Process Explorer.

**Expected:** no `cmd.exe` sits above `atrium-pty.exe`, and `ptyhost.log` in the state dir still gains lines.
