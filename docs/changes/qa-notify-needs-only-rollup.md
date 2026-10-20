## Test plan

## @LETTER@. notify hangs off rollup alone

### @LETTER@1. The graph is a chain

1. Open a CI run's page after this lands.
2. The graph draws one line into `notify`, from `rollup`, and none from the check groups.
3. A red check group still sends a failure notice, and a green run a success notice.
