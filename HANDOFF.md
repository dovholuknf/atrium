# fb01 handoff

Branch `claude/fb01-provision`. WIP commit f293ece has the script changes. Nothing is merged.

## INCIDENT to know first

`scripts/atrium-service.ps1 uninstall -Verb room` calls `Stop-AtriumGracefully`, which runs
`atrium stop --url http://127.0.0.1:7781` no matter which `-TaskName` was given. My throwaway-task test on sg3
(`-TaskName atrium-fb01-test`) therefore stopped sg3's real room. Health on 7781 answered DOWN afterwards. I did
not restart it. `~/.atrium/bin/atrium.exe room --detach` on sg3 brings it back. Whoever continues must NOT run
`uninstall` or `stop` against a machine with a live room: for a test task, use `schtasks /Delete /TN <name> /F`
directly, or make Stop-AtriumGracefully skip the stop when `-TaskName` is not `atrium`.

## The six items

1. **Binary default: done, proven on claudevm.** `Get-Release` returns `$null` on a 404 when there is a checkout
   and no `-Version`, after a `warn` step. `Build-Checkout` builds. `-FromCheckout` still works.
2. **auth step: done, proven on claudevm (not signed in) and by probe on m1mini and sg3 (signed in).**
   Placed after the mcp step, before smoke, at the end of the file (fb02 and fb03 add their lines elsewhere).
3. **schtasks: done, proven.** claudevm full install with `-Autostart`, rerun, and `-Remove`. sg3 (which denies CIM):
   the `Sch` and `Get-AT` helpers ran, and `atrium-service.ps1` install, status, uninstall of a throwaway task
   worked. See the incident above. Unproven: the binary swap's `Sch /End` on a live task (same build every time, so
   the swap never ran), and `-Autostart` start on sg3.
4. **systemd PATH: written, NOT proven.** `packaging/atrium.service` uses a login-shell ExecStart
   (`/bin/sh -c 'exec "$${SHELL:-/bin/sh}" -lc "exec /usr/bin/atrium daemon --db %h/..."'`).
   `scripts/atrium-service.sh` writes a plain ExecStart plus `Environment="PATH=<login shell PATH>"` (with `%`
   doubled) at install time. Needs a Linux run: WSL over `ssh localhost`, with `-Autostart`, then
   `systemctl --user show atrium -p Environment` and a runner in `~/.local/bin`. postinstall.sh only symlinks the
   unit, so it needs no change, but check `$$` in a real unit with `systemd-analyze verify`.
5. **smoke: written, NEVER RUN.** `-NoSmoke`, `-SmokeTo`, `-SmokeCwd`, `-SmokeTimeout` (180) exist. Only the skip
   path ran (claudevm, not signed in). To prove: a signed-in room. m1mini is signed in. @fabric said to ask before
   the first smoke launch on m1mini or sg3. Things unverified in it: POST `/v1/launch` body accepted with
   `spawned_by` and `spawned_by_id`; the card JSON having `recap` and `supervised`; `/v1/tasks/<id>/exit` then
   `supervised` going false; whether a `done` from a non-agent-launched card needs sha (prompt gives `no_commit`).
6. **Docs: not started.** Header comment of `provision-room.ps1` (steps `auth`, `smoke`, exit code 8, the release
   fallback), `docs/packaging.md` provisioning section, `docs/changes/fabric-1-provision.md` (Changelog, and test
   plan "FA. One command makes a room" FA1..: bare to smoke pass, rerun all ok, not signed in gives the command,
   Windows without CIM, -Remove). Do not edit CHANGELOG.md or docs/test-plan.md.

## What I learned

- `claude auth status` prints JSON by default, no flag: `loggedIn`, `authMethod` ("claude.ai"), `apiProvider`,
  `email`, `orgName`, `subscriptionType`. Exit 0 either way, so read `loggedIn`. `claude auth login` has
  `--claudeai` (default), `--console`, `--sso`, `--email`. It prints a URL, so it works over `ssh -t`.
- Windows sshd shell here is Windows PowerShell 5.1. `2>&1` on a native command under `Stop` turns stderr into a
  terminating error, so every schtasks call goes through a function that sets `Continue`. The base64 command line
  limit is 7800: the `-Remove` script was over it once the helpers were added, so the logon-task removal is its
  own remote call, and the helpers are only added when a script names `Sch` or `Get-AT`.
- schtasks: `/Query /TN x /XML` gives Exec Command and Arguments, `/V /FO LIST` gives `Status`, `Last Result`,
  `Next Run Time`, `/Create /TN x /XML file /F` registers for one's own user without admin, `/End`, `/Run`,
  `/Delete /F`. `atrium-autostart.ps1` now registers by XML (LogonTrigger, InteractiveToken, LeastPrivilege,
  PT0S, RestartOnFailure 3 x PT1M). claudevm does NOT deny CIM. sg3 does.
- Card fields: `recap` and `reported_at` on the task (store.Task). Exit is `POST /v1/tasks/{id}/exit` with the
  `X-Atrium-Room` header. `GET /v1/tasks/{id}` exists. Neither smoke assumption has been run.
- In Windows PowerShell on this box `scp` and `ssh` resolve to Git's. Use `C:\Windows\System32\OpenSSH\`.
- The Bash tool's hook here refuses `;` chains, `>` and `2>&1`. Use PowerShell for ssh work.

## Step lines (claudevm, first full run)

```
provision fetch warn dovholuknf/atrium has no release on GitHub, so this builds atrium from the checkout the script is in
provision build ok windows/amd64 b227aa72cb54
provision binary done ~\.atrium\bin\atrium.exe, atrium dev
provision join done joined as claudevm over direct to 192.168.1.68:7779
provision install:claude done C:\Users\claude\.local\bin\claude.exe
provision autostart done logon task atrium, RunLevel Limited
provision start done
provision attached ok claudevm on the hub since 23:27:49, host claudevm, build dev
provision runner:claude ok 2.1.284 (Claude Code) at C:\Users\claude\.local\bin\claude.exe
provision mcp done atrium-control in C:/Users/claude/.atrium/mcp.json, named by the claude runner row with --mcp-config
provision auth warn claude on claudevm is not signed in. the room works and needs you once: run "ssh -t claudevm claude auth login" and follow the URL it prints
provision smoke skip claude is not signed in on claudevm, so a worker there cannot answer. sign in, then rerun
provision done ok
```

Rerun: `binary ok`, `join ok`, `autostart ok`, `start ok`, `mcp ok`. `-Remove` ended `provision hub done removed
claudevm` and `provision done ok`, and `schtasks /Query /TN atrium` then found nothing.

## Waiting on

- @fabric: whether to restart the sg3 room (stopped by my test, see INCIDENT), and permission for a first smoke
  launch on m1mini or sg3.
- Standing rule: no stopping actions on m1mini or sg3 (no -Remove, no new binary, no autostart takeover).
  claudevm is the test machine. Check atrium_peers rooms=true before any restart of a remote room.
- Files: scripts/provision-room.ps1, scripts/atrium-autostart.ps1, scripts/atrium-service.ps1,
  scripts/atrium-service.sh, packaging/atrium.service.
- Check the work: `pwsh -NoProfile -File scripts/check-powershell.ps1`, then the provision command against
  claudevm.

## Next steps

1. Get sg3's room back (see incident), then fix `Stop-AtriumGracefully` or avoid `uninstall` in tests.
2. Sign claudevm in (clint: `ssh -t claudevm claude auth login`), rerun the provision command, and watch the
   smoke step run. Fix whatever the first real run shows. Or ask @fabric for a smoke launch on m1mini.
3. Prove the systemd PATH on WSL.
4. Write the header comment, docs/packaging.md and docs/changes/fabric-1-provision.md.
5. Report to @fabric with the step lines.

## Update after @fabric's answer (commit 9dbe1bf has docs and the guard fix)

- Item 6 docs done. `Stop-AtriumGracefully` now runs `atrium stop` only when the named task is Running.
- sg3's room was restarted by @fabric and is attached. The incident threw away a throwaway card. Do not repeat it.
- NO smoke on m1mini until @fabric clears it: it is about to restart that room for its toolchain PATH.
- NO WSL. WSL on this machine is the room sg4-wsl and is not ours. Item 4 needs a different Linux box.

## Still unproven

1. Item 4 systemd PATH: needs a Linux run with `-Autostart`, then `systemctl --user show atrium -p Environment`,
   a runner in `~/.local/bin`, and `systemd-analyze verify` on the unit.
2. Item 5 smoke: never run. Needs a signed-in room and @fabric's clearance (m1mini after its restart, or claudevm
   once clint signs it in with `ssh -t claudevm claude auth login`). Unverified: `/v1/launch` body, card `recap`,
   exit then `supervised` false, `done` from a non-agent-launched card.
3. The binary swap's `Sch /End` on a live task, and `-Autostart` start on sg3.
