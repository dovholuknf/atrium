## Test plan

## @LETTER@. A launcher's say wakes its finished worker

### @LETTER@1. Say to a done worker

1. Launch a worker and let it report done, so that atrium exits its session a few seconds later.
2. From the launcher, call `atrium_say` to the worker with some text and no `wake`.

**Expected:** the worker's session is resumed, the text is typed into it once it is ready, and the launcher is not
told the say was only kept.

### @LETTER@2. Say from a stranger

1. With the same finished worker, say something to it from a session that did not launch it, with no `wake`.

**Expected:** the say is kept on the worker's card and the worker stays exited.
