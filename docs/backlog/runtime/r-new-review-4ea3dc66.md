# Review: opencode runner 4ea3dc66 on 610819c1 (m1mini, 2026-10-01): HOLD

This is sg4 branch claude/opencode-runner, sent as a format-patch by the orchestrator, a pause exception for clint.
m1mini cannot read the branch. I reviewed the pasted patch and exercised the plugin's logic in node 22 against a stub
`atrium` (`ATRIUM_HOOK_EXE`). I did not `git am` it: the patch reached me only as message text, so no review copy of
4ea3dc66 exists here. The Windows test cannot run on macOS. Unsigned.

## The four questions

1. **Does the gate fail open? Yes, as atrium's gate does.** Throwing is the one way to refuse a call, and only an
   explicit `deny` throws. Garbage output, no output, a missing exe (ENOENT), `ask`, or nothing printed all let the
   tool run. Proven with the stub. That matches `hook_permission.go` ("a hook must never fail a session").
   One difference to name: in Claude Code the fallback is Claude's own permission prompt, and in opencode it is
   opencode's permission config, whose defaults allow bash and edit. The opencode runner row should ship an `ask`
   permission config (or `OPENCODE_PERMISSION`), or a down daemon means unattended tools. That is not in this patch.
   Info, for whoever adds the runner row.
2. **Is activity fire and forget with a 1 s limit? Yes.** `activity()` is never awaited, and `run(..., 1000)` kills
   the child at 1 s. Proven: `chat.message` returned in 1 ms with a stub that sleeps 5 s, and the stub was killed
   before it finished. Session start has 3 s and turn end 2 s. `sessionEnd` is a `spawnSync` with a 3 s bound on
   exit. The gate has no deadline, by design: `/gate` is probed at 3 s and the POST waits on a human.
3. **Does the tool mapping keep the gate sound? Yes.** `permSkipTools` (hook_permission.go:33) skips Read, Grep,
   Glob, WebFetch, WebSearch, TodoWrite, Task and ToolSearch. Every name mapped onto one of them is a read or a
   container: list→Read, codesearch→WebSearch, todoread→TodoWrite, task→Task. A task's child session runs its tools
   through the same `tool.execute.before`, so they are gated. Every writing tool maps to a gated name: bash, edit,
   multiedit, write, patch and apply_patch. An unmapped tool keeps its own name, and opencode's names (lowercase, MCP
   tools as `<server>_<tool>`) do not collide with the skip list, so it is gated. The only way past is a custom
   plugin tool named exactly `ToolSearch` or one of the mapped ids, and opencode would clash on those anyway.
4. **Is a human-edited command honored? Only for four fields. That is the hold.**

## Medium (holds): an edit to raw JSON is dropped and the original runs

`editedInput` (hook_permission.go:311) edits `command`, `file_path`, `url` or `pattern` in place. For any other tool
it shows raw JSON, and it returns the parsed JSON the human edited. The plugin maps back only those four fields
(atrium.js, `tool.execute.before`, the `upd.*` block), so a JSON edit is dropped silently and the decision is still
allow. Affected: `patch` and `apply_patch` (`patchText` only), `question`, `skill`, `list` (`path`), and every MCP
or plugin tool. Proven with the stub: `updatedInput: {"patchText":"*** edited patch"}` with allow, and the plugin
ran `{"patchText":"*** original"}`. A human who trims a dangerous patch and approves it gets the whole patch run.

Fix, either one:
- Apply the whole `updatedInput`: map it back through the inverse of `ARG_NAMES` (`content`→`patchText` when the
  tool had `patchText`), and replace the keys of `output.args` with it.
- Fail closed for this one case: when `updatedInput` carries a key the plugin did not apply, throw "atrium
  approved an edit this plugin cannot apply: ask again".

Test it both ways with the stub shape above.

## Low

- `ancestry_windows_test.go` is indented with 4 spaces, so `gofmt -l` lists it and `scripts/ci.sh`'s gofmt step goes
  red. Run `gofmt -w`.
- `claudeArgs` adds `content = patchText` with no `file_path`, so `permSummary` falls to raw JSON and the board
  shows the patch twice. Either give a patch a `file_path` from its first `*** Update File:` line, or drop the
  alias.
- The ancestry change is sound. The walk skips its own process (`i > 0`, ancestry_windows.go:58), so the test
  binary copied to opencode.exe finds the runner and not itself. The darwin walk matches by name and argv0, so a
  bun-built `opencode` is found there too.

## Tests

The plugin was driven in node, against stub copies, across these cases: deny, allow with a command edit, allow with
a JSON edit, garbage, a 3 s stub, a missing exe, and a 5 s activity. `go vet` and the Windows test were not run, for
the reason above.

Quality: careful work. The mapping is reasoned against the real gate's skip list, every call is bounded, and the
fail-open posture is stated, not left to chance. The miss is the gate's fourth edit shape, raw JSON, which is
easy to miss when reading only the fields `editedInput` names.

Verdict: HOLD 610819c1..4ea3dc66. A re-read starts at 610819c1, room-ok and hub-ok (test tooling and the hook).
