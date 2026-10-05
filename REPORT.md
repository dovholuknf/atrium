# rnd-forge-doc report

## Changed
- `docs/rnd/scm-forge-design.md`: body rewritten to state the design as clint decided it. Host picks the CLI, same-repo
  PR on its real branch and fork PR on `pr-<N>`, least busy room with no checkout ranking, the hub's clone for a room
  without one, no automatic re-placement with a warning alert, no PR list or source row or polling or webhook, access
  on ask through one alert, review on arrival dropped. Staging, call sites, the hub relation and "what is out" follow.
  The open questions and the Answers section are kept as the record. A "Built" section lists what is on claude/main and
  what is in progress (Bitbucket forge, idle-CPU tie-break, paste-to-card glue).
- `docs/runtime/scm-design.md`: "No repository model" said atrium does not clone. It now points at the scm clone path
  that clones from the hub.

## Could not reconcile
- The brief names `docs/scm-design.md` and `docs/rnd/hub-forge-answers.md`. Neither is in the repo. The scm doc is
  `docs/runtime/scm-design.md`. `hub-forge-answers.md` is not on claude/main (hub-forge-design.md says it lives on sg4),
  so it was not checked. `hub-forge-design.md` itself had nothing that contradicts the answers.
- The brief names `internal/api/pulls.go`. The fold is `foldPRRows` in `internal/link/pulls.go`.
- Built differs from the old design in places, and the body follows the code: the requirement is `forges: gh: {host,
  scopes}` keyed by CLI, the PR worktree verb takes `number`, and the room's forge host and command are settings
  (`forge.<tool>.host`, `.cmd`).
- Stage 4 and 5 in the staging list are the in-progress items. Their owners and sizes are not stated.
