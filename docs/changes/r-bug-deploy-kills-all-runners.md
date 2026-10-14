## Test plan

## @LETTER@. A deploy does not kill runners on its own

### @LETTER@1. The refusal stops the deploy

1. Run `pwsh scripts/live/test-stop-room-409.ps1`. Every line passes.
2. With a room whose host is inside its task's job (`GET /v1/preflight` says `in_job` true), run `deploy-batch.ps1`.
   It stops before changing anything, names the runner counts and says to pass `-KillRunners`.
3. Run it again with `-KillRunners`. The room is stopped with `--runners`.

### @LETTER@2. A restart leaves the host up

1. With `pty_host` on, restart the room with `atrium stop`. `GET /v1/preflight` reports `in_job` false afterwards and
   the runners are still there.
