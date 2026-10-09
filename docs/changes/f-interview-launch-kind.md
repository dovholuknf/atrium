## Test plan

## @LETTER@. Interviews and durable work

### @LETTER@1. An interview asks its question in the terminal

1. From an orchestrator, call `atrium_launch` with `kind: "interview"`, a `cwd` that is a worktree on a `claude/*` branch, and a `brief` holding a topic.
2. Open the new card.

**Expected:** the card is tagged `atrium:interview`, runs on opus, BRIEF.md starts with the interviewer rules and then your topic, and the first question is printed in full in its terminal.

### @LETTER@2. Nothing nudges an interviewer

1. Leave the interviewer's question unanswered for ten minutes.

**Expected:** atrium types nothing into it, the launcher gets no silent-stop or launch-idle notice, and the card is not marked STUCK.

### @LETTER@3. The interview ends in git

1. Answer the questions until it finishes.

**Expected:** it commits on its own branch, pushes to the hub, publishes and calls `atrium_done` with the sha.

### @LETTER@4. A cwd outside git is refused

1. Call `atrium_launch` with a `cwd` that is a plain folder.
2. Call it again with `scratch: true`.

**Expected:** the first is refused and says the folder is not a git checkout and how to get past it. The second starts.

### @LETTER@5. A cwd on main is refused

1. Call `atrium_launch` with the main checkout as `cwd`.

**Expected:** refused, naming the branch it is on and saying it is not a claude/* branch.
