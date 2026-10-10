## Test plan

## @LETTER@. A child card wears its launcher's colour

Before and after: `img/subagent-parent-before.png`, `img/subagent-parent-after.png`.

### @LETTER@1. A child in a repo with no colour entry

1. Turn card colours on and colour one repo. Launch a card from a card in that repo, with its directory in a
   different repo that has no colour entry.

**Expected:** the child wears its launcher's colour in the terminals list and on the board, not the default.

### @LETTER@2. A launcher with an explicit theme

1. Launch a card with `theme` set, then launch a child from it with its directory a scratch folder.

**Expected:** the child wears the launcher's theme. A grandchild wears it too, up to 5 launchers away.

### @LETTER@3. Its own colour still wins

1. Launch a child whose directory is in a repo that HAS a colour entry.
2. Launch another with its own `theme`.

**Expected:** the first wears its own repo's colour, the second wears the theme it was given.
