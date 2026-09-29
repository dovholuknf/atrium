## Changelog

- **One command makes a room.** See `docs/backlog-2.md` item 46 and `scripts/provision-room.ps1`.

  - `scripts/provision-room.ps1` builds from the checkout when there is no release and no `-Version`, with a
    `fetch warn` line. The bare command needs no flags.
  - New `auth` step: `claude auth status` on the remote. Not signed in is a `warn` carrying
    `ssh -t <target> claude auth login`. No credential is read or carried.
  - New `smoke` step, last: a small claude worker launched on the room reports a nonce back. `-NoSmoke`, `-SmokeTo`,
    `-SmokeCwd`, `-SmokeTimeout`. New exit code 8 when it does not report. It runs in the clone `room-git.ps1 init`
    made when init succeeded, else in the remote home.
  - The smoke worker runs on `claude-sonnet-5-5` at low effort, not Haiku. Claude Code's auto mode does not run on
    Haiku, so a Haiku worker stopped to ask permission for `atrium_say` and nobody answered.
  - The smoke worker gets `--allowedTools=<list>` as one argument. As two, the variadic flag also swallowed the
    prompt, and the worker came up at an empty input line. Proven by `-SmokeOnly` against sg3: reported in 9s.
  - `-SmokeOnly` runs just `auth` and `smoke` against a room this script already provisioned. It stops before
    anything is written, so it cannot restart a room in use.
  - `-Repo none` skips the clone again. The checkout path had been held in `$repo`, which PowerShell reads as the
    same variable as the `-Repo` parameter, so the parameter was always overwritten.
  - Scheduled task actions go through `schtasks.exe`, so Windows machines that deny CIM over ssh work (binary swap,
    `-Autostart`, `-Remove`, `atrium-service.ps1`). `atrium-autostart.ps1` registers by XML.
  - `atrium-service.ps1` no longer runs `atrium stop` against the URL when the named task is not running, which had
    stopped a live room while a throwaway task was being tested.
  - The systemd user unit finds runners in `~/.local/bin`: a login-shell ExecStart in the packaged unit, and the
    login shell's PATH written by `atrium-service.sh`.

## Test plan

## @LETTER@. One command makes a room

### @LETTER@1. A bare machine, no flags

On a bare machine reachable by ssh, run `pwsh -File scripts\provision-room.ps1 <target>` with no flags.

**Expected:** it builds from the checkout with a `fetch warn`, and every step ends `ok` or `done`, including
`smoke ok`.

### @LETTER@2. Run it again

**Expected:** every step is `ok` and nothing is restarted.

### @LETTER@3. Not signed in

On a machine where claude is not signed in, run it.

**Expected:** `auth` is a `warn` with the `ssh -t <target> claude auth login` command, `smoke` is `skip`, and the exit
code is 0. After signing in, a rerun passes smoke.

### @LETTER@4. Windows that denies CIM

On a Windows machine that denies CIM over ssh, run with `-Autostart`, rerun, then `-Remove`.

**Expected:** no CIM error appears and `schtasks /Query /TN atrium` finds nothing after.

### @LETTER@5. Linux autostart

On Linux with `-Autostart`, read `systemctl --user show atrium -p Environment`.

**Expected:** it carries the login shell's PATH and a runner in `~/.local/bin` starts.

### @LETTER@6. Remove

Run `-Remove`.

**Expected:** the room's row leaves the hub and the manifest's additions are gone.

### @LETTER@7. Smoke only, against a room in use

Against a room already provisioned and in use, run `-SmokeOnly`.

**Expected:** only `ssh`, `os`, `hub`, `state`, `auth` and `smoke` lines appear, and the room's `attached` time on the
hub is unchanged after.

### @LETTER@8. Smoke timeout

With `-SmokeTimeout 5` on a slow room, run it.

**Expected:** smoke fails with exit 8 and the card is still exited.

### @LETTER@9. The smoke worker is not stuck on a question

During a smoke, read the card's scrollback on the board.

**Expected:** the prompt is in the input and was sent, and no permission question for `atrium_say` or
`atrium_report` appears.
