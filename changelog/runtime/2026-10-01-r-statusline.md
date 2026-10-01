- **Every new room gets clint's Claude Code status line.** `scripts/provision-room.ps1` has a `statusline` step that
  copies `scripts/statusline-command.sh` to the account's `~/.claude/` and merges only the `statusLine` key into
  `settings.json`, with a `settings.json.statusline-<stamp>.bak` first and nothing written when the key is already
  right. `room-check.ps1` flags a room whose `settings.json` has no `statusLine`, through `statusline: required` in
  `atrium.requirements.yaml`. Item r-statusline.
