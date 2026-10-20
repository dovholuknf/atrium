Docs: none, a fix that makes in_job match what the docs already say it means

## Test plan

## @LETTER@. A pty host in a harmless job is not reported as in a job

### @LETTER@1. Probe from inside an agent host's job

1. On Windows, start the room from a terminal whose job has BREAKAWAY_OK and SILENT_BREAKAWAY_OK but no
   KILL_ON_JOB_CLOSE and no limits.
2. Turn `pty_host` on and read `GET /v1/preflight`.

**Expected:** `pty_host.in_job` is false and the ptyhost log line reads `limits=false`.

### @LETTER@2. A job that kills

1. Start the room from a process in a job with KILL_ON_JOB_CLOSE that refuses breakaway.

**Expected:** `in_job` is true, with the cause, and `atrium stop` refuses without `--runners`.
