## Test plan

## @LETTER@. A pinned card is never removed by a hide pill

### @LETTER@1. Pinned rows stay with the pills on

Pin a subagent (a card tagged `origin:agent`) and pin an agent whose session has exited. Turn both `hide inactive`
pills on. Both pinned rows are still in the pinned bucket, the exited one grey, and the bucket heading shows a plain
count. The pills' counts cover only unpinned rows. Fold the pinned bucket: the heading still shows the count. Run
`HEADLESS_ONLY=pinShows node scripts/test-board-headless.js`.
