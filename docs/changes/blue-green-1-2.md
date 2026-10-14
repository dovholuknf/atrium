# blue-green 1+2: a stop that leaves host runners up

With `pty_host` on, a plain `atrium stop` stops the daemon and leaves host-held runners alive. The next daemon adopts
them, so a deploy no longer kills a mid-turn card. `atrium stop --runners` (or `POST /v1/shutdown` with
`{"runners":true}`) ends them too.
`GET /v1/preflight` reports `pty_host{on,in_job,build}` and `runners{host,in_process}`. A stop is refused with 409
when the host sits inside the daemon's job. The permission hook retries for up to 30s while the daemon is away.

## Turn on, per room

1. Run the platform gate: systemd on cdzrok, launchd on m1mini, Task Scheduler on Windows. None has run yet.
2. `POST /v1/settings {"pty_host":true}` through the hub with the `X-Atrium-Room` header.
3. Deploy with one test card running.
4. Pass: the card has the same pid, was not resumed, and preflight reports `in_job` false.

## Test plan

## @LETTER@. A deploy keeps host runners

### @LETTER@1. Preflight

1. With `pty_host` on, `GET /v1/preflight`.

**Expected:** `pty_host.on` true, `in_job` false, and `runners.host` counts the live cards.

### @LETTER@2. Stop keeps runners

1. Start a card that is mid-turn.
2. Run a plain `atrium stop`, then start the daemon.

**Expected:** the card keeps its pid, is not resumed, and finishes its turn.

### @LETTER@3. Stop with --runners

1. Run `atrium stop --runners`.

**Expected:** host runners end with their exit keys, and in-process runners stop as before.
