## Test plan

## @LETTER@. A worker's result is not a question

### @LETTER@1. A plain result

1. As a launcher, prompt a worker to do a small job.
2. The worker answers with a plain `atrium_say` (default kind), such as "merged, 28 pass".

**Expected:** the launcher's card shows no owed item and no held notice. The worker's debt is settled.

### @LETTER@2. A real question

1. The worker says something that ends in an `Open Questions:` list with one numbered item.

**Expected:** the launcher's card shows one owed item for that worker.
