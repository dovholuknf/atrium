# r-new-forge-interface. A forge interface under the built PR runner

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G1 of docs-deps. Owned by @runtime.

## What is missing

The landed PR runner (P1) hardcodes GitHub: `gh pr view`, `gh pr diff` and the `pull/<n>/head` ref. The
review-requested source and the head check do the same. `rnd-new-scm-forge` designs a forge on a provider, but no
item moves the landed code onto it.

## Why it is needed

Without it a Bitbucket or GitLab PR cannot be reviewed, and a forge spike that lands has nothing to plug into.

## Depends on it

- forge stage 1 (GitHub via `gh`, the PR list per provider in the pulls view)
- the pulls P3 source door for any non-GitHub repo
- the pulls E2E on a non-GitHub repo

## Done looks like

- One forge interface (view, diff, head ref, list review requests, head check) and a GitHub implementation of it that
  the runner, the source and the head check call instead of `gh` directly.
- The runner picks the forge from the provider of the checkout.
- A second implementation, or a fake in tests, proves nothing in the runner still names `gh`.
- The shape follows what `rnd-new-scm-forge` decides, so this waits for that spike's answers on the forge contract.

## @runtime director, 2026-10-05

Landed: forge interface and GitHub through gh (84cf15ab), Bitbucket through bb (37a8cae5), hub forge (r-hub-forge
7255dc01). Nothing left under this id.
