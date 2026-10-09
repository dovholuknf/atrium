# Gate as gate.exe or as `atrium hook`

Verdict for clint. Inputs: the dotfiles go-gate-plan.md, backlog item H14, branch claude/r-hooks-all-in-atrium
(e97eaa42, guard in `internal/guard`, wiring in `internal/cli/hook_guard.go`), the current pre-tool-use-hook.ps1,
`internal/claudeconf/whichexe.go` and `docs/rnd/rolling-restart-design.md`. Nothing was built or timed. The latency
of `atrium hook` is not measured here.

## 1. The three reasons, against fact

**"The gate is down whenever atrium restarts or upgrades."** Mostly wrong. `atrium hook` is a short-lived process
that Claude Code starts per call. The guard on the H14 branch never talks to the daemon. Its only daemon-side input
is the location file (the hub rule reads the room's agent address, as the ps1 already reads daemon.json). A daemon
restart changes nothing for it. What can hurt is the binary file itself. whichexe.go resolves the settings path to
the running daemon's exe, and deploy uses the rename-aside swap (reload-design.md section 4) because Windows will not
overwrite a running image. So there is a window of milliseconds where the installed path does not exist. A call
landing in it hits "hook cannot start", which Claude Code treats as non-blocking, so it fails open. The plan's own
`build.ps1` has the same window (rename old aside, rename new in) and the same open failure. The two designs have the
same exposure, and it is tiny. The real coupling is different: an atrium release with a gate bug reaches every
session at once. That is true of a gate.exe rebuild too, but gate.exe is rebuilt only when the rules change, while
atrium ships for many reasons.

**"Sessions atrium did not start need it too."** Wrong as stated. Hooks come from `~/.claude/settings.json`, which
every Claude Code session on the machine reads, whoever launched it. `atrium hook` needs the binary on disk, not a
daemon. It would fail only on a machine with no atrium installed, and there the hub rule means nothing anyway.

**"Rules change several times a week."** True, and the real constraint. But atrium already has the answer on the
branch: rules are data (`rules.json`), and `ATRIUM_GUARD_RULES=<path>` points at a file in dotfiles, so a personal rule
edit needs no rebuild and no atrium release. It is live on save again, which gate.exe is not. The gap: the override
replaces the built-in set, so it has to be made layered (built-in then personal). The branch notes say this.

## 2. What atrium gives that the plan misses

- **Same binary on every room, no dotfiles checkout, no Go toolchain.** Matters most. sg3, m1mini, Linux and macOS
  already run atrium. gate.exe needs a dotfiles checkout, a compile and a staleness check per room, and an unbuilt
  room fails open. This is the plan's weakest point (see 4). gate.exe cannot get this without shipping prebuilt
  per-OS binaries, which is rebuilding atrium's release pipeline.
- **Parser instead of regex.** The guard uses mvdan.cc/sh and a PowerShell tokenizer, so it follows `cd`, `git -C`,
  wrappers and `bash -c`. It also fixes the known misfires (`2>&1`, `;` in quotes, only the first git of a chain).
  This removes the plan's regex-translation risk (lookaround, `$Matches`, Unicode `\w`) instead of porting it. The
  price is that it is a rewrite, not a port, so parity needs the same 30-day replay and expected diffs must be
  triaged. gate.exe could not get this cheaply.
- **Room and hub knowledge for the git-remote rule.** Matters a little. The ps1 already gets it from daemon.json, and
  gate.exe can too. Not a differentiator.
- **Permission chain and standing rules.** Matters little for this gate. Permission answering is a separate hook
  (`hook_permission.go`) and already coexists with a deny hook. Keep them separate. gate.exe loses nothing.
- **Rules on the board, audit records.** Nice, not needed. A block log helps the weekly rule-count check the plan
  wants (Get-HookReport does it from transcripts today). Editing security rules from a web board is also a new attack
  surface, so I would not do it. gate.exe could write its own log line.

## 3. What the plan gets right

- **Independence from atrium releases.** Real, as argued above. A gate regression should not ride along with an atrium
  deploy. This is the strongest argument for gate.exe, and only partly answered by data-driven rules, since the
  parser and checks are still compiled into atrium.
- **Works with atrium absent or broken**, for example mid-reload or on a bad atrium build.
- **Rule iteration speed** with plain Go and one `build.ps1`. Atrium matches this only through the rules file.
- **The Phase 0 and Phase 2 work** (run the suite under powershell.exe 5.1, add missing rule coverage, 30-day
  differential replay, reason-text asserts) is correct and needed for either choice. Do it first.
- Fail closed on internal error is the right posture. The H14 branch already does it, more completely (bad rules,
  override file, panic and failed git each deny).

## 4. Risks in the plan

- **A gate.exe bug locks every session.** Fail closed on a panic or bad stdin means a bug that trips on common input
  blocks every Bash, Write and Edit, including the edits needed to fix it. The plan has no escape hatch. The H14
  branch has `ATRIUM_GUARD=off` and lets the rules file itself still be written. gate.exe needs the same, or a
  last-known-good copy.
- **Missing or stale gate.exe fails open**, and the plan admits its SessionStart warning does not stop calls made
  before anyone reads it. On a fresh room, or after a Go change with no rebuild, the gate is silently off. Across
  several rooms that is a repeat risk, not a one-off.
- **Distribution to other rooms** is unsolved. The plan's paths are Windows (`C:/Users/claude/...gate.exe`), and the
  rest of the fleet needs per-OS builds, a toolchain, or release artifacts. This is what atrium already does.
- **Regex parity.** The plan lists the hazards well, but "zero unexplained differences" over 30 days only covers
  inputs seen. Boundary cases (lookbehind replacements, non-ASCII) rely on hand-written tests.
- **Two sources of truth**: the ps1 until Phase 4, and the atrium branch if it stays alive. Pick one.

## 5. Verdict

**`atrium hook`, with personal rules in dotfiles.** Resume the H14 guard on its branch, not a gate.exe. Specifically:

- Atrium's rules stay compiled in (git policy, hub remote, co-author phrase, only-atrium-subagents).
- Personal rules (no-find, no-perl, no-python, cmake and vcpkg, em-dash, `!important`, `gh api`, tee, compound-cd)
  live in a dotfiles rules file via a layered `ATRIUM_GUARD_RULES`. This gives the plan its fast iteration without a
  rebuild.
- Do the plan's Phase 0 and Phase 2 (5.1 test run, coverage, 30-day replay) against `atrium hook`.

Top three reasons:

1. The two outage arguments do not hold. No daemon is involved, and the swap window is the same for any exe.
2. Same binary on every room with nothing to build. gate.exe's fail-open-when-unbuilt risk multiplies per room.
3. A parser removes the regex-parity risk, and the guard and its ported test cases already exist.

The one real cost to accept: a bad atrium release can break the gate for all sessions. Cheap mitigations: keep
`ATRIUM_GUARD=off` as the kill switch, run the replay and the suite in atrium CI, and deploy only through the
rename-aside swap. If clint judges that coupling unacceptable for a security boundary, the fallback is gate.exe, and
then the missing-build fail-open must be closed first, not just warned about.

**H14 keeps:** the guard (this verdict), plus phases 2 to 5 as written: state and ledger, tab title, context lines and
bootstrap, so settings.json ends up holding only atrium commands. That is wider than the plan's "state and title
only", and consistent with H14.
