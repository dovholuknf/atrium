provision-room.ps1 takes `-WorkRoot V:\localai`: clones, reviews, hand-offs and the npm, go, pip and cargo caches all go under it,
and an account that cannot examine a parent folder gets exit 13 with the administrator's lines instead of Claude Code's
unanswerable prompt. A new provision also installs the operator's agents and skills from the hub's dotfiles mirror, and
`room-check.ps1` has `work-root` and `agent-pack` rows. The drive grant is attributes only, and `atrium room set` takes
`reviews_root` and `context_handoff_dir`. (f-room-bringup)
