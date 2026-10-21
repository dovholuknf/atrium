# Codex parity

Status: built. Native approval adapter in `internal/cli/hook_permission.go`, installed by
`internal/claudeconf/target.go`. Existing activity, session and Stop handlers are shared with Claude Code.

## Version and evidence

Checked on macOS on 2026-10-21 with **codex-cli 0.159.0** using `codex --version`, `codex --help` and
`codex features list`. The `hooks` feature reports stable and enabled. The installed npm package ships a README
linking to online documentation, rather than a bundled configuration reference.

Read `~/.codex/config.toml`: this machine has TUI preferences and one trusted project, with no `hooks`, `notify`,
`approval_policy` or sandbox overrides. `CODEX_HOME` is unset and `~/.codex/hooks.json` does not exist. Missing
configuration explains the absent activity reports on this machine. Even with the old installer run, it omitted
`PermissionRequest`. It installed reporting hooks and offered Stop separately, but left native approvals in the TUI.
Untrusted hook definitions are another reason an installed file can report nothing.

The [official hooks reference](https://developers.openai.com/codex/hooks) defines the native approval callback and
its distinct response schema. The
[configuration reference](https://developers.openai.com/codex/config-reference) documents notification commands,
approval policies and sandbox settings. These references describe upstream behavior. Integration tests exercise
Atrium's installed commands with representative payloads and a real room, without a model-backed Codex session.

## Event map

| Atrium event | Codex mechanism | Before | Now and remaining gap |
| --- | --- | --- | --- |
| Permission request | `PermissionRequest` | Missing | Fixed, native decisions through `/permission` |
| Tool activity | `PreToolUse`, `PostToolUse` | Supported | Works after install and trust, one second POST |
| Session start and end | `SessionStart`, `SessionEnd` | Supported | Works after install and trust |
| Turn end | `Stop` | Optional | Explicit install, repeat-block guard preserved |
| Queued message | Native denial or Stop block | Stop only | Works at native approval or opted-in Stop |
| Finish | `atrium finish` from a shell tool | Works | Works, no Codex-specific hook needed |

`UserPromptSubmit` clears idle activity. Subagent start and stop and pre-compaction retain their existing mappings.
Codex has no `Notification` or `PostToolUseFailure` event. PostToolUse reports shell commands with nonzero exits too.
PostCompact and Interrupt have no Atrium mapping. No new migration is needed.

## Install and use

Use the installed Atrium binary associated with the room:

```sh
atrium hook install --runner codex
atrium hook install --runner codex --event turn-end
```

The first command installs reporting hooks and the native permission gate. Stop remains an explicit choice because
its block response can continue a turn. The board's Codex hooks controls offer the same events. Both commands retain
foreign hooks and make a backup on change. The CLI prints the hook trust requirement.

Restart Codex and review the exact definitions with `/hooks`. Atrium does not modify the trust store. Automation
that has already vetted every enabled hook can explicitly use Codex's `--dangerously-bypass-hook-trust` flag.
A binary path containing spaces is still refused by Atrium's Codex target, based on the earlier Windows probe
recorded in `other-runners.md`.

For native interactive approvals, use `codex --ask-for-approval on-request --sandbox workspace-write`.
`approval_policy` controls when Codex asks, while sandbox settings constrain execution. `never` or the combined
approval and sandbox bypass flag cannot provide interactive approval parity. Atrium does not rewrite those settings.
`notify` invokes a notification command with JSON. It is not a synchronous approval decision channel and is not used
here because lifecycle hooks already provide the needed events. No terminal prompt parsing or answer typing is built.

## Decision path and limits

The hook probes the existing gate and posts the existing permission wire format with the runner identity.
The room keeps its existing chain: replay, queued messages, paused card, standing rules, auto mode, human. Its existing
deploy hold also remains between paused cards and rules. No step moves. The request still records a durable decision
and exposes `needs-permission` while waiting for a human. An unreachable room, malformed reply or disabled gate emits
nothing and leaves Codex's normal approval flow in control. The native adapter makes one POST attempt.

A native allow uses `hookSpecificOutput.decision.behavior = "allow"`. A refusal uses `"deny"` with the room's reason
in `decision.message`, including queued-message attribution. Codex rejects `updatedInput` at this event, so a board
edit produces a denial telling the model to retry with the edited input. The original command never receives an allow.
The board's stored decision remains an approval of the edited input, while Codex receives that retry instruction.

Native payloads do not promise `tool_use_id`. When present, it retains exact replay behavior, including
approved edits. When absent, the request
has no dedup key, so two identical commands cannot silently share approval. Such native requests are not retried.
Legacy PreToolUse callers keep their existing hash fallback and response format.

Only operations for which Codex requests approval reach the native gate. Commands already permitted by Codex do not
consult standing rules or drain queued messages. A queued message can wait until the next approval or opted-in Stop.
An idle unsupervised session without Stop cannot hear its queue. This differs from Claude's gate on every tool call.
The activity hooks still cover supported tool calls independently of native approvals. Coverage follows Codex's hook
support, rather than promising all internal operations or all approval UI categories. Hook trust dialogs themselves
cannot be handled by an untrusted hook. Forced termination cannot guarantee a SessionEnd callback.

`turnquestions.go` reads Claude transcript conventions. Codex transcript-derived questions and background-task
summaries are not guaranteed by this work. The shared Stop callback, identity and loop guard are tested. A live
Codex 0.163.0 escalation verified native allow and deny through scratch-scoped standing rules while auto stayed on.
The [smoke log](../changes/img/bug-codex-permissions-and-hooks-parity/smoke.md) contains the captured tool results.
Interactive board decisions remain a separate manual test.
