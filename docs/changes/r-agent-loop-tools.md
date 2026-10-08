## Test plan

## @LETTER@. A worker ends with atrium_done or atrium_blocked

### @LETTER@1. Done

1. Launch a worker with a brief. Read its launch prompt.
2. Have it commit and call `atrium_done` with the commit's sha.

**Expected:** the prompt names `atrium_done <sha>` and `atrium_blocked <up to 50 words>`. The launcher gets
`report from <worker>: done at <sha>`, the card moves to done, and the worker is never nudged afterwards.

### @LETTER@2. Refusals

1. Call `atrium_done` with `zzz`, then with a hex id no repository has.
2. Call `atrium_blocked` with an empty reason, then with 51 words.

**Expected:** each is refused with a short error, and the launcher hears nothing.

### @LETTER@3. Blocked

1. Call `atrium_blocked` with a reason of 50 words, newlines included.

**Expected:** the launcher gets the reason as a blocked report, the work item is `reported`, and there is no
"ended without a report" notice when the session exits.
