# Security report: scope and placement verdict (rnd-001)

Evaluation only. Nothing was written to hooks, ACLs or git config. Sources: atrium `internal/guard` (checks.go,
rules.json), `docs/rnd/gate-exe-vs-atrium-hook.md`, `docs/changes/tests-ignore-operator-git.md`, docs/README.md, the
untracked `docs/security/SECURITY-AUDIT-2026-10-01.md` (615 lines, main checkout only), and the dotfiles facts relayed
by dotfiles-41800@claude-sg4 (dovholuknf/dotfiles c4f97c8). Dotfiles paths below are as relayed, not read by me.

## Where the walls live: two layers, two repos
Dotfiles (live today):
- Trailer wall: `githooks/lib/attribution.sh` (regexes), `githooks/agent/commit-msg` (refuses author, committer or
  trailer naming claude/anthropic). `--no-verify` skips it, so `githooks/agent/pre-push` re-scans pushed commits.
- Push wall: `claude/hooks/pre-tool-use-hook.ps1` (about lines 118, 208-270) blocks git push/pull/fetch and lfs remote
  ops except `git push [-u] hub|atrium-hub <branch>` and `git fetch hub|atrium-hub`; checks the remote URL against
  atrium's daemon.json. The hub's pre-receive is the wall behind it.
- hooksPath split: claude's global `core.hooksPath` = `githooks/agent` (commit-msg, pre-push, other names are _chain
  copies running the repo's own hooks). clint's = `githooks/` (pre-push attribution scan before GitHub, plus a
  main/master push confirm).
- Drive-root guard: same ps1; rationale in `claude/hooks/docs/gate-decisions.md` ("2026-10-10: drive-root guard kept").
  Kept because icacls gives Authenticated Users Modify on D:\ and AD on C:\.
Atrium (parallel port): `internal/guard/rules.json` has the same three rules (trailer, `git-remote`, `drive-root`),
wired by `internal/cli/hook_guard.go` (`atrium hook`), per gate-exe-vs-atrium-hook.md. The report must say which
layer is live per machine and that the two must not drift.

## 1. Scope
In:
- Threat model for an agent with a shell and the walls that answer it, each with its file/rule id and its bypass
  (e.g. `--no-verify` and why pre-push and pre-receive back it; a hook that cannot start fails open).
- The claude/clint hooksPath split and why: claude is held to attribution and no-remote, clint gets the scan and a
  main-push confirm.
- Incident: the misdirected `git reset --soft HEAD~1` in the main checkout (claude/main moved to 7857da92, restored
  to 8836cd0d). Facts, cause, what would have stopped it. Blameless, short.
- Drive-root guard versus ACL hardening: a path rule is weaker than an OS permission. State today's ACL fact (Modify
  for Authenticated Users on D:\, AD on C:\) and the plan.
- Known gaps, each with owner and status.

Out:
- The 2026-10-01 audit's findings (C1..L8): product vulnerabilities, a different subject. Link, do not restate.
- Hook install how-to, and any secret, key path or token value.
- A restatement of gate-exe-vs-atrium-hook.md or gate-decisions.md; link them.

## 2. Placement
- Document: atrium `docs/security/`, beside the audit. It is the cross-cutting record, and atrium is where the
  audit, the guard port and the incident happened. Add one line for `security/` to the docs/README.md map in the same
  commit (it is not listed). Commit the audit with it: it is untracked and exists only in the main checkout.
- Status line per README convention: `Status: built, ...` for the walls; the ACL section `Status: proposed, not
  built`. The report links into dotfiles by repo and path rather than copying the hooks.
- Dotfiles keeps the hooks, gitconfig, gate-decisions.md and the ACL scripts. Do not move the report there: it
  would lose the guard rules and the audit it sits beside. Add a one-line pointer in dotfiles gate-decisions.md.

## 3. Document vs code/config, and owner
| Part | Kind | Owner |
|---|---|---|
| Trailer and push walls | document; code already in dotfiles and atrium guard | doc: atrium; code: dotfiles (live), atrium guard director (port) |
| hooksPath split | document; config in dotfiles | doc: atrium; config: dotfiles/clint |
| reset --soft incident | document (postmortem) | atrium |
| ACL hardening replacing drive-root | CONFIG, not a doc: `powershell/local-llm/harden-localai.ps1` (deny pattern), `grant-localai-path.ps1` (allows), `powershell/onpath/sandbox-account.ps1` (dotfiles TODO.md "Security / hardening"). Not applied. Needs elevation | dotfiles / clint, elevated. Atrium only documents it and drops its own `drive-root` rule afterwards |
| Guard against `git reset` on claude/main in the main checkout | optional new rule; design choice | dotfiles ps1 and atrium guard, separate card |

Keep the drive-root rule (both layers) until the ACLs are applied and verified.

## 4. Size
- Report: about 150-250 lines, one worker, a few hours; the facts are now gathered, the incident needs the reflog.
- README map line and status lines: trivial.
- ACL hardening: scripts exist; applying is a short elevated session by clint plus verification. Own card, not part
  of the report.
- Optional reset guard rule: small.
