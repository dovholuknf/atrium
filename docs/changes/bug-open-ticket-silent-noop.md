Docs: none, a fix that makes ctrl-k match what the docs already say.

## Test plan

## @LETTER@. Enter on a discourse link opens its card

### @LETTER@1. Open and focus

1. On the hub's all view, with two rooms where only the second has a runner turned on, make the first the last room you
   launched on.
2. Press ctrl-k, paste `https://openziti.discourse.group/t/upgrade-1-1-15-1-6-3/4784/11` and press Enter.

**Expected:** a card `discourse-4784` opens on the second room and the board switches to it. No row is left on the
Terminals list.

### @LETTER@2. The same topic twice

1. Paste the same link again, then `https://openziti.discourse.group/t/4784`.

**Expected:** the existing card comes up with a toast saying it is already open. No second card.

### @LETTER@3. A failure says why

1. Turn off every runner and paste the link in ctrl-k.

**Expected:** the bar stays with a sentence saying no runner is turned on and what to do, the bell holds it, and no
pending row is parked.
