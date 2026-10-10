## Test plan

## @LETTER@. The launcher hears a stuck menu once

Room side, so it needs a room running this build.

### @LETTER@1. A launched worker at a menu is held for the orchestrator

1. Tag the launching card `atrium:orchestrator`.
2. From it, launch a claude worker and get it onto a selection menu with the terminal then quiet, such as a model
   switch dialog. Leave it for over 30 seconds across several watchdog ticks.

**Expected:** the launcher's card holds exactly one notice, `source: terminal-menu`, with the menu title, the worker's
card id and its handle. Nothing is typed into the launcher's terminal. The worker's card still shows the STUCK line.

### @LETTER@2. Answering and a new menu

1. Answer the menu on the worker's terminal, then bring up another menu and leave it quiet.

**Expected:** one more notice arrives for the new menu and none repeats for the first.

### @LETTER@3. The operator's own card

1. Start a claude card by hand and leave it on a selection menu for over 30 seconds.

**Expected:** its card shows the STUCK line and no card holds or is sent any notice about it.
