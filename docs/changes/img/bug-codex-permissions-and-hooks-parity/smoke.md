# Native Codex approval smoke, 2026-10-21

Result: **passed the scoped standing-rule allow and deny smoke** on Codex 0.163.0, with global auto left on.
Codex refused the deny command with the rule's exact reason and executed the allow command successfully.
Both temporary rules were deleted. See the final round below and its captured tool results.
Earlier rounds proved hooks but were preempted by auto approval. Their empty pending lists did not indicate
missing hooks. Interactive board approval remains a separate unverified case, replaced by rules for this smoke.

## Installation and versions

`which codex` returned `/Users/claude/.local/opt/node/bin/codex`, a symlink into npm's global `@openai/codex`.
Upgraded the existing npm installation from 0.159.0 to registry latest 0.163.0:

```sh
npm view @openai/codex version
npm install -g @openai/codex@latest --cache "$PWD/build.claude/npm-cache"
codex --version
make build
mkdir -p .smoke/codex-home .smoke/scratch
CODEX_HOME="$PWD/.smoke/codex-home" \
  ATRIUM_HOOK_EXE="$PWD/build.claude/atrium.exe" \
  build.claude/atrium.exe hook install --runner codex
```

All succeeded. Installer wired PermissionRequest and reporting hooks, leaving Stop unwired.
The hook commands pointed to this worktree's build. Native permission timeout was 86400 seconds.
The [0.122.0 changelog](https://github.com/openai/codex/releases/tag/rust-v0.122.0) lists
`#17563 Add PermissionRequest hooks support`. Version 0.159.0 already postdated that addition.
The [0.124.0 changelog](https://github.com/openai/codex/releases/tag/rust-v0.124.0) marks hooks stable.

`codex -a untrusted -s workspace-write --version` exited 2, accepting only `on-request` and `never`.
A config override `approval_policy="untrusted"` also failed. Used interactive on-request with workspace-write.
Noninteractive `codex exec` recorded `never`, so it was excluded from approval evidence.

## Prove hook execution first

Added a second SessionStart handler to the throwaway home's hooks.json:

```json
{"hooks":[{"type":"command","command":"/usr/bin/touch <worktree>/.smoke/session-start-fired.marker"}]}
```

Started Codex with that CODEX_HOME and a prompt asking it to say ready without calling tools.
The marker appeared. `ls -lT` subsequently showed it updated at `Oct 21 16:05:45 2026` after the trusted restart.
The first check was too early and reported no file, but the later check proved execution.

The [official hooks reference](https://learn.chatgpt.com/docs/hooks) documents active config layers,
project trust, exact-definition hook trust and the one-shot trust bypass.
The feature list reported `hooks stable true`. Initially used `--dangerously-bypass-hook-trust` after reviewing
commands. `/hooks` still displayed definitions needing review. Explicitly trusted the reviewed definitions in
`/hooks`, then added a diagnostic PermissionRequest handler and restarted without any bypass flag.
At startup selected `Trust all and continue` for the changed definition. `/hooks` had shown the original
PermissionRequest active after explicit trust. No approval or sandbox bypass was used in the final process.

```sh
CODEX_HOME="$PWD/.smoke/codex-home" RUST_LOG=codex_core=debug,codex_hooks=trace \
  ATRIUM_AGENT_NAME=codex-hook-proof-trusted ATRIUM_TASK_ID= \
  ATRIUM_HOME="$PWD/.smoke" ATRIUM_SCRATCH="$PWD/.smoke/scratch" \
  codex --no-daemon --enable hooks -a on-request -s workspace-write \
  --no-alt-screen -C "$PWD/.smoke/scratch"
```

Cleared inherited CODEX_SESSION_ID, CODEX_THREAD_ID, CODEX_TOOL_CALL_ID and CODEX_CI for these launches.
The diagnostic PermissionRequest handler was `/bin/cat > <worktree>/.smoke/permission-request-input.json`.
It returned no decision and ran alongside the unchanged Atrium handler.

## Native request and the actual cause

Requested one `exec_command` with `sandbox_permissions=require_escalated`, justification
`Allow Atrium approval smoke marker?`, and command:

```sh
printf approved > ../approved-hook-proof.marker
```

Codex invoked PermissionRequest. The capture handler recorded `tool_name: Bash` and the command.
The saved input artifact contains the latest capture, from the second command below.
The command exited 0 and the marker contained `approved`.
The pending permission list was empty because the room immediately approved it through `global-auto`.
`GET /v1/permissions/history` recorded request `01a16012-0f07-7626-a7e0-0b4ddce490f8`, approved at
`2026-10-21T20:05:58.664Z`, with reason
`global auto mode: every session is approved without asking, and recorded`.
History also shows that earlier 0.163.0 direct-tool probes reached Atrium and received global-auto approval.
Those earlier probes did not establish a Codex or code-mode defect.

Temporarily posted `{"global_auto":false}` to the room's `POST /v1/settings` and requested:

```sh
printf approved > ../api-approved.marker
```

A polling script found the pending permission and asserted its card status was `needs-permission`.
Its subsequent `POST /v1/permissions/01a16013-3119-7923-bb03-1767e6df28fa/decide` with
`{"decision":"approve","reason":"Native smoke approve"}` returned 409 because the hub had already answered.
History records `decided_by: global-auto`, with the distinct reason
`board-wide auto mode: approved by the hub without asking, and recorded`.
The room setting was restored to its original `true` value immediately afterward.
The hub's independent board-wide auto mode remains on. No deny test was claimed and no race against hub auto was used
as evidence of manual API decisions.

## Captured evidence and cleanup

- [Native PermissionRequest input](permission-request.json), captured by Codex's hook execution.
- [Codex runtime excerpts](codex-hook-runtime.jsonl), hook started/completed events from logs_2.sqlite and
  policy, tool calls and command results from Codex's own rollout. No authentication data is included.
- [Atrium permission history](atrium-history.json), including room and hub auto decisions.

Every Codex process was quit and awaited. Throwaway authentication and runtime state were removed.
Build passed. Final `bash scripts/test-touched.sh` passed in 184 seconds.
The remaining manual smoke needs both room auto and hub board-wide auto disabled together, then API approve and deny.

## Final round: scoped standing rules with auto left on

Used the same npm Codex 0.163.0, worktree-built hook commands and throwaway CODEX_HOME.
Started interactive `codex --no-daemon --enable hooks -a on-request -s workspace-write --no-alt-screen`
with `.smoke/scratch` as its working root. Reviewed and trusted the installed hooks at startup.
No approval, sandbox or hook-trust bypass was used. No global auto setting was written in this round.

Created two rules through `POST http://127.0.0.1:7781/v1/rules`, with common fields:

```json
{
  "tool": "Bash",
  "kind": "command",
  "scope": "/Users/claude/atrium/cards/codex-perms-hooks-smoke-byut11/work/.smoke/scratch"
}
```

The deny rule used prefix `echo atrium-smoke-deny-5c092d22a`, decision `block` and reason
`Atrium scoped native smoke deny 5c092d22a`.
The allow rule used prefix `echo atrium-smoke-allow-5c092d22a`, decision `approve` and reason
`Atrium scoped native smoke allow 5c092d22a`.
Both create requests returned 200 and their saved definitions retained the scratch scope.

Requested those exact commands in order, each with `sandbox_permissions=require_escalated` and a native
approval justification. Instructed Codex to avoid retrying or working around the denial and to continue to the
separate allow call after the first refusal.

The first tool result was:

```text
exec_command failed: CreateProcess { message: "Rejected(\"Atrium scoped native smoke deny 5c092d22a\")" }
```

Codex displayed `Blocked by hook` and the exact rule reason. No command execution result was emitted for the deny.
The second command exited 0 and printed:

```text
atrium-smoke-allow-5c092d22a
```

Atrium history recorded `block` and `approve`, respectively, with the matching reasons and each rule's command
in `decided_by`, rather than `global-auto`. Asserted one hit on each rule before cleanup.
Deleted both rule ids through `DELETE /v1/rules/{id}` and verified both absent from `GET /v1/rules`.
The empty post-cleanup API list serialized as `null`, so the verification normalized it to an empty list.
Global auto was true before and after, and the hub's auto switch was untouched.
Codex was quit and awaited, and throwaway credentials and state were removed.

- [Rule definitions, decisions and deletion verification](standing-rule-evidence.json).
- [Codex policy, calls and tool results](standing-rule-codex.jsonl).

Final round `bash scripts/test-touched.sh`: passed in 186 seconds.
