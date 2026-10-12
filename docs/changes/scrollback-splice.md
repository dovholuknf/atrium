## Test plan

## @LETTER@. Scrollback holds only what the runner printed

### @LETTER@1. A clear and a repaint do not splice the old page

1. In an agent terminal, print a long diff, then run `/clear` and let the agent print a long reply.
2. Scroll up through the whole reply.

**Expected:** the old diff appears once, before the reply, and no row of it appears after the reply's first line.

### @LETTER@2. A real clear still keeps the page

1. Print a few lines, then run `/clear`.

**Expected:** the printed lines are in scrollback above the new screen, once.
