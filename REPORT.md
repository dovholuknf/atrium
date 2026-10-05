# r-new-sec-daemon-wide-bind

## Already met
- `atrium daemon` defaults to 127.0.0.1 for --addr and --http: 83063fbf.
- runRestart carries the running daemon's addresses from the location file (BoardListen, AgentListen, with loopback fallback): 83063fbf.
- Tests for both: TestRestartKeepsTheDaemonsBind, TestDaemonDefaultsAreLoopback (83063fbf).

## Built here
- Location file records `room` (daemon.Location.Room). restartDaemonArgs now restarts a room as `atrium room --db --agent --http` and a plain daemon as `atrium daemon`. The room's --dir is not recorded, so it uses the default.
- warnWideBoard in runDaemon logs a warning for a non-loopback or wildcard board bind. It is a warning and not a refusal, because restart keeps an operator-chosen wide bind and refusing would leave no daemon.
- Tests: TestRestartKeepsARoomAsARoom, TestWarnWideBoard.

## Tests
internal/cli passes. internal/daemon fails only the known TestGlobalAutoSurvivesAReopen and TestAnOlderClaudeIsStartedWithoutTheFlag...

## Left
- `atrium room` itself is not checked for a wide --http (room bind is not covered by the warning).
- The LaunchAgent in docs/release/packaging.md was not changed.
