# f-new-gh-login-requirement. A forge CLI login check in room requirements

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G5 of docs-deps. Owned by @fabric.

## What is missing

The PR runner and the review-requested source need `gh` logged in with the right scopes. Neither r-018 preflight nor
`atrium.requirements.yaml` names it, so a room with `gh` logged out or short of scopes passes preflight and fails
mid-run.

## Why it is needed

The failure shows up halfway through a review instead of at provisioning or at launch, with an error that does not say
what to fix.

## Depends on it

- the 378 acceptance replay
- pulls P3
- forge stage 1

## Done looks like

- `atrium.requirements.yaml` can name a forge CLI login with the scopes needed.
- Preflight runs `gh auth status` (never reads a token) and reports logged out, or missing scopes, per room.
- The room's page and the PR source say "gh is not logged in on <room>" before a run starts.
- Atrium holds only the name of the command, never the credential.
