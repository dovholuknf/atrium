## Test plan

## @LETTER@. A silent stop types nothing into the worker

### @LETTER@1. Worker stops without reporting

1. Launch a worker from another card and prompt it with something that ends in a plain reply.
2. Let its turn end without atrium_done, atrium_blocked or a say.

**Expected:** nothing is typed into the worker and it spends no further turn. Its launcher gets one notice that it
ended its turn without reporting, and a second silent stop under the same prompt adds none.

### @LETTER@2. Waiting cards stay quiet

1. Leave an interview card waiting on a question, and a worker you prompted yourself waiting for you.

**Expected:** neither is nudged, and neither tells its launcher it went quiet.
