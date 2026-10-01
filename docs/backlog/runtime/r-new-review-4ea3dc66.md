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

## Re-read at 6bce194f (2026-10-01, m1mini): OK 610819c1..6bce194f

This time the patch was rebuilt on a scratch branch off 610819c1. Every file matches the blob hash in the patch's
`index` lines: ancestry.go 7872775c, ci.sh 63e355e8, ancestry_windows_test.go b05aa99e, atrium.js e1dc09a6,
atrium.test.mjs 8d974de9. So what was tested is 6bce194f byte for byte. Unsigned.

**A correction to my first review.** The gofmt low was wrong. The paste had turned the test file's tabs into spaces.
The real blob b05aa99e is tab-indented, and `gofmt -l internal/cli` is empty. `gofmt -l` lists only
`internal/daemon/fyi_test.go`, which this patch does not touch. It fails on 610819c1 too, so the gofmt step in
`scripts/ci.sh` is red on claude/main as it stands. That one belongs to @runtime.

- **The medium is closed.** `applyEdit` maps the whole `updatedInput` back through the inverse arg names, in place,
  and drops the keys the edit dropped. It then checks every key it wanted landed. Anything it cannot apply throws, so
  the edit runs or nothing does. That covers a JSON fallback edit (patch, MCP tools), a `file_path`→`filePath`
  edit, and a non-object, array or null edit, which is refused. A frozen args object throws inside the try, so
  `applyEdit` returns false and the call is refused.
- **The patch double send is closed.** The `content` alias is gone, and the test checks the board gets `patchText`
  once, as Edit.
- **Tests.** `node --test --experimental-test-module-mocks scripts/opencode/atrium.test.mjs` passes 8 of 8 on node
  24.21. `go vet ./internal/cli/` is clean on darwin and with `GOOS=windows`. The Windows ancestry test was not run
  (macOS).

**The fallback: acceptable, with one thing named.** The worker is right that `ask` would put a second, unanswerable
prompt behind every approval. But the plugin lets a tool run on more than an unreachable atrium.
`permissionHook` prints nothing whenever the gate is not engaged for the session: the probe fails, gate `off`, or
not joined and no MCP wired (hook_permission.go:85-92). A Claude card falls back to Claude's own prompt in the
terminal, which the board shows. An opencode card falls back to running bash and edits unprompted. That is no worse
than opencode without the plugin, since the plugin only adds a gate, so it holds nothing. clint should know that
an opencode card with the gate off is effectively a skip-permissions card. One line on the runner row or in the
launch docs is enough. A later option, his call: when the hook prints nothing, the plugin denies Bash, Edit and Write
with "atrium is not gating this session".

**Should `editedInput` returning nil be an item? Yes, a medium for @runtime.** hook_permission.go:157-163: an
approval whose edit does not parse back (a JSON fallback edit with a typo) gets no `updatedInput` and is still sent
as `allow`. So Claude runs the original the human edited, which is the same bug this review held for opencode. The
board does not validate the JSON before it sends. Fix: when `ans.Command` differs from the summary and
`editedInput` is nil, answer `deny` with "the edit did not parse, nothing was run", or refuse the approval on the
board. Test: approve a raw-JSON summary with invalid JSON and expect a deny.

Quality: after the Sonnet switch, a clean fix. It checks its own write, and its tests fail on the old code for the
reason held. The pushback on the gofmt claim was right and checked.

Verdict: OK 610819c1..6bce194f, hub-ok and room-ok. The orchestrator lands it on sg4.
