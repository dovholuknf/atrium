# Idle parking is off by default

`idle_park_after` defaults to off, and an empty or unreadable value is off. The idle clock only ever parks cards with
`origin:agent`. `atrium:park-idle` and `atrium:orchestrator` no longer make an operator card subject.

## Test plan

1. Fresh store, `GET /v1/settings`. `idle_park_after_now` reads `off`, and the settings page shows the box unchecked.
2. Leave an agent-launched card idle past two hours. It is not parked.
3. Set `idle_park_after` to 3600. An idle agent-launched card parks after an hour. A card started from the launch dialog,
   even tagged `atrium:park-idle`, never does.

**Expected:** as stated. Covered by `TestIdleParkIsOffUnlessSet` and the integration tests in `internal/daemon`.
