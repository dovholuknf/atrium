# r-new-telegram-notify-build

Not landed elsewhere (no earlier commit on claude/main built it, only the design).

The design says atrium changes nothing, so no atrium code changed.

- `internal/link/notify_leak_test.go`: runs Announced through the real Notifier and CommandSink with a card
  carrying a command, question, recap, diff, path and token canaries. The fake command's record holds none of them,
  stdin is exactly four fields, argv is empty.
- `scripts/notify/telegram.ps1` and `README.md`: the design's sketch as an example. Token read from a file, never
  argv, fixed failure line. Nothing calls Telegram in a test.
- Changelog added.

Tests: `go test ./internal/link -run 'Notify|CommandSink|Presence'` passes. Not run: the whole suite.
Left: nothing. The script is untested against real Telegram, by design.
