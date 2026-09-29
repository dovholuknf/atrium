# fabric 1: one command makes a room

## Changelog

- `scripts/provision-room.ps1` builds from the checkout when there is no release and no `-Version`, with a
  `fetch warn` line. The bare command needs no flags.
- New `auth` step: `claude auth status` on the remote. Not signed in is a `warn` carrying
  `ssh -t <target> claude auth login`. No credential is read or carried.
- New `smoke` step, last: a small claude worker launched on the room reports a nonce back. `-NoSmoke`, `-SmokeTo`,
  `-SmokeCwd`, `-SmokeTimeout`. New exit code 8 when it does not report. It runs in the clone `room-git.ps1 init`
  made when init succeeded, else in the remote home.
- `-SmokeOnly` runs just `auth` and `smoke` against a room this script already provisioned. It stops before
  anything is written, so it cannot restart a room in use.
- `-Repo none` skips the clone again. The checkout path had been held in `$repo`, which PowerShell reads as the
  same variable as the `-Repo` parameter, so the parameter was always overwritten.
- Scheduled task actions go through `schtasks.exe`, so Windows machines that deny CIM over ssh work (binary swap,
  `-Autostart`, `-Remove`, `atrium-service.ps1`). `atrium-autostart.ps1` registers by XML.
- `atrium-service.ps1` no longer runs `atrium stop` against the URL when the named task is not running, which had
  stopped a live room while a throwaway task was being tested.
- The systemd user unit finds runners in `~/.local/bin`: a login-shell ExecStart in the packaged unit, and the login
  shell's PATH written by `atrium-service.sh`.

## Test plan

### FA. One command makes a room

- FA1. On a bare machine reachable by ssh, run `pwsh -File scripts\provision-room.ps1 <target>` with no flags. It
  builds from the checkout with a `fetch warn`, and every step ends `ok` or `done`, including `smoke ok`.
- FA2. Run it again. Every step is `ok` and nothing is restarted.
- FA3. On a machine where claude is not signed in, `auth` is a `warn` with the `ssh -t <target> claude auth login`
  command, `smoke` is `skip`, and the exit code is 0. After signing in, a rerun passes smoke.
- FA4. On a Windows machine that denies CIM over ssh, run with `-Autostart`, rerun, then `-Remove`. No CIM error
  appears and `schtasks /Query /TN atrium` finds nothing after.
- FA5. On Linux with `-Autostart`, `systemctl --user show atrium -p Environment` carries the login shell's PATH and
  a runner in `~/.local/bin` starts.
- FA6. Run `-Remove`. The room's row leaves the hub and the manifest's additions are gone.
- FA7. Against a room already provisioned and in use, run `-SmokeOnly`. Only `ssh`, `os`, `hub`, `state`, `auth`
  and `smoke` lines appear, and the room's `attached` time on the hub is unchanged after.
- FA8. With `-SmokeTimeout 5` on a slow room, smoke fails with exit 8 and the card is still exited.
