## Test plan

## @LETTER@. A session reads CI through the hub

### @LETTER@1. Runs, jobs and a failed log with no gh on the room

On a room that has no `gh` login, from a worker session call `atrium_ci` with `action` `runs`, `repo` `<owner>/<repo>`
and `branch` `claude/main`. Then call `run` with a failed run's `run_id`, then `log` with the same `run_id`.

**Expected:** the runs list has id, workflow, status, conclusion, head sha and url. The run shows its jobs and steps
with conclusions. The log is the failed steps only, ends with the last line of the failure, and says `truncated` when
it was longer than 400 lines. Nobody was asked to run `gh`.

### @LETTER@2. An artifact

Call `artifacts` for a run that uploaded `ci`, then `artifact` with `name` `ci`, then again with `file` set to one of
the listed files.

**Expected:** the first answer lists the artifact with its size. The second names a folder on the hub and its files. The
third answers the last lines of that file.

### @LETTER@3. A missing login and Bitbucket

On the hub run `gh auth logout`, then call `runs`. Log back in with the command it gave. Then call `runs` with a
`bitbucket.org/<owner>/<repo>` repo.

**Expected:** the first answer is the exact command to run on the hub (`gh auth login --hostname github.com`) and
the hub raises its forge alert. After the login the call works and the alert ends. Bitbucket answers
`not supported on bitbucket`.
