## Test plan

## @LETTER@. The strip condenses single-child chains

### @LETTER@1. One card, three levels

1. Open the terminals view with grouping by project and no expression, and one card under `claude/atrium/rnd`.

**Expected:** one heading reads `claude/atrium/rnd 1` with the card under it.

### @LETTER@2. A split

1. Fold the `claude/atrium/rnd` heading.
2. Launch a second card under `claude/atrium/x`.

**Expected:** `claude/atrium 2` is expanded with `rnd` (still folded) and `x` under it.
