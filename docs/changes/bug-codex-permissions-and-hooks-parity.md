Docs: none, updated the runner parity reference, historical runner notes and published hook guide.

## Test plan

## @LETTER@. Codex permissions and hooks parity

### @LETTER@1. Install and trust

1. Use codex-cli 0.159.0 and an Atrium binary built from this change.
2. Run `atrium hook install --runner codex`, then repeat it to verify no duplicate entries appear.
3. Install `turn-end` explicitly with `--event turn-end`.
4. Restart Codex, review the definitions in `/hooks`, and trust them.

**Expected:** PermissionRequest is installed with an 86400 second timeout. Tool activity and session hooks are present.
Stop is installed only by the explicit command. Existing foreign hooks survive. The CLI explains hook trust.

### @LETTER@2. Native decisions and activity

1. Run Codex with `--ask-for-approval on-request --sandbox workspace-write` in a joined card.
2. Ask it to perform an operation requiring native approval.
3. Answer the request on the board, first approving, then denying a new request.
4. Repeat with a standing deny rule and with auto mode. Queue a message before the next native approval.
5. Run an ordinary shell tool and end a turn, then close the session.

**Expected:** Requests use the existing chain without a TUI prompt after a valid board answer. A rule still wins over
auto mode and a queued message still wins over a rule. The card keeps its Codex harness, reports tool activity,
becomes ready on Stop and finishes on session close. Ordinary already-permitted tools do not ask Atrium for permission.

Live smoke passed on 2026-10-21 with npm Codex 0.163.0 and scoped standing allow and deny rules.
[Commands and captured evidence](img/bug-codex-permissions-and-hooks-parity/smoke.md) show Codex refusing the deny
with its rule reason and running the allow. Both rules were deleted, and global auto stayed on for that round.

### @LETTER@3. Edits, unavailable room and continuation

1. Edit a command in a board approval.
2. Queue a message and end a Codex turn with Stop installed.
3. Repeat a Stop with `stop_hook_active: true` in an integration payload.
4. Stop the room cleanly and provoke another native approval.
5. Call `atrium finish` from a known Codex card.

**Expected:** The original edited command is denied with retry instructions. Stop carries the queued message once.
The repeat guard permits the continued turn to end. An unavailable room leaves native approval to Codex, and activity
reporting never fails the session. Finish records the recap through the existing best effort endpoint.

### @LETTER@4. Automated coverage

Run `bash scripts/test-touched.sh`.

**Expected:** Installer tests cover native timeout, reporting hooks, preservation and optional Stop. CLI integration
covers native allow, deny, queued-message text, edited input, runner identity and silent failure without retry.
The room integration covers needs-permission, board decisions and exact replay, including edited approvals.
Existing fake-runner integration covers Codex activity, session lifecycle, turn-end message delivery and the
repeated Stop guard.
