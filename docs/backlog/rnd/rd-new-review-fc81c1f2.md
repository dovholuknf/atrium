# Review of fc81c1f2: room machine for $2,500 (docs/rnd/room-machine-2500-design.md)

Reviewer: @review, 2026-10-01. Doc only, no atrium change. Range 5abf6282..fc81c1f2 (the design and
docs/backlog/rnd/rnd-new-2500-server.md). Prices were not re-checked: they are cited and marked est, as asked. The
claims about atrium were checked against docs/release/packaging.md, docs/rnd/room-autostart-design.md and the scripts.

**Verdict: CHANGES.** One claim is wrong and it is the one the recommendation rests on. The rest hold. A fold of
findings 1 and 2 is enough for OK.

## Findings

1. **Medium. "Linux provisioning has never run on real Linux" is wrong.** The design says it twice (Recommendation,
   and OS: "`docs/release/packaging.md` says the Linux paths have never executed on a real machine"). packaging.md
   says the opposite:
   - `:801-806`: provision proven on 2026-09-28 on WSL Ubuntu over `ssh localhost`: install, rerun, `-Autostart`
     taking over a detached room (lingering off), `-Remove`.
   - `:573-589`: the deb installed on WSL Ubuntu 24.04, its postinstall turned on lingering and brought the user unit
     up `active (running)`. "This proves the case docs above marked unproven."
   - `:650-662`: the no-sudo `atrium-service.sh install` user unit proven on the same box.
   - `:809-811`: WSL also joined a hub over ziti.

   What is still unproven, and is the real first-week list: a non-WSL Linux box, provision WITH `-Linger` (the WSL
   provision run had lingering off), a reboot with nobody logged in, Chromium's system libraries for the headless
   suite, and `go test ./internal/daemon` on Linux. Restate the risk as that list. It is smaller than "never run", which
   weakens the argument for option B as the fallback.

2. **Low. Rooms per machine.** "One machine can hold more than one room (sg4 runs claude-sg4 and sg4-control)" is true
   of sg4, but provisioning refuses it: "One room per machine. A remote that is already a room of any hub ... is
   refused with exit 6" (packaging.md:771). sg4's second room was set up by hand, and a second room on one machine
   needs its own `ATRIUM_LOCATION` and ports or it takes the first room's hook pointer. Say the new box gets one
   provisioned room, and that a second (for example, moving sg4-control there) is hand work today.

3. **Low. The suite already runs off sg4.** `scripts/test-board-sharded.js:13,172` dispatches to sg3 by default
   (`ATRIUM_SUITE_ROOM` names another room) through `board-suite-remote.ps1`, which runs `board-suite-run.ps1` on the
   target. So moving the suite is `ATRIUM_SUITE_ROOM=<new room>` plus pwsh on Linux, and the design's "check
   board-suite-run.ps1" item should say that pwsh is required, not optional: the dispatch itself spawns pwsh. Also
   `board-suite-run.ps1:49` prints `$env:COMPUTERNAME` and `$env:NUMBER_OF_PROCESSORS`, which are empty on Linux
   (cosmetic). The "What the workload tells us" section should also say which sg4 number was `--local`, since by
   default an sg4 run is an sg3 run.

4. **Nit.** "make the unit `Restart=on-failure`" is already done: `packaging/atrium.service:25,53-54` has
   `After=network-online.target`, `Restart=on-failure`, `RestartSec=15s`, and `atrium-service.sh` writes that file
   with only `ExecStart` replaced (`:140`). Drop it from the to-do list.

## Claims confirmed

- Linux lingering: `atrium-service.sh` leaves it off unless `ATRIUM_LINGER=1` (`:156-166`), and provision `-Linger`
  passes it (provision-room.ps1:173, :1985-1992, packaging.md:753). Correct.
- Windows: the logon task fires only at an interactive logon, ssh does not fire it (room-autostart-design.md:27). The
  S4U boot task is designed, not shipped: `atrium-autostart.ps1` has no `-Start boot`, only a comment naming S4U
  (`:178`), and the design's staged plan still waits on the ConPTY spike (`:314`, `:330`). Correct.
- macOS: the LaunchAgent needs a GUI login (`:27`), FileVault stops an unplanned reboot at the unlock screen (`:38`),
  and the LaunchDaemon survives an unplanned reboot only with FileVault off (`:59`). Correct.
- `scripts/room-defender.ps1` exists. Correct.
- The board suite is one job on one room, not spread across rooms (`dispatchRemote` picks one room). Correct.

Quality: after the Sonnet switch, no drop seen. The doc is careful about marking estimates, and missed the
one place where the repo's own record answered the question.

## Re-read of the fold 79860a5d (fc81c1f2..79860a5d): OK

All four findings are folded. The Linux risk is restated as the steps unproven off WSL, in both the Recommendation and
the OS section, and open question 1 matches. The second-room line cites the exit 6 refusal. The suite line names
`ATRIUM_SUITE_ROOM` and pwsh. The `Restart=on-failure` to-do is gone. Not folded, and not needed for OK: which sg4
benchmark ran `--local` (finding 3's last sentence). Nothing new found.
