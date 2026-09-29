- `atrium version` no longer says `(modified)` for a build whose only changes are untracked files. Builds stamp
  `cli.Tree` from tracked files (Makefile, provision-room.ps1), and it wins over Go's own flag. (deploy-guards)
- New `scripts/live/build-deploy.ps1` builds the deploy binary from a clean claude/main only. Both live deploy scripts
  refuse a build that says `(modified)`. (deploy-guards)
- The live deploys now pass only when the room `claude-sg4` is attached, by name, as both the hub and the room see
  it, and still attached 30s later. Before, any one attached room passed. Serving but not attached exits 2 and
  reverts nothing. (deploy-guards)
