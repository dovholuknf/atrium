# f-room-bringup report

Branch `claude/f-room-bringup`, head af914e9d.

## What changed
- Go: `roomsettings.go` gains `reviews_root` and `context_handoff_dir` room settings (test passes).
- `scripts/room-work.ps1` (helpers) and `scripts/test-room-work.ps1`.
- `scripts/provision-room.ps1`: `-WorkRoot`, `-NoAgentPack`, `-AgentPackRepo`, `-AgentPackBranch`, exit 13 (work root not usable, prints the admin lines to run), work-root / work-dirs / work-cache steps, a settings loop (git_root, scm_root, reviews_root, context_handoff_dir; a rerun reads `GET /v1/settings`), `Invoke-AgentPack`, `-Remove` notes.
- `scripts/room-check.ps1`: rows work-root, work-dirs, work-cache, agent-pack.
- `scripts/room-toolchain-c.ps1`: the drive grant is attributes only (RA,REA).
- Docs: `docs/room-accounts.md`, `changelog/fabric/2026-10-08-f-room-bringup.md`, `docs/changes/f-room-bringup.md`.
- Last commit: the probe login comes from WindowsIdentity (a workgroup box reported WORKGROUP\localai).

## Tests
- test-room-work: 109/109. test-room-start: 61/61. `go test ./internal/cli -run TestRoomSet`: pass.
- test-room-toolchain-c: 8 failures, the same 8 as before my change (Windows host: quote-ps x2, payload head, msys find, cpresets size, cmsys x2, cvcpkg).
- test-room-folders: 1 failure, "ps: prints the verb's lines" (Windows host, not touched).
- test-room-account: stops at line 460, `Say` not recognised when there is no sh. Pre-existing, not touched.
- An early test run wrote to the real ~/.npmrc of the claude account on sg4. Fixed (scripts use `$env:USERPROFILE`) and the file removed.

## claudevm
Phase 3, cleared as claudevm\claude: ~/.atrium, ~/.claude, ~/.claude.json, ~/.codex, ~/.room-git, ~/.ziti, ~/git (1143 files), ~/.local/share/atrium-tools (27272 files). No task, process or PATH entry was left. Left alone: ~/go, ~/w32c, ~/e2ee, vm-*.ps1, the w32c-watcher run key, .local\state\claude.

Set up: `C:\vdisks\work.vhdx` (dynamic, 40000 MB) as V: NTFS "work", scheduled task `attach-work-vhdx` (SYSTEM, at startup), V:\ ACL only Administrators and SYSTEM, standard user CLAUDEVM\localai (Users only, key-only login). localai cannot examine V:\.

Runs of `provision-room.ps1 localai@claudevm -Name claudevm -WorkRoot V:\localai`:
1. `-Check` before grants: exit 13, work-root fail, three admin lines printed. Applied verbatim as admin.
2. `-Check` after: all ok/warn, exit 0.
3. Real run from empty: work-dirs (9), work-cache, build, join, settings under V:/localai/..., autostart, room attached to the hub, mcp, agent-pack (14 agents, 24 skills, 41 files, commit 178adb4a2). Exit 5 only for the claude runner not installed plus unrelated warns: room-git (MinGit release v2.56.0.windows.2 has no PortableGit asset, pre-existing), room-gate (no pwsh), statusline (no bash), defender (reads clone paths from this repo's remote, not the work root, a follow-up).
4. Rerun: every step ok, agent-pack "already at 178adb4a2, nothing changed", settings read back.
5. `room-check`: work-root, work-dirs, work-cache, agent-pack all ok. Exit 4 from clone/hooks/gate/statusline rows (toolchain not provisioned, not mine). The hub-main row breaks with a Windows `-Ssh` path (backslashes eaten in sh), pre-existing.

Cleanup: `-Remove` run, the room `claudevm` is removed from the hub and the hub remote. Work root, cache settings and agent pack deliberately kept (they hold the account's work). Left in place as the repro: V:, the vhdx, the startup task, localai. Temp scripts on the VM deleted.

## Review fixes (REVIEW.md on af914e9d, fixed in 17c1f4a4)
Tests after: test-room-work 155/155 (was 109), test-room-start 61/61, `go test ./internal/cli` ok.
The Unix scripts were run under a real dash: the ubuntu:24.04 container (`/bin/sh` is dash), through `sh -s` with the script
on stdin the way ssh sends it. A foreground `docker run` loses stdout here, so the test reads `docker logs`. Without docker it
falls back to any `dash` on PATH (an msys dash makes `ln -s` a copy, so the link check only runs under docker).
Windows PowerShell 5.1 (`powershell.exe`) ran the Windows cache script for the non-ASCII test.

1. HIGH, bash arrays: the folders ride in `set --` and `"$@"`. A static scan of all six Unix scripts for `x=(`, `[@]`, `[[`, `local`, `<<<`, `function`, `source`, plus the dash runs (probe, -Check, run, rerun, pack install).
2. HIGH, -Check wrote on Unix: temp files go to `${TMPDIR:-/tmp}` only, and `mkdir` runs only inside `apply` when `mk=1`. Test: -Check on a bare home leaves the home and TMPDIR empty (local sh and dash), and the work root is not made.
3. MEDIUM, write probe: with -Check the Windows probe reads the root's ACL against the login's SIDs (Allow CreateFiles+AppendData, no Deny, inherit-only entries skipped) and writes nothing. The file probe stays for a real run. Test: folder untouched (no file, same write time), and a Deny entry makes it not writable.
4. MEDIUM, missing parents: the probe reports `parentN=missing`. The admin lines make each missing parent first (`New-Item`, `install -d`), then grant, then the root. The text says "is not there" rather than "cannot examine". Tests for Windows, Linux and the verdict.
5. MEDIUM, `chmod o+x`: now `setfacl -m u:<acct>:x` (`chmod +a 'user:<acct> allow search'` on a Mac), never a line for `/`. Test: no `chmod o`, no line for `/`, quoting for sh.
6. MEDIUM, PS 5.1 encoding: the key and ini files are read with `[IO.File]::ReadAllLines` as UTF-8. Test under powershell.exe 5.1 with a BOM-less file holding é and CJK.
7. MEDIUM, pack links and edits: each folder below `~/.claude` is checked for a link or junction. A file behind one is refused, named (`refused_files`), the record is not written (so the rerun retries) and provision says `warn`. A file that is there, differs from the pack and is not what the last pack wrote (the record's hashes) is counted `edited`, named, replaced, and provision says `warn`. Tests under dash in docker (link and edit cases). The Windows install script has the same logic; its link case is not run, because making a junction in the test needs rights (the Windows pack tests that ran are unchanged).
8. MEDIUM, `room set`: `context_handoff_dir` goes through `store.CheckContextHandoffDir` (trim, absolute, Clean) and `reviews_root` through `filepath.Clean`. Provision compares with slashes normalised, so a Windows room storing `V:\...` still reads back as the same. `TestRoomSetCleansTheWorkRootPair`.
9. LOW, cargo quote: the profile line is built by a runtime `shq` (`'\''`). Test: a root with a `'` is exported intact. Not fixed: an `sh -s` ssh session never reads `.profile`, so on Unix the setting reaches the account's later logins, not the room's own process. Say so to whoever sets up a Unix room.
10. LOW, `awk -v` escapes: done too (it was in the same lines): k, l and sec go by `ENVIRON`.
11. LOW, -WorkRoot against the OS: `Test-WorkRootOs` runs in `Invoke-WorkRoot` once the OS is known, before anything is asked of the remote. Exit 1 with a fail step.
12. LOW, `sh` pack parsing with spaces in names: not done (not on your list).
13. LOW, GIT_TERMINAL_PROMPT: set and restored in try/finally. Test.
14. LOW, git args: `git clone -b $Branch -- $url $src`, and `Test-AgentPackArg` refuses a repo that is not owner/name and a branch that starts with `-` or holds `..`, in provision's argument check, `New-AgentPack` and `Get-HubMirrorCommit`. Tests.
15. LOW, `(OI)(CI)F` instead of `M`: left as is, by design.

On the PowerShell question from the user: these scripts are the operator-side driver, and the part that runs on the room is generated sh or PowerShell. Moving provisioning into the atrium binary is a bigger change than this review asked for. I did not start it.

## Deviations
- No backlog text was readable, so I worked from BRIEF.md alone.
- The agent pack comes from the hub mirror tarball, because the /git/hub forwarder is tokenized per card.
- The panel agents list is hard-coded, and the test checks it against internal/store/prs.go.
- The hub "room row" was not touched.
- Two skills that link into openziti repos (debug-ziti-desktop-edge-win, debug-ziti-edge-tunnel-log) are not in dotfiles and are not packed.
- The Windows test host failures above.

## f-room-spec phase A

`atrium room setup --spec <file|-> --plan|--apply [--json] [--db] [--hub-addr] [--pack-dir]` (internal/cli/roomsetup.go) on top of
the new `internal/roomspec` package (spec, lock, adapters for windows/linux/darwin, plan/apply sharing one `run`). Plan takes a
`View` with no write method (asserted by a test), apply a `Host`; apply writes `~/.atrium/room.lock` even on 13/3. Exit 0 / 13 /
1 / 3 via a new `exitCodeError` honoured in `runRoot`. Room settings go through `storeRoomSetting`, extracted from `runRoomSet`
so `room set` and setup validate alike; a running room is `ErrRoomRunning` (warn).

Tests: MemFS fake per adapter, a real-filesystem test (Windows here, with the home moved to a temp dir, no real ~/.npmrc or
registry), and `TestLinuxAdapterInDocker` which cross-compiles and runs the same in ubuntu:24.04 (passed, and skips without
docker). Not done, by brief: phase C (rewire the .ps1 scripts), D (codex/gemini packs), B (hub stores specs).
Changes beyond the brief: repo regex now refuses a leading `-` in owner/name; EditProfile owns its line per key.
Files: internal/roomspec/AGENTS.md, changelog/fabric/2026-10-08-f-room-spec.md, docs/changes/f-room-spec.md.

## f-room-spec phase C (scripts drive `atrium room setup`), commit 9f2b1cf6

- Item 1 (home parent and another user's home refused in `CheckWorkRoot` and `Validate`) was committed earlier as 8fa2d27a.
- `scripts/room-work.ps1` and `scripts/test-room-work.ps1` are deleted. `scripts/room-spec.ps1` replaces them: it builds the room.yaml
  (`New-RoomSpecYaml`, account written as `@@ACCOUNT@@` and filled in on the room from the ssh login), the remote script
  (`Get-RoomSetupScript`, Windows spec as base64 piped to `atrium room setup --spec -`, Unix a quoted printf), reads the rows and
  administrator lines and exit code (`ConvertFrom-RoomSetup`), and gets the pack source onto the room (`New-PackSource`,
  `Get-PackUnpackScript`). The second-drive hint and `Format-WorkPath` moved there.
- `provision-room.ps1`: with a work root and an atrium on the room it runs `--plan` first (exit 13 or a refused spec ends the run
  before any change, `-Check` prints the rows), and `--apply` right after the join and before any start (settings, caches, folders
  and the agent pack in one call). No atrium yet, or one with no `room setup`, is a `work-root warn` and the first run checks it.
  `Invoke-AgentPack` is gone. The manifest still records `workroot` and `agentpack {repo, branch, commit}`.
- `room-check.ps1`: the work-root and agent-pack rows are the plan's rows from a spec rebuilt from the manifest. `human` stays human,
  `todo` is a warn, an atrium with no `room setup` is a `skip`.
- A real bug in phase A found and fixed: `atrium room setup` returned `alreadySaid` for a refused spec or bad arguments without
  printing anything, so it exited 1 in silence. It now prints `atrium room setup: <why>` on stderr (`setupRefused`, test in
  `internal/cli/roomsetup_refused_test.go`). The scripts print that reason.

Deviations from the handoff design, on purpose:
- No `--hub-addr` is passed to the room. `$HubAddr` is the hub as THIS machine sees it (often 127.0.0.1), which the room cannot reach.
  The plan's pack row therefore says "could not be asked", and `Set-PackRowLatest` settles it against the commit this machine reads
  with `git ls-remote`, so a stale pack is still a warn.
- The pack clone is whole, not `--depth 1`, as the old code was (its comment says the hub serves whole fetches only).
- Without a work root there is no room.yaml (it needs `work_root`), so the shared-folder behaviour stays and the agent pack is NOT
  installed, with an `agent-pack skip` row. This is a regression by design: the pack used to install with no work root.
- A spec the binary refuses is exit 1 from provision only once there is an atrium on the room. The old local argument checks for
  a relative folder, drive root or UNC path are gone from the script (they are `internal/roomspec` tests); only an empty or
  control-character `-WorkRoot` and a bad pack repo/branch are refused locally.

Tests: `pwsh -NoProfile -File scripts/test-room-spec.ps1` 84/84 (with `ATRIUM_TEST_BIN` set to a built atrium: the real
`room setup --plan` reads the spec this builds, and the Windows remote script runs in a child pwsh), `go test ./internal/roomspec
./internal/cli -run 'RoomSet|Spec|Setup'` pass, both scripts parse. NOT run: any ssh run, a real provision against claudevm or any
room, Windows PowerShell 5.1 (the remote runs it), and the full go test (the launcher runs it).

- osfs_test.go reads .zprofile on darwin (was .profile), fixing TestRealFSApplyConverges on m1mini.

- Phase C review fixes: pack failures are warns (Limit-PackFailures); room setup --validate pre-check before ssh; pack-only spec when no -WorkRoot; room-check reads the default pack unless agentpack none; home check is case-exact on Linux, covers root/Shared/Public, resolves links/8.3 and the real home (checkRootHere). go test ./internal/roomspec and scripts/test-room-spec.ps1 (97 checks, with the built binary) pass. Not run on m1mini or PowerShell 5.

- Phase D: codex and gemini packs. runnersetup.PackLayoutFor gives each runner's folder and kinds (gemini ~/.gemini agents+skills, measured in gemini-cli 0.60.0; codex ~/.codex skills only); roomspec packStep installs per runner, filtered to what it reads. provision-room.ps1 still writes only the claude pack.

- Phase D follow-up: CODEX_HOME and GEMINI_CLI_HOME are followed (account user env, then process env). provision-room.ps1 now installs the pack for every runner the repo has (claude, plus codex/gemini when their folder holds agents or skills), records runners in the manifest, and room-check shows agent-pack-codex/gemini rows. go test roomspec+runnersetup and scripts/test-room-spec.ps1 (101 checks) pass. Not run on m1mini or PowerShell 5.
