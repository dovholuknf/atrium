## Test plan

## @LETTER@. The CI hook tells the operator what happened

### @LETTER@1. A routed run

1. Set a route for a repo to a live session: `PUT /_hub/ci-hook/routes` with `[{"repo":"o/r","to":"<handle>"}]`.
2. POST a `workflow_run` body for `o/r` to `/_hub/ci-hook` with the right `X-Ziti-Hook-Secret`.

**Expected:** an alert appears on the board, and as a desktop notification when the board allows them, titled with the
conclusion, repo, workflow, branch, short sha and attempt. Its body says `Delivering to <handle> (<kind>)` and carries
the run link.

### @LETTER@2. A run that is dropped

1. POST a run for a repo no route names.
2. POST a run whose conclusion, branch or workflow the matching route filters out.
3. POST the same run twice.

**Expected:** one alert per post. The bodies say `Dropped: no route names this repository`, `Dropped: filtered` with
the field that did it, and `Dropped: a repeat of a run already recorded`.

### @LETTER@3. A refused or malformed post

1. POST with no secret, then with a wrong one.
2. POST `not json`, then a `workflow_run` body missing `run_attempt`.
3. With `ci_hook.repos` set, POST a run for a repo outside it.

**Expected:** each raises an alert beginning `Dropped:` with the reason. The secret alerts name nothing from the body.
Repeated bad secrets raise one alert a minute, not one per post. No alert holds the secret, a body's free text or the
full sha.
