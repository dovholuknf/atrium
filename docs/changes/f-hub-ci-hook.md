Docs: none, the design is docs/fabric/ci-hook-design.md and phase 2 (OIDC) has not landed

# f-hub-ci-hook, phase 1

The hub receives a CI run's conclusion on the ziti service `ziti-hook`, records it, and says it to the sessions a routes
table names. The design is `docs/fabric/ci-hook-design.md`. This phase is the secret, the routes, delivery and the
ziti bind. OIDC (`ci_hook.require`), the `.github` workflow and any default route are not in it.

## What was built

- `atrium run --ci-hook-service <name>` binds the service with `--identity`, serves `POST /` and nothing else, and keeps
  retrying the bind (30 s, doubling to 10 minutes) while the controller is down. The row `ci-hook` is in the hub's
  listener states, and `ci-hook-down` and `ci-hook-up` are one audit line each.
- `POST /_hub/ci-hook` on the hub board is the same handler, for the machine's own operator only.
- `atrium ci-hook secret` prints a 32-byte secret once and keeps its SHA-256. `--rotate` keeps the old one good for 24
  hours. Without a minted secret every notice is a 401.
- `GET` and `PUT /_hub/ci-hook/routes` (operator only). The body is a list of rules, or `{"routes": [...], "repos":
  [...]}`. One bad rule refuses the list. `repos`, when set, is the only repositories allowed to report.
- Hub store migration `0030_ci_hook`: `ci_run` and `ci_delivery`. The request body is never stored.
- `scripts/ci-notify.sh` and `scripts/ci-notify.ps1`, with `scripts/test-ci-notify.js` and two fixtures.

Where the design left a choice:

- The sender is `ci@hub`. There is no hub name yet to put after the `@`.
- Two rules that name one card by different spellings send one message. The second row ends `folded`, a state the
  design did not list.
- A room that answers `parked` to a message sent without wake leaves the delivery pending. A real room queues instead,
  so the card is told when it next runs.
- A rule's `to` that is a bare handle is resolved across every room, and one that two rooms hold is `refused` as
  ambiguous.

## Routes example (not a default)

```
curl -X PUT http://127.0.0.1:7778/_hub/ci-hook/routes -d '[
  {"repo":"dovholuknf/atrium","to":"qa"},
  {"repo":"dovholuknf/atrium","branch":"main","conclusion":["failure"],"change":true,"to":"orchestrator"}
]'
```

## Test plan

## @LETTER@. The hub hears CI

### @LETTER@1. Secret and routes, with no GitHub

1. On the hub's machine run `atrium ci-hook secret` and keep the output.
2. Start a session and give it the alias `qa`. Put the example route above, with `qa` as the target, using the PUT.
3. Run `scripts/ci-notify.sh scripts/fixtures/ci-workflow-run.json --local --secret <the secret>`.
4. Run it again with the same file.
5. Run it with `--secret wrong`.

**Expected:** the first run answers 202 with `"targets":1` and the session reads `CI failure: dovholuknf/atrium CI on
main at 0123456, attempt 1. https://github.com/... (from CI, no reply goes anywhere)`, from `ci@hub`. The second answers
200 `{"duplicate":true}` and says nothing to the session. The third is a 401 and the audit pane shows `ci-hook-refused:
wrong secret`.

### @LETTER@2. A target that is not there

1. Close the `qa` session. Run step 3 above with a fixture whose `run_attempt` is 2.
2. Start another session and give it the alias `qa`. Wait 30 seconds.

**Expected:** the first run leaves the delivery pending. The new `qa` session gets the notice within 30 seconds.

### @LETTER@3. Live ziti (manual)

1. Run the hub with `--identity <file> --ci-hook-service ziti-hook` where the identity may bind `ziti-hook`.
2. From an identity that may dial it, POST a fixture to `http://ziti-hook.ziti/` with the secret header.
3. Stop the controller, restart the hub, then bring the controller back.

**Expected:** step 2 gets the same 202 as the loopback route. Step 3 shows `ci-hook-down` at start, the hub serving
everything else meanwhile, and `ci-hook-up` once the controller answers, with no hub restart.
