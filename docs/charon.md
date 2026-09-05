# Charon: what it is, and what atrium should do about it

Standing reference. Written by reading the source, not the pitch.

Source read: `github.com/Lomchat/charon`, Apache 2.0, cloned shallow to `D:/tmp/charon` on 2026-09-03 and not
committed here. Every Charon path below is relative to that clone. Every atrium path is relative to this repo.
Nothing was executed. Every claim is a static read, and where a claim rests on a comment or a design note rather
than on code that was traced, it says so.

This supersedes the earlier `docs/charon-spike.md`, which asked seven questions, is folded in here in full, and
was deleted so there is one document on the subject rather than two that drift.

## 1. What Charon is

A self hosted board that drives coding agents on rented VPSes. A Next.js hub in TypeScript
(`app/`, `lib/`, `server.js`) holds the browser UI and a SQLite database, and it reaches each machine by spawning
the operating system's own `ssh` binary as a child process (`lib/server/agent/AgentClient.ts:474`). On the far end
a Python daemon runs the provider SDK **in its own process**: `claude_agent_sdk.ClaudeSDKClient`
(`agent/charon_agent/session.py:30`) with hooks registered as in-process Python callables
(`session.py:2000-2010`), and the OpenAI Codex app-server protocol as a second backend
(`agent/charon_agent/codex_session.py`). There is no interactive `claude` CLI anywhere and no terminal being
scraped. Charon is not supervising a program a human types into, it is *being* the client. That single fact
shapes almost every difference between the two projects. It is single user by design, one shared
`MASTER_PASSWORD` for the whole fleet (`SECURITY.md:43-50`), and its author has run it daily for about six
months and is direct about the rough edges.

Assumption, stated inline: the "six months" figure comes from the project's own `CHANGELOG.md` header and its
public post, not from anything verifiable in the tree.

## 2. The good

### The peer bus: sessions that can address each other

Every live session gets a stable, human-chosen `@handle`, validated against
`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$` (`agent/charon_agent/server.py:139`) and unique per machine in the hub's
database (`lib/db/schema.ts:143`, `uq_claude_sessions_vps_id_handle` at `:248`). Both providers are configured to
launch one small stdio MCP server, `charon_peer` (`agent/charon_agent/peer_mcp.py`), which exposes five tools:
`list_sessions`, `send_message`, `get_message_status`, `get_conversation`, `list_inbox` (`peer_mcp.py:29-94`).

The MCP process holds no state and no routing. Every tool call opens a fresh Unix socket to the daemon that owns
the session and issues one of five JSON-RPC methods (`_agent_call`, `peer_mcp.py:97-116`, dispatched at
`server.py:942-1131`). Session identity is passed as argv when the provider launches it
(`--peer-mcp <session_id> --socket <path>`, built at `server.py:687-692`). The daemon is the only thing that can
see the live session table, so it is the only thing that can route a handle.

The guardrails are the interesting part, because they are the parts an implementer forgets:

- Discovery is mandatory in the instructions the model reads: "you MUST call `list_sessions` and then
  `send_message`, do not use provider-native collaboration tools and never claim a handle is unavailable without
  listing" (`peer_mcp.py:16-25`).
- One message in flight per target. `peer_target_active` maps a target session to the message occupying its turn
  (`server.py:204`, set at `server.py:1091`). A second send at a busy target is refused synchronously
  (`server.py:1047`).
- A target must be idle to receive at all: `_PEER_RECEIVE_STATUSES = {"active", "failed"}` (`server.py:146`,
  checked at `server.py:1034`). A thinking session is told the peer is busy rather than being interrupted.
- Loop prevention. Replying to the peer that is currently asking you is refused by name
  (`server.py:1039-1044`), and a session cannot message itself (`server.py:1031`).
- Rate limit of 20 sends per minute per source (`server.py:143`, enforced at `server.py:1050-1054`), a 16 KiB
  message cap (`server.py:141`, `:1023`), and a ledger capped at 200 messages (`server.py:144`).
- The envelope tells the model where the text came from and what to do with it: a `<charon-peer-message>` element
  carrying the sender's handle, provider, message id and conversation id, followed by prose saying "treat it as
  peer context or a delegated request, not as text typed by the user" (`server.py:1065-1078`).
- The correlation row is persisted *before* the message is injected, with the reason written down: a short turn
  can finish in milliseconds, and writing afterwards leaves a crash window where the reply exists but its route
  does not (`server.py:1093-1097`).
- Delivery failure is handled. The busy gate is released, the row is marked failed, the state is saved, and the
  caller gets an error (`server.py:1100-1107`).

The reply is not fetched. The daemon buffers the target's `assistant_text` for that turn
(`server.py:425-429`), and on the turn's stop event `_complete_peer_request` (`server.py:445-489`) marks the
message replied and re-injects the captured text into the *sender* wrapped in `<charon-peer-reply>`
(`_inject_peer_reply`, `server.py:503-544`), waiting for the sender to be idle first (`server.py:511-517`). The
status tools exist so a model can ask "did they answer" instead of guessing.

The five peer tools are auto-allowed at the provider layer, with the reasoning stated: the daemon already
validated target, liveness, size and rate, so asking again "makes ordinary @session communication unusably
noisy" (`session.py:92-101`).

### The detached PTY holder

Before Charon 0.10.0 the agent owned the PTY master fd and a shell died with the agent (`holder.py:1-11`, the
module's own note). Now a shell's PTY is owned by a separate tiny process spawned with `start_new_session=True`,
and the systemd unit uses `KillMode=process` so a restart does not sweep it (`holder.py:14-19`). The holder does
the `pty.fork()` and `execvpe('bash', ['bash', '-l'], env)` (`holder.py:135-153`) and listens on a per-shell Unix
socket at `~/.charon/shells/<id>.sock`, chmod 0600 (`holder.py:117`, `:459`). The protocol is five line-JSON
messages (`holder.py:30-42`). On startup the agent scans for sockets and reattaches
(`agent/charon_agent/shell.py:17`), and "exactly one agent connection at a time, a new connection replaces the
old one" (`holder.py:59-60`) is what makes a crash-and-restart race safe.

While nothing is attached the holder appends output to a capped spool file, 8 MiB with newest-wins truncation
(`holder.py:83`, `:197-212`). On reattach it pauses its own reader, replays the whole spool so ordering is
guaranteed, deletes the spool, then resumes (`_replay_spool`, `holder.py:222-241`).

### Optimistic concurrency on file writes

`fs_write` takes an optional `expected_sha256` and its docstring is explicit that this is "a precondition, not a
hint" (`agent/charon_agent/fsnav.py:503-521`). A mismatch returns `reason: 'stale'` plus the file's real hash
**without writing** (`fsnav.py:532-540`) and the browser decides: reload, or resend with `expected_sha256=None`
as a deliberate force overwrite. `""` means "this file must not exist yet". The write itself is a temp file in
the same directory, `fsync`, then `os.replace`, and it preserves the existing file mode (`fsnav.py:544-557`).
The same stale check guards deletion, specifically so reverting a snapshot-diffed edit cannot delete work an
agent produced after the snapshot (`fsnav.py:697-725`). There is no lock and no arbitration of who owns a file.
The daemon just refuses to clobber silently.

### One upload pipeline for three gestures

`app/ClaudeSessionView.tsx` wires window-level drag listeners while a chat session is open (`:1271-1332`) and
distinguishes an OS file drop from dragging a path out of the in-app explorer (`isPathDrag`, `:1278-1283`,
`:1308-1317`). The explorer case skips upload entirely and splices the existing remote path into the textarea at
the caret. A real drop, a picker selection and a clipboard paste carrying files (`onPaste` reading
`e.clipboardData?.files`, `:1336-1342`) all route into the same `handleFiles` (`:1259-1264`), which streams bytes
to the hub, on to the machine's session workspace, and appends the resulting remote path into the message text as
each upload lands. An overlay covers the input while an upload is outstanding so the caret cannot move under the
user (`:1344-1349`, `:1537-1550`).

A pasted screenshot therefore becomes a file on the far machine and the model reads a path with its own file
tool. There is no image-specific branch and no base64 inlining, which is exactly why one pipeline covers files
of any kind.

### Reachability that does not become a second source of truth

Telegram is not a channel into the daemon. It is one more client of the same hub calls the browser makes:
`respondPermission` / `respondQuestion` (`lib/server/claude/telegram.ts:322-324`, `:360`), reached by a bot long
polling `getUpdates` (`pollLoop`, `telegram.ts:255-285`) and rendering an inline keyboard
(`sendPermissionToTelegram`, `telegram.ts:148-183`). The whole authorization is one allow-listed chat id, checked
on every inbound update (`telegram.ts:300`, `:370`). The file is under 500 lines because it adds no state. There
is also Web Push with stored subscriptions (`claude_push_subscriptions`, `lib/db/schema.ts:531-539`), so an alert
reaches a closed tab.

### SSH used well

One `ControlMaster=auto` per machine, keyed by a sha256 of `user@host:port` (`lib/server/agent/sshShared.js:59-66`)
with `ControlPersist=120`. The first connection pays the handshake and every later channel, the persistent RPC
client, per-shell WebSocket proxies, one-shot byte-range file streams and directory zip streams
(`buildAgentFileStreamSshArgs`, `buildAgentZipStreamSshArgs`, `sshShared.js:133-164`), reuses the master.
`BatchMode=yes`, key-only auth, and a Charon-scoped `known_hosts` at `~/.ssh/charon_known_hosts`
(`sshShared.js:43`) keep it out of the operator's personal trust store, with `StrictHostKeyChecking=accept-new`
so a first connection is trusted on sight and a later key change hard-fails.

### Git and LSP as bounded daemon services

`agent/charon_agent/git.py` (1171 lines) shells out to `git` with argv only and no shell (`:102-106`), in a
hardened environment built so git fails instead of blocking: `GIT_TERMINAL_PROMPT=0`, empty `GIT_ASKPASS` and
`SSH_ASKPASS`, `GIT_OPTIONAL_LOCKS=0`, `LC_ALL=C`, `ssh -o BatchMode=yes` (`:75-91`). Every call is timeout
bounded (`:36-41`), nothing raises, and failures come back as `ok: False` plus a machine-readable reason
(`_classify:128`, `_fail:160`). The write surface is a documented allow-list with no `reset --hard`, no force
push and no interactive rebase (`:16-19`).

`agent/charon_agent/lsp.py` (723 lines) spawns real language servers and speaks LSP over stdio with
`Content-Length` framing (`_Server:135`, `start:242`, `request:303`). It is bounded at 4 servers, a 900 second
idle stop, a 20 second request timeout and 300 diagnostics per file (`:45-50`), one server per root and language
(`_get:341`), with an idle reaper (`_reap_idle:333`). It deliberately does not install anything, it reports the
install command (`:55-58`).

### A durable event log with a resume cursor

The agent writes a JSON-Lines log per session at `~/.charon/events/<session_id>.jsonl` with a monotonic `seq`
(`agent/charon_agent/event_log.py`), rotated at 10 MiB times 3 files (`:70-71`), and the hub reconnects with
`subscribe({session_id, after_seq})` from the last seq it persisted (`lib/db/schema.ts:158-160`). The problem it
solves is real and specific: the agent keeps producing events while the hub is disconnected, and the in-memory
ring is 2000 events deep (`server.py:111`).

Both bounded queues drop the *client* rather than the data on overflow (`server.py:2515-2522`, `:2560-2579`),
because the hub reconnects and resumes from its cursor. That is the right choice and it is the reason the cursor
exists at all.

### State is written atomically, and two daemons cannot share it

`save_state` is mkstemp in the same directory, `json.dump`, flush, `fsync`, `os.replace`
(`agent/charon_agent/state.py:63-71`). A `SIGKILL` cannot corrupt it. Most callers do not use the 200 ms
debounce (`schedule_save`, `server.py:232-244`) but call `_save_state_now()` and await it, including the peer
correlation write before delivery (`:1097`) and every session state change (`on_state_change`, `:671`).

The single-instance guard probes the socket before unlinking it rather than assuming a leftover file is stale
(`server.py:2329-2339`, rationale at `:128-133`), and shutdown only unlinks a socket whose inode is still its
own (`:2474-2475`). Two daemons over one `state.json` is a documented past incident and is now covered by a test
(`agent/tests/test_single_instance.py:1-8`).

### Upgrades are orchestrated rather than hoped for

The agent is a committed `agent/dist/charon-agent.pyz`, base64'd in Node and piped over `ssh` into a `.new` file
then `mv`'d, so the swap is atomic (`lib/server/claude/bootstrap.ts:183-213`). CI enforces byte reproducibility
of that artifact (`.github/workflows/ci.yml:86-95`) and a version bump check
(`scripts/check-agent-version-bump.mjs`). The upgrade sequence snapshots resumable sessions, persists a
`resumePending` flag **before** touching anything, drops the client, deploys, recreates the client
unconditionally, then resumes (`lib/server/claude/agentUpdate.ts:40-173`). The durable flag exists because
fire-and-forget resumes died with a hub restart and left sessions asleep forever, and the comment says so
(`agentUpdate.ts:56-62`). There is also an `'ahead'` branch that hands off when a machine's agent is newer than
the hub, added after a two-hub rollback fight (`lib/version.ts:58-61`).

## 3. The bad

Judged against what Charon is trying to be: one operator's board over a handful of machines they own.

### Approvals expire, and expiry is a deny

Three timeouts, not the two the earlier note assumed:

| Path | Timeout | Where |
| --- | --- | --- |
| Claude tool permission, both the hook and `can_use_tool` paths | 10 min | `session.py:1281` |
| Claude `AskUserQuestion` | 30 min | `session.py:1313` |
| Codex command, file change, permission approval and MCP elicitation | 30 min | `codex_session.py:1816`, `:1857`, `:1886` |

On expiry `asyncio.wait_for` raises, and the code returns a deny: `PermissionResultDeny(message="timeout (10min
without response from the dashboard)")` (`session.py:1351`), or from the hook path `permissionDecision: "deny"`
with reason `"timeout/cancellation"` (`session.py:1249-1253`). An `interaction_resolved` event with
`outcome: "expired"` is emitted so the UI can distinguish it (`session.py:1288-1294`). The model cannot. It
receives a deny that is indistinguishable from a human's no, and continues its turn from there. Each site also
emits `expires_at = now + timeout + 1` so the hub can count down and stay strictly behind the provider
(`session.py:1284`, `codex_session.py:1889`), which is careful work in service of a decision that costs
correctness.

For Codex `requestUserInput` the timeout is whatever Codex sends as `autoResolutionMs`, clamped to a 1 second
floor, and only when it arrives as a JSON integer (`codex_session.py:1856-1857`). A float falls through to 1800
seconds. Charon never sets the field itself.

The consequence is not theoretical. A machine left running unattended for eleven minutes produces a silent,
unrecorded-as-such refusal that the model may then route around by trying a different tool.

### "Always" is coarser than it looks

The Telegram and browser "Always" button does persist something, and it is the one rule-shaped thing in the
tree. `respondPermission(permId, allow=true, always=true)` (`telegram.ts:323`) reaches
`lib/server/agent/sessionOps.ts:1807`, RPCs the daemon at `:1840`, and then writes hub-side at `:1847-1850`:
`this.alwaysAllow.add(row.toolName)` plus `_persistAlwaysAllow()` (`:2096-2102`), stored as a JSON array in
`claude_sessions.always_allow_tools` (`lib/db/schema.ts:244`, migration `drizzle/0031_jazzy_makkari.sql:1`) and
hydrated on stream creation (`:2106-2117`).

The key is the bare SDK tool name. One "Always" on a `Bash` card approves **every later `Bash` command in that
session**, silently, with the card inserted into the database and then auto-answered (`sessionOps.ts:1052`,
`:1086-1093`). Their own comment shows this was a considered line: Codex is deliberately excluded because its
card labels are broad classes like "Codex command" and "remembering one would silently approve every later
request of that class" (`sessionOps.ts:1048-1051`). The same argument applies to `Bash`, one level down.

On the daemon side `always` is accepted and discarded: `respond_permission` resolves the future with
`bool(allow)` and never reads the flag (`session.py:795-800`). So the rule lives only in the hub, which means a
session driven by anything other than this hub has no memory at all.

For Codex, "always" is handed to Codex itself as `acceptForSession` or `scope: "session"`
(`codex_session.py:1895`, `:1899`). Charon stores nothing and the memory dies with the provider session.

### Permission modes are the real escape hatch, and one of them is a total bypass

`normal | acceptEdits | auto | plan` for Claude, and Codex sandbox modes
`read-only | workspace-write | full-access | accept-all`, in one column
(`lib/sessionCapabilities.ts:10-22`, `lib/db/schema.ts:157`). `auto` is a full bypass in the PreToolUse hook
(`session.py:1230-1238`). There is no middle ground between "ask me about this tool forever" and "stop asking
about anything".

Worse, the model can put itself there. `ExitPlanMode` is auto-allowed (`session.py:92-93`) and its handler
schedules `_switch_to_auto_after_exit_plan()`, which sets `self.permission_mode = "auto"`
(`session.py:1177-1183`, `:1356-1372`). Approving a plan therefore hands the rest of the session an unguarded
tool surface, and the gate that would have asked is the same gate that was just switched off. Plan mode also
auto-allows `Bash` when `_is_safe_bash(cmd)` says so (`session.py:1195`), which puts a string heuristic on the
boundary. That function was not read, so no claim is made about how well it holds.

### The event log degrades exactly when it is needed

`append` swallows `OSError` and prints a warning, on the reasoning that the in-memory ring is still there so
"live subscribers don't notice" (`event_log.py:161-168`). But the ring is the thing the log exists to survive.
A full disk therefore turns durable-with-resume back into lossy-2000-events silently, and the failure surfaces
later as a gap banner rather than at the moment durability was lost.

The stated reason for the append being safe is also not the real one: "'a' is atomic at the syscall level on
POSIX for writes <= PIPE_BUF" (`event_log.py:158`, and `:31-34`). `PIPE_BUF` governs pipes, not regular files.
What actually makes it safe is that there is one writer per file, which the same docstring says two lines
earlier. Harmless in practice, and the kind of comment that misleads the next person to touch it. Each append
opens and closes the file, so it is flushed but never `fsync`ed, and a host power loss can lose the tail. The
read path skips corrupt lines with a warning (`:37-40`), which is the right recovery either way.

### The provider split is duplicated, not abstracted

`server.py` is 2709 lines, `session.py` 2209, `codex_session.py` 3150. The Claude and Codex paths reimplement
the same lifecycle, the same approval future pattern and the same event vocabulary side by side rather than
behind one interface, and the asymmetries that follow are the ones documented above: `always` persists for one
provider and not the other, timeouts differ by provider, the hub's replay fallback branches on `kind` in several
places (`sessionOps.ts:1053-1055`). Every future provider pays that cost again.

### `list_dir` is not contained, by design

The tree RPCs are contained under a caller-supplied root, and the docstring says plainly why that is not a
security boundary: "the ssh user can already read anything, the hub hands out shells, so this is not a privilege
boundary, it is there so that a `..` in a path can't quietly turn a file browser into a way to page through
`/etc` by accident" (`fsnav.py:22-25`). Correct and honest. But `list_dir`, the path autocomplete backend, takes
any absolute or `~`-prefixed path and is not contained (`fsnav.py:50-60`). Consistent with the stated posture,
and still a thing a reader should know before assuming the file surface is scoped.

## 4. The ugly

The parts that are load-bearing and would not survive a second user.

### One password guards the entire fleet

Authentication is a single shared `MASTER_PASSWORD` from the environment (`lib/server/auth.ts:20-24`), compared
with `crypto.timingSafeEqual` (`:36-41`), with a per-IP login throttle (`app/login/actions.ts:28-33`). The
comment says it outright: "ONE MASTER_PASSWORD guards the whole fleet" (`app/login/actions.ts:25-27`). The AES
key for encrypted settings is `scrypt(MASTER_PASSWORD, MASTER_SALT)` (`auth.ts:32-34`), so the password is also
the data-at-rest key. `SECURITY.md:49-50` is explicit that there is no MFA and that learning the password is
full access.

The `users` table exists (`lib/db/schema.ts:4-10`) but `ensureUser()` takes `LIMIT 1` and inserts a sentinel row
with `passwordHash: 'env'` (`auth.ts:67-81`). The columns are vestigial. There is no registration route, no
role, no ACL, and every session belongs to that one row. Multi-user is not partially built, it is a shape the
schema gestures at and the code does not implement.

The password is also triple-purposed. It is the dashboard login and the scrypt seed for the at-rest key, so
rotating it destroys every encrypted setting. `.env.example:11-19` says so plainly, which is the right thing to
do about a constraint you cannot remove.

Encryption at rest covers three values: `telegram.bot_token`, `claude.api_key`, `vapid.private`
(`lib/server/claude/settings.ts:19-23`), AES-256-GCM, failing closed in production on both write (`:36-38`) and
on reading a legacy plaintext row (`:56-59`). The SSH private key is not among them, because `ssh` has to read
it: the hub stores a path and passes it to `ssh -i` (`lib/server/agent/AgentClient.ts:471`), default
`/root/.ssh/id_rsa` (`sshShared.js:84`). It is bind-mounted, excluded from the build context so it cannot be
baked into a layer (`.dockerignore:8-13`), and `chmod 600`'d with a readability preflight
(`docker/entrypoint.sh:98-124`). The crown jewel is correctly handled and correctly outside the encryption
story, which is worth knowing before assuming the encrypted-settings feature covers the thing that matters.

The wider blast radius is stated rather than hidden: every managed machine is trusted and an SSH key holder is
root-equivalent (`SECURITY.md:60-64`), and permission cards "reduce accidental actions, they are not a security
boundary against a deliberately trusted full-access agent" (`SECURITY.md:65-69`). That is the most useful
sentence in the repository and it is worth quoting to anyone who thinks an approval gate is a sandbox.

A compromised hub is not limited to the agent protocol either. It spawns `ssh` with an argv it fully controls
(`AgentClient.ts:471-474`, `sshShared.js:80-113`), so it can run any command as that user on every machine in
its database. The bootstrap path already does exactly that, writing a systemd unit, calling
`loginctl enable-linger`, and trying `sudo -n` opportunistically (`bootstrap.ts:203-209`, `:271-292`).

### The socket authorizes nothing, and the file root arrives off the wire

Two facts that compound.

First, the daemon does no authorization on JSON-RPC at all. `dispatch` routes on method name
(`server.py:911-918`) and `_handle_client` builds a client and runs it with no handshake, no token and no
`SO_PEERCRED` check (`server.py:2480-2485`). The only boundary is filesystem permissions: 0600 on the socket and
0700 on `~/.charon` (`server.py:2341-2345`, `:2416-2419`). Which session a `charon_peer` process claims to be is
whatever was passed on its command line (`server.py:687-692`, `peer_mcp.py:190`,
`agent/charon_agent/__main__.py:121-123`). Anything that can open the socket can send a peer message as any
session, read any conversation, call `shell_start` to get a detached `bash -l` (`server.py:2100`,
`holder.py:147`), and write or delete files. Socket access equals arbitrary code execution as that user, and on
a default install that user is root.

Second, `_contained()` realpaths both sides and is correct as written (`fsnav.py:86-99`), but the `root` it
contains against comes straight off the wire: `_fs_read(str(params.get("root") or ""), ...)` and the same for
`fs_write`, `fs_list` and `fs_stat` (`server.py:1139-1157`). Nothing checks `root` against a session's cwd, so
`root="/"` yields the whole filesystem. The `--stream-file` and `--stream-zip` out-of-band paths take their root
and path as base64 argv from the hub with the same property (`__main__.py:76-104`).

For Charon this is coherent, and the docstring quoted above says as much: the trust boundary is the SSH key and
everything past it is one user, so containment is an accident guard and not a wall. The consequence is that
there is exactly one security boundary in the whole system and no depth behind it. For anything that wants two
people, or an untrusted agent on the same box, the peer bus and the file surface have no authorization layer to
tighten. They would each have to grow one.

### The peer reservation leaks, and the leak wedges the session it leaked on

This is the most concrete defect found. `peer_target_active` is set in `peer_send` (`server.py:1091`) and
cleared in four places: normal completion (`:450-451`), a delivery exception (`:1101`), timeout expiry
(`:391-393`) and a late target status event (`:415-416`).

The timeout path clears it **only if the target is still `active` or `failed`** (`server.py:391-392` against
`_PEER_RECEIVE_STATUSES`, `:146`). A target that died, slept or was killed mid-request reads as `"error"`, so
the reservation is deliberately kept, waiting for a resumed turn that will never arrive. And `kill_session`
never touches peer state: it clears `sessions`, `rings`, `subscribers`, `recent_input_ids` and the event log
(`server.py:2066-2086`) and leaves `peer_target_active`, `peer_messages`, `peer_timeout_tasks` and
`peer_send_times` alone.

The consequences chain:

- The reservation is permanent for the daemon's lifetime, and it is **persisted and re-armed across restarts**.
  `_peer_messages_for_state` protects timed-out reserved rows (`:288-293`) and `_restore_peer_messages`
  reinstalls the reservation (`:326-327`).
- `_inject_peer_reply` refuses to deliver while the source is in `peer_target_active` (`:511-513`), so a stale
  reservation on a session blocks *that session's own* incoming replies for the full 10 minute deadline and then
  returns silently.
- The ledger bound is defeated. `_prune_peer_messages` treats reserved rows as non-removable (`:263-269`) and
  `_peer_messages_for_state` returns the protected set unconditionally even past the 200 cap (`:295-297`), so
  leaked reservations grow `state.json` without limit.

The only test-suite reference to `peer_target_active` is one line of setup (`agent/tests/test_peer_mcp.py:214`).
Nothing covers the timeout, the leak, or the injection deadline.

### A peer message can go missing, quietly

Each of these ends in a bare return with no event, no error and no retry:

- **The reply never lands.** `_inject_peer_reply` waits up to 10 minutes for the sender to be receivable and on
  deadline returns with `reply_injection` still pending (`server.py:508-519`). Rescheduling happens only on a
  `ready` event for that session (`:639-644`) or a daemon restart (`_resume_peer_runtime`, `:546-551`). The
  mitigation that does exist is a good one: the reply is emitted as a durable `external_message` first
  (`:475-487`), so the human sees it even when the model never does.
- **A tool-only turn reads as a failure.** If the target's turn produces no assistant text the request is marked
  failed with "target turn produced no assistant reply" (`server.py:459-462`). A legitimate turn that only ran
  tools is indistinguishable from an error.
- **The partial answer is runtime-only.** `_reply_buffer`, `_turn_error` and `_completing` are stripped before
  persistence (`server.py:300-302`). A `SIGKILL` mid-turn loses the partial reply, the row stays accepted, and
  `_arm_peer_timeout` recomputes the remaining time from `created_at` (`:375-376`), so a daemon that was down
  longer than ten minutes expires the request immediately on the way back up.
- **Correlation can be lost after a timeout.** The reservation may have been popped (`:391-393`) and a restart
  loses the session object's `_active_peer_request_id` (`session.py:1071-1073`). `_emit`'s fallback correlation
  is exactly that reservation (`server.py:586-592`), so with both gone a later stop event correlates to nothing.

Nobody hangs, because `peer_send` returns `accepted` immediately (`server.py:1124-1131`). The failure mode is a
silent non-answer, which for a model waiting on a delegated task is the harder one to notice.

### The holder is POSIX-only, and a slow box can orphan one permanently

The mechanism is `pty.fork()`, Unix domain sockets, `start_new_session=True` and hangup teardown. There is no
Windows analog: ConPTY has no reattach primitive and no way to hand a pseudo console to a new parent, which
atrium already records as an open risk (`docs/architecture-v2.md:633-636`).

The real fragility is the reattach budget. `attach()` allows 2 seconds to connect and 5 for the hello line
(`shell.py:150-154`, `:235-255`). On failure the boot scan `unlink()`s the socket (`server.py:2386-2389`) and
`cleanup_orphans` then wipes that shell's event log (`:2393`). A loaded box, a wedged holder or a future
protocol change therefore deletes the socket of a **live** holder, whose bash keeps running, unreachable by any
agent, with nothing that will ever reap it. Only the holder's own signal handler can end it
(`holder.py:466-474`). Reaping happens at agent boot and nowhere else (`server.py:2366-2406`), so there is no
periodic sweep to catch it later. `KillMode=process` is what stops systemd from cleaning up, and that is the
same setting the survival property depends on.

`holder.py:81` defines a `PROTO_VERSION` and sends it in the hello. `shell.py:256-272` never reads it. The
version field that would have made that failure diagnosable is present and unused.

Two smaller notes, one correcting an easy assumption. The reboot case is handled correctly: holders die with the
box and the next agent boot cleans the files. And the spool cap is lossy but **not** silent, since overflow
reopens the file and writes a visible marker into the terminal stream (`holder.py:197-212`). The caveat is that
truncation drops everything accumulated rather than the excess, so a marker can stand in for up to 8 MiB, and a
spool write failure loses output with only a line on stderr (`:211-212`).

### Errors on the state write are swallowed

`_save_state_now` catches everything and prints a traceback (`server.py:229-230`). A full or read-only disk means
the daemon keeps serving with a state file that is silently frozen and nothing tells the hub. The write itself is
correct (see section 2), and the parent directory is not fsynced, so a host power loss can lose the rename even
though a `SIGKILL` cannot.

A `SIGKILL` mid-turn also leaves a specific mess. `to_persist` rewrites `starting` and `thinking` to `active`
(`session.py:1047-1048`) and `_restore_existing` relaunches those (`server.py:795-839`), but the prompt that was
in flight is not re-sent. The result is a live, resumed session whose last user message may have produced
nothing at all, with no marker saying so. `sleeping` and `killed` are correctly not resurrected (`:816-821`).
`recent_input_ids` is also lost, so a hub retry carrying the same `client_message_id` can re-execute a prompt,
which is the exact failure that key exists to prevent.

### Version skew is handled per call site, not negotiated

There is no protocol version constant in `protocol.py` and `hello` carries only `agent_version`
(`lib/types.ts:63`). An unknown method returns `-32601` (`server.py:918`) and each hub call site catches it and
degrades individually (`lib/server/claude/git.ts:147-150`, `lsp.ts:38-40`, `sessionOps.ts:3072-3075`,
`sessionRpc.ts:29-31`, `AgentClient.ts:384`, `:422-425`). So a newer hub against an older agent loses features
one wrapped call at a time, anything unwrapped surfaces as a raw error, and **changed semantics on an existing
method are invisible**, because the name is the only contract. The mitigation is a documented rule enforced by
lint, "bump `__version__` on ANY agent change, it is the ONLY propagation signal" (`lib/version.ts:62-66`), plus
a build-time check that keeps the method list and the types aligned (`scripts/check-protocol-sync.mjs`). That is
a real mitigation resting on every future contributor reading a comment. Note also that `compareVersions` is not
semver: there is no prerelease ordering and `"1rc1"` falls back to a string compare (`lib/version.ts:22-29`).

Deployment has the matching gap. The pyz is piped over ssh and `mv`'d atomically, but nothing verifies what
landed (`bootstrap.ts:183-213`). The hub reads the hash of its own artifact (`lib/server/agent/builtPyzSha.ts:22-33`)
and then trusts whatever version the running agent self-reports. And auto-update runs unattended every 30
minutes and restarts the daemon (`sdkWatch.ts:64`, `:112`), gated on sessions that are thinking or starting and
on unanswered permissions (`:138-158`) but not on shell activity. That last one is mostly fine, because shells
live in holders and `KillMode=process` leaves them alone, which is the property doing the work.

### Where the tests are, and are not

Coverage is better than the shape of the project suggests: 39 Python test files including
`test_single_instance.py`, `test_state.py` and `test_integration_daemon.py`, plus around 63 vitest files on the
hub covering the parts that bit before (`replayExactness`, `messageOrderEpoch`, `sessionPermissionScope`,
`secretsAtRest`, `agentVersionStaleness`). The gaps are specific and they line up exactly with the defects above:
the peer reservation lifecycle, the `_inject_peer_reply` deadline, and holder attach failure unlinking a live
socket. Those are the three places where a failure costs someone's work rather than a repaint.

## 5. Worth stealing, ranked

### 1. The peer bus: handles, discovery, and a message that carries its provenance

**Adopt the shape. Do not port the code, and do not port the delivery mechanism.**

This is the one thing `CLAUDE.md` says atrium has no answer for at all, and Charon's version is concrete enough
to build from. What transfers is the four-part shape: a stable handle, a `list` tool as mandatory discovery, a
`send` that returns acceptance rather than an answer, and status tools so a model can ask instead of guess.

How it maps, concretely:

- **The handle already exists.** `task.wire_name` is `UNIQUE` (`internal/store/schema.go:38`) and is already the
  routing key every hook posts under. `store.Register` upserts by it (`internal/store/tasks.go:90`), and
  `Qualify` already namespaces it for the multi-node case (`internal/store/tasks.go:563-565`). Nothing new is
  needed to address a session.
- **Identity does not need argv.** Charon passes `session_id` on the command line because its daemon spawns the
  session. Atrium's runner spawns the MCP server, so the MCP process inherits the session's cwd and environment
  and can compute the same name the hook computes: `agentName` is `ATRIUM_AGENT_NAME` or the basename of the cwd
  (`internal/cli/join.go:64-75`). Reuse that function and the peer tool is gated under the same name the session
  is gated under, which is the property the comment there already insists on.
- **Transport is HTTP, not a socket.** Charon dials a Unix socket because its daemon listens there. Atrium's
  daemon already has an agent-facing listener with a fixed route table
  (`internal/daemon/daemon.go:481-487`), and `whereami.go` already exists so a process in a session atrium did
  not start can find it with no flag. Add `/peer/list` and `/peer/send` next to `/submit` and `/activity`. A new
  `atrium peer-mcp` subcommand, sibling to `atrium hook`, is the whole client.
- **The busy gate is a status, not a second dict.** Do not copy `peer_target_active`. Atrium's `task.status`
  already has `needs-permission`, `needs-input` and `shelved` (`internal/store/schema.go:30-31`), and the
  permission chain already refuses on a shelved card (`internal/daemon/daemon.go:376-378`). "Can this session
  receive a peer message right now" is a query over that column plus the pending message count
  (`UndeliveredCounts`, `internal/store/messages.go:103`). Charon's in-memory gate plus a JSON snapshot would be
  a regression against the halt (`docs/architecture-v2.md:382`), and the leak described in section 4 is what
  that shape costs: a reservation held in a dict with four scattered release paths, one of which declines to
  fire when the target is dead, wedges the session it leaked on and then survives a restart. A status column
  and a query cannot leak, because there is no second place for the truth to live.
- **Delivery is the existing message queue.** This is the substitution that makes the feature honest for atrium.
  Charon injects the envelope with `send_input` as though it were typed, which is safe only because no human is
  in its loop. Atrium supervises a real terminal a human may be typing into right now, and
  `docs/supervision-design.md:60-65` already rules that out. The right target is the `message` table
  (`internal/store/schema.go:248-256`) and the two hooks that drain it: the permission hook carries text on the
  next tool call, and the Stop hook reaches an idle session (`internal/daemon/messages.go:14-28`). A peer
  message becomes a queued message with a sender, delivered by machinery that already exists and already sits
  second in the permission chain.
- **Steal the envelope wording, because atrium already agrees with it.** `messageBanner`
  (`internal/daemon/messages.go:35-51`) exists for exactly Charon's reason: a model reads a blocked tool call as
  a policy refusal unless told otherwise. A peer variant needs one more thing Charon's has and atrium's does not
  yet need: who sent it, and that it was not the human (`server.py:1065-1078`).
- **Steal every guardrail.** The rate limit, the size cap, the self-send refusal and the do-not-reply-by-sending
  rule (`server.py:1023-1054`) are the difference between a peer bus and two agents talking to each other until
  the budget runs out. They are ten lines each and they are the reason Charon's works.
- **Leave the reply capture alone for now.** Charon buffers `assistant_text` because it holds the SDK's event
  stream. Atrium has no "this turn's final text" event. `Stop` fires at a turn boundary
  (`internal/daemon/messages.go:78`) and could carry a reply if the hook posted the turn's last message, but
  that is a new hook contract, not a port. Ship send-and-status first, where the sender polls
  `get_message_status`, and treat automatic reply delivery as a second stage. Charon's own version of this is
  the least settled part of its peer bus: a turn that only ran tools is recorded as a failure
  (`server.py:459-462`), and an undeliverable reply is written where the human can see it but silently never
  reaches the model (`server.py:475-487`, `:508-519`). If atrium builds the second stage, that split is the
  right instinct to keep and the silence is the part to fix.

### 2. Out-of-band approvals, as one more client of an endpoint that already exists

**Adopt as-is in shape, and nothing in the permission chain changes.**

`docs/backlog.md` already names this as the gap that matters most, and the code holds the claim up.
`telegram.ts` is small precisely because it never becomes a second source of truth: it polls `getUpdates`, posts
an inline keyboard, and calls the identical hub function the browser calls (`telegram.ts:255-285`, `:322-324`),
authorized by one allow-listed chat id (`:300`, `:370`).

Atrium's equivalent is a client of `GET /v1/permissions` and `POST /v1/permissions/{id}/decide`
(`internal/api/api.go:148-149`). `onPermRequest` does not need to know it exists. The gap this closes is real
and citable: atrium's alerting is Web Notifications through a service worker that displays but never subscribes,
so there is no `pushManager.subscribe` anywhere in the board (`internal/api/web/index.html:4484-4512`,
`:4502-4522`). An alert needs the tab open. Charon has both a bot and stored Web Push subscriptions
(`lib/db/schema.ts:531-539`).

**What has to change for atrium's shape.** A block carries a free-text reason, which is atrium's whole "no, do
X instead" story, and an inline keyboard has no text field. Either the bot takes a reply-to-message as the
reason, or a phone denial sends a fixed reason that says so. Do not let the transport quietly turn a considered
block into a bare no. And atrium refuses timeouts, so the countdown Charon shows becomes "waiting since", which
`task.waiting_since` already carries (`internal/store/schema.go:34`).

### 3. The detached PTY holder, for POSIX hosts only

**Adapt, and write down that it does not close the Windows gap.**

`docs/supervision-design.md:84-87` asks what happens to a supervised runner when the daemon dies unexpectedly,
and names window mode's survival as an argument for keeping window mode. The holder is the missing third option:
not "the daemon owns the pty and dies with it" and not "the runner detaches and atrium loses control", but "the
pty is owned by a small process the daemon spawned detached, and the daemon reattaches over a chmod-0600 socket
when it comes back" (`holder.py:14-19`, `:59-60`, `:222-241`).

**What has to change.** Everything about it is POSIX. Atrium's supervision runs on Windows first and
`docs/architecture-v2.md:633-636` already states there is no ConPTY reattach and that the answer there is
resume ids. So this is worth building for a Linux-hosted daemon and worth an explicit sentence in
`docs/supervision-design.md` saying the Windows gap stays where it is. Note also that Charon does the same
resume-id thing atrium plans, at the provider level, by passing `resume=self.claude_session_id` into the SDK
(`session.py:2012-2013`). A restart costs the terminal and not the conversation. That is atrium's own stated
plan, already working in someone else's tree.

Take the offline spool with it (`holder.py:83`, `:197-212`), including the visible truncation marker, which
atrium's own bounded in-memory buffer (`docs/supervision-design.md:44-53`) should arguably have too. Improve on
two things: truncate the excess rather than the whole file, and do not let a 2 second connect budget delete a
live holder's socket (section 4).

### 4. Optimistic concurrency on any write endpoint, and CodeMirror 6 over Monaco

**Adopted, both halves, and the editor turned out not to need CodeMirror.**

The `expected_sha256` precondition (`fsnav.py:503-521`, `:532-540`) is the same shape as HTTP `If-Match` and it
is the answer to the question `docs/backlog.md` asks about a save racing the agent. It is
`internal/api/filetext.go` now, on `GET`/`PUT /v1/tasks/{id}/files/text`, and it went in on day one of that
endpoint rather than after the first lost edit.

It did NOT land on `/v1/browse`, which this section suggested. That endpoint lists directories and never reads
a file, and the path question it seemed to answer was the thing `docs/file-transfer-design.md` refuted:
`browse.go` had no notion of containment to reuse. `internal/safepath` is what both now go through.

**Adapt on one point.** Charon's force-overwrite is `expected_sha256=None`, which means "no precondition" and
"deliberately clobber" are the same value. Made different: an absent field is an error, not consent.

The editor itself is CodeMirror 6, not Monaco (`app/CodeEditor.tsx:2-16`), with lazy per-file language modes from
`@codemirror/language-data` and an LSP client wired in (`app/lspClient.tsx`, imported at `CodeEditor.tsx:8`).

**The claim that it "vendors as a static asset the way xterm.js already does" was wrong, and it was checked
later.** CodeMirror 6 ships `dist/index.js` as an ES module with bare specifiers (`import ... from
'@codemirror/view'`) across seven packages, and no prebuilt bundle. xterm.js vendors because it ships UMD: one
file, no imports, a global afterwards. Using CodeMirror means adding rollup or esbuild, which means adding a JS
build step to a repo that has none and whose board is one HTML file on purpose. Charon pays that cost already,
because it is a Next.js application.

So the conclusion this section reached does not transfer. Atrium's answer is a plain textarea over the
concurrency model below, and highlighting is not worth a bundler. `docs/backlog.md` has the full argument under
"An editor in the board".

### 5. One upload pipeline for drag, paste and picker

**Adopt the pattern. Atrium already has the harder half.**

Charon's answer is small because one function serves three gestures and because it never tries to outsmart the
model's own file reader: upload, get a remote path, splice the path into the prompt at the caret
(`ClaudeSessionView.tsx:1259-1264`, `:1336-1342`). Progress is visible because the path appends as each upload
lands, and the input is covered while one is outstanding so the caret cannot move underneath
(`:1344-1349`).

**Built.** `POST /v1/tasks/{id}/files` takes the bytes, `.atrium/incoming` under the card is where they land,
and one function serves paste, drop and the picker exactly as this section argues.

**The paragraph that used to be here was wrong and is worth recording as wrong**, because it is the mistake this
whole comparison nearly caused. It said the landing spot should be derived from `task.worktree` through
`browse.go`, "which already decides what a safe path is". It did not: its entire treatment of caller input was
`filepath.Clean`, with no root and no symlink resolution. `docs/file-transfer-design.md` opens by refuting this
premise, and the answer was a new primitive, `internal/safepath`, which `browse.go` was itself later bounded
through. Reading a peer's design and assuming the local equivalent does the same job is the specific failure.

### 6. An idempotency key on the prompt endpoint

**Adopt. It is a few lines and atrium already believes in it.**

Charon's `send_input` accepts a client message id and remembers a bounded set, so a caller can retry a lost
response without sending the prompt twice. Atrium has exactly this concept for permissions already:
`permission.dedup_key` is `UNIQUE` (`internal/store/schema.go:60-61`) and the reason is written down at the call
site, that a daemon which crashed between recording a decision and answering would otherwise ask twice
(`internal/daemon/daemon.go:338-342`). `POST /v1/tasks/{id}/prompt` takes only `{"text": ...}`
(`internal/api/api.go:516-533`), so a human who retried after a lost response sends the prompt twice. Same
argument, different write path.

**Adapt on one point, which Charon gets wrong.** Its accepted-id set is in memory and is dropped by a
`SIGKILL` along with the rest of the runtime state (`recent_input_ids` is cleared at `server.py:2066-2086` and
is not persisted), so the retry that survives a crash is exactly the one the key cannot catch. Atrium's
`dedup_key` is a `UNIQUE` column in the store for that reason. Put the prompt key in the same place, not in a
map on the daemon.

### 7. Git as an argv-only, timeout-bounded, never-raising service

**Adapt if and when atrium grows a git surface. Not otherwise.**

`git.py`'s hardened environment is the transferable part, not the RPC list: `GIT_TERMINAL_PROMPT=0`, empty
askpass variables, `GIT_OPTIONAL_LOCKS=0`, `LC_ALL=C`, `ssh -o BatchMode=yes` (`git.py:75-91`), so a git call in
an unattended process fails fast instead of blocking on a prompt nobody will see. Every call timeout bounded
(`:36-41`), failures as `ok: False` plus a reason rather than exceptions (`:128`, `:160`), and a write surface
that is an allow-list with no force push (`:16-19`). If atrium ever shells anything on a runner's behalf, this
is the posture to copy.

## 6. Not worth stealing, and why

Refusal is a legitimate conclusion, and most of these are refusals against a decision atrium already wrote down.

**Approval timeouts.** `docs/architecture-v2.md:102-129` is the argument, and it is stronger than the feature.
No timeout on the agent side, by design, because a timeout either wakes the model or guesses at an answer, and a
stale auto-deny is materially different from a human's deliberate no. Charon accepts that cost knowingly for a
machine left unattended. Atrium's three lifecycle rules exist precisely so a waiting card cannot be put down
without answering, and a timeout would reintroduce the stranding those rules close off. Refuse.

**Hub dials out to each machine as the federation transport.** `docs/federation-design-v2.md:107-119` already
has the table. Charon's direction requires the control plane to have a route to every managed machine, which is
true of a rented VPS running sshd and false of every case atrium's forum design targets: a laptop behind NAT, a
container on a bridge network, a pod. This is not "SSH is worse", it is two projects solving different
reachability shapes. Refuse the shape.

Keep one idea from it. SSH's `ControlMaster` is exactly the connection-pool problem
`docs/forum-implementation.md:133` solves by hand, and SSH carries per-connection authentication in the
transport, which is the whole difficulty `docs/federation-design-v2.md:141-209` works through for the forum
side. That makes "leaf dials out over SSH" a real alternative to name in stage 7's transport options
(`docs/federation-design-v2.md:587`), with its own cost written down: it trades "atrium never handles an
identity" (`docs/overlays.md:13-16`) for "the forum now runs an SSH server and manages `authorized_keys`". A
different set of operational costs, not a smaller one. It answers nothing for the pod with no sshd.

**A second durable store on the agent side.** `event_log.py` exists because the hub can be disconnected while a
separate agent process on a separate machine keeps producing events. Atrium's daemon and its store are the same
process, and the `event` table already is the single durable log (`internal/store/schema.go:41-50`). Adopting
the split would solve a problem atrium's architecture does not have, and would import the degradation described
above. Refuse, and refuse it loudly, because the ADR reads persuasively enough that someone will propose it by
analogy.

**A session-scoped allow-list keyed by tool name.** Atrium's `perm_rule` is strictly more precise: prefix, glob
or folder matching with most-specific-wins (`CLAUDE.md`, "The permission chain", step 4, and
`internal/store/schema.go:64`, rebuilt at `:293`), a durable decision log with what answered each one
(`permission.decision`, `permission.reason`, plus `by` on the event, `internal/daemon/daemon.go:351-356`), and
an import and export path (`internal/api/api.go:154-156`). Charon has one JSON array of bare tool names per
session (`lib/db/schema.ts:244`). Atrium is ahead here and should stay.

Worth reading their comment anyway, because it is a validation and not a competition: they moved that set from
memory onto disk after finding the hub restarts far more often than a session lives, so "the answer was re-asked
for work the user had already approved minutes earlier" (`lib/db/schema.ts:231-243`). That is the same
observation `perm_rule` was built from.

**A total-bypass permission mode.** Charon's `auto` skips the hook entirely (`session.py:1234`). Atrium's auto
mode is the opposite trade: approve without asking, record everything, sit last in the chain so replays,
messages, shelving and rules all still win, and pay for it with a review (`docs/auto-mode.md`,
`internal/daemon/daemon.go` step 5). Atrium already answers this better.

**Holding provider credentials, and signing in through the board.** Charon has in-hub OAuth for Claude and a
device-code flow for Codex (`CHANGELOG.md`, "In-hub sign-in for both backends"), and an encrypted Anthropic key
at rest (`SECURITY.md:51-59`). It needs this because it *is* the SDK client. Atrium does not replace the runner
and does not hold the subscription (`CLAUDE.md`, "Out of scope"). Adopting this would change what atrium is.

**Any of the authentication.** One shared `MASTER_PASSWORD` for a whole fleet (`lib/server/auth.ts:20-24`), a
vestigial `users` table (`:67-81`), and a Telegram allow-list of one chat id (`telegram.ts:300`). `CLAUDE.md`
rules authentication out of scope and `docs/overlays.md:3-5` says why: an auth layer invented here would be
worse than the overlays that exist. Atrium's answer, driving zrok or OpenZiti and answering on the overlay's own
listener with no proxy hop (`internal/daemon/overlay_native.go:19-33`), is a better answer to the same want and
is already built.

**Injecting text into a session as though it were typed.** `send_input` works for Charon because its sessions
are SDK turns with no human at a keyboard. Atrium owns a terminal a human may be mid-command in, and
`docs/supervision-design.md:60-65` already decided input is not fanned out. The atrium-shaped substitute is item
1's message queue. Refuse the mechanism, keep the goal.

**The zipapp plus venv deployment split.** It solves reproducible-interpreter versus SDK churn, a problem that
only exists because Charon ships provider SDKs. Atrium ships one static Go binary. Named for completeness.

## 7. Corrections to earlier assumptions

`docs/backlog.md`'s Charon section was written from the author's public post, before the code was read. What it
got wrong, and what settles each one.

| The backlog claimed | The code does | Settled by |
| --- | --- | --- |
| Go and TypeScript, supervising agents like atrium | Python agent driving the Claude Agent SDK **in process**, hooks as Python callables, no interactive CLI anywhere. Hub is Next.js. No Go in the tree | `session.py:30`, `:2000-2010` |
| "Charon's approvals are per request" | One standing-rule mechanism exists. "Always" persists a JSON array of bare **tool names** per session, hub-side. One "Always" on a `Bash` card approves every later `Bash` command in that session | `sessionOps.ts:1847-1850`, `:1052`, `lib/db/schema.ts:244` |
| "Times out at 10 minutes for Claude and 30 for Codex" | Three timeouts. 10 min for a Claude tool permission, **30 min for Claude's `AskUserQuestion`**, 30 min for Codex, and Codex can override its own with `autoResolutionMs` | `session.py:1281`, `:1313`, `codex_session.py:1886`, `:1856` |
| "A small IDE", and "the obvious candidate is the Monaco editor" | CodeMirror 6, lazy per-file language modes, a remote LSP client behind it. Not transferable: it ships ES modules with bare specifiers and no bundle, which Charon can use because it is a Next.js app and atrium cannot because its board is one file | `app/CodeEditor.tsx:2-16`, `lsp.py` |
| "Sessions talk to each other over MCP" | True, and not the mechanism the phrase suggests. The MCP server holds no state and no routing, it is a thin argv-identified adapter over a Unix socket. The reply is not fetched by a tool: the daemon captures the target's turn text and injects it into the sender | `peer_mcp.py:97-116`, `server.py:503-544` |
| "Charon is session-shaped. Atrium's cards carry history across restarts" | Charon's sessions are durable too: `claude_sessions` rows, an event log with a `seq` resume cursor, SDK resume ids. The difference is not durability, it is that Charon has no unit above a session and no status a human curates | `lib/db/schema.ts:143-160`, `event_log.py`, `session.py:2012` |
| "No exposed ports on the remote side" | True of *new* ports. It requires an inbound `sshd` the operator already runs, so the far side is by definition already accepting connections. That is why the shape does not reach a laptop or a pod | `sshShared.js:115-121`, `docs/federation-design-v2.md:75-97` |
| A Telegram bot is the whole of its out-of-band reach | Also Web Push with stored subscriptions, so an alert reaches a closed tab. Atrium's side needs correcting too: it registers a service worker but never calls `pushManager.subscribe`, so it needs the board open | `lib/db/schema.ts:531-539`, `internal/api/web/index.html:4484-4512` |
| Implied: that atrium's overlay claim needed rechecking | Holds up, and is stronger than the section says. The board answers on the overlay's own `net.Listener`, no child process and no proxy hop | `internal/daemon/overlay_native.go:19-33` |

Three further corrections, to the spike rather than the backlog:

- The spike assumed the "Always" button meant a session-mode flip and could not confirm it. It is neither a mode
  flip nor nothing. `respondPermission` never touches `permissionMode`, and the daemon discards `always`
  entirely for Claude (`session.py:795-800`). The rule is created and enforced in the hub.
- The spike called the peer state "in-memory-plus-best-effort-snapshot". The snapshot is not best effort: it is
  mkstemp, `json.dump`, flush, `fsync`, `os.replace` (`state.py:63-71`), written eagerly rather than on the
  debounce for every path that matters, and written before delivery on purpose (`server.py:1093-1097`). The
  argument for atrium using its store instead survives, but it is an argument about a second place for the truth
  to live, not about a careless write.
- The spike said the peer bus "never needed" a lock because inter-agent messaging is not file ownership. True,
  and it did need a reservation, which is the thing that leaks. Section 4.

## 8. What remains unverified

Each item names what access would settle it.

- **Nothing was run.** Every claim here is a static read of a shallow clone taken on 2026-09-03, so no commit is
  pinned and a re-clone may differ. Settled by: cloning at a pinned commit and standing up the Docker compose
  install against one machine.
- **Behaviour under real failure.** The peer reservation leak and the missing-reply paths in section 4 are read
  from the code, not observed. The chain is traceable line by line and the release paths genuinely do not cover
  a dead target, but whether the wedge is reachable in ordinary use is a different claim. Settled by: two
  sessions on one machine, a peer request in flight, `kill_session` on the target, and then a peer reply aimed
  at the sender. Then the same with `kill -9` on the daemon mid-turn.
- **How often the holder attach budget actually fires.** The code path that unlinks a live holder's socket
  exists (`server.py:2386-2389`, budgets at `shell.py:150-154`), and there is no evidence either way about a
  2 second connect on a loaded box. Settled by: loading the machine and restarting the daemon, or adding a
  counter.
- **`_is_safe_bash`.** Referenced at `session.py:1195` as the whole of plan-mode Bash containment and not read.
  No claim is made about it here. Settled by: reading it.
- **Whether any hub call site passes an `fs_*` `root` other than a session cwd.** The agent provably does not
  constrain it (`server.py:1139-1157`), which is the fact that matters for section 4, but the hub's call sites
  were not audited. Settled by: grepping every `fs_read` / `fs_write` caller in `lib/`.
- **The Codex parity claims.** The Codex peer, terminal and upload paths are assumed to share `peer_mcp.py`,
  `holder.py` and `fsnav.py` with the Claude paths, which the imports support, but `codex_session.py` (3150
  lines) was read only around the approval dispatch. Settled by: reading it end to end.
- **What a killed-mid-turn prompt does to the resumed conversation.** Section 4 says the in-flight prompt is not
  re-sent. Whether it survives inside the provider's own transcript, and so reappears on resume, depends on
  `claude-agent-sdk` internals that are not in this tree. Settled by: reading the SDK, or killing a daemon
  mid-turn and looking at what the resumed session believes it was asked.
- **Charon's Git and LSP surfaces beyond their entry points.** Both files were read for mechanism and bounds,
  not for correctness of the porcelain parsing or the LSP edit application (`git.py:233`, `lsp.py:609-636`).
  Settled by: using them.
