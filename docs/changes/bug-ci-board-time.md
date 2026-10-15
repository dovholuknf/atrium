## Test plan

## @LETTER@. CI board step runs sharded

### @LETTER@1. Same units, one report

1. Run bash scripts/ci.sh --only board.
2. The log shows 'board suite: 312 units in N shards' and one merged report with every unit.
3. The step finishes in minutes, not tens of minutes.
