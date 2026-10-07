## Test plan

## @LETTER@. One instruction to a launched worker, and a launcher's fyi owes nothing

### @LETTER@1. The launch line and the nudge name atrium_say

1. From an agent, `atrium_launch` a worker with a short prompt and let its first turn end without saying anything.
2. Read the worker's terminal.

**Expected:** the prompt ends with "Before you end your turn, tell your launcher with atrium_say: done <sha> ...". The
nudge typed after the silent turn says "Tell your launcher with atrium_say: done <sha> ... blocked: <one line>". Neither
names atrium_report.

### @LETTER@2. A launcher's fyi after done creates no debt

1. Have the worker `atrium_say` its launcher `done <sha>`.
2. From the launcher, `atrium_say` the worker "stop, nothing else to do" with `kind: fyi`.
3. Let the worker end its turn without replying.

**Expected:** no nudge reaches the worker, the launcher gets no "ended its turn without reporting" notice, and the
worker's card shows no STUCK.

### @LETTER@3. A launcher's needs still creates one

1. Repeat @LETTER@2 with the launcher's message sent without `kind` (or `kind: needs`).

**Expected:** the worker is nudged once after its silent turn, and the launcher is told after a second silent turn.

### @LETTER@4. Across rooms

1. Launch the worker on another room (`atrium_launch` with `room`), let it say done, then send the fyi from the
   launcher's room as `name@room`.
2. Repeat with the hub link to the worker's room down, so the say is held, then bring it back.

**Expected:** both times the worker owes nothing and no nudge or notice follows.
