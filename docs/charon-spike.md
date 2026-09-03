# Charon spike: what atrium should learn from it

Source read: `github.com/Lomchat/charon`, cloned shallow to `D:/tmp/charon` (outside the atrium tree, not
committed here). Apache 2.0, single user, self hosted, six months of the author's own daily use per the
project's own changelog and ADR.

**Correction to the brief.** The stack is not Go/TypeScript. The VPS-side daemon (`agent/`) is Python
(standard library plus the `claude_agent_sdk` and `codex` provider SDKs, shipped as a zipapp). The hub
(`app/`, `lib/`) is a Next.js app in TypeScript with a Node `server.js`. There is no Go anywhere in the
tree. This matters for the comparison: Charon's agent process talks to the *SDK*, not to the interactive
`claude` CLI a human would type into, which changes the shape of nearly every answer below.

All file paths below are relative to the Charon clone unless stated otherwise. Atrium paths are relative to
this repo.

## 1. Agent to agent MCP

**Mechanism, precisely.** Every live Claude or Codex session on one VPS gets a stable, human-chosen
`@handle` (`agent/charon_agent/server.py:1938` `set_session_handle`, validated against
`_PEER_HANDLE_RE = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$")` at `server.py:139`). Both
providers launch a small local stdio MCP server, `charon_peer`
(`agent/charon_agent/peer_mcp.py`), as one of their own configured MCP servers. It exposes five tools:
`list_sessions`, `send_message`, `get_message_status`, `get_conversation`, `list_inbox`
(`peer_mcp.py:29-94`). Every tool call in that stdio process does nothing but open a fresh Unix socket
connection to the same daemon that owns the session (`_agent_call`, `peer_mcp.py:97-116`) and calls one of
five JSON-RPC methods: `peer_list`, `peer_send`, `peer_status`, `peer_conversation`, `peer_inbox`
(`server.py:874`, dispatched from `_handle_meta_rpc`, `server.py:942-1131`). The MCP process itself carries
no session state and no routing logic; it is a thin RPC-to-MCP adapter, and the daemon is the only thing
that can see the live session table.

**Addressing.** The handle is scoped to one VPS's one daemon process (`self.sessions`, an in-memory dict).
There is no cross-VPS router: the ADR states this explicitly ("No cross-VPS router is implied by this
decision", `docs/adr-001-charon-agent.md:189`). `peer_list` returns every other session's handle, provider,
status and an `available` flag (`server.py:942-966`).

**Discovery.** `list_sessions` is the discovery call. The model is instructed (via the MCP server's
`initialize` response `instructions` field, `peer_mcp.py:16-27`) to call it before ever guessing a handle
exists or is unavailable.

**Is there a lock or a lease?** Two different mechanisms, and neither is a file lock — Charon's own worked
example is inter-agent messaging, not file ownership, so it never needed one:

- **A one-message-in-flight gate per target session**, not a lease with a duration. `self.peer_target_active:
  dict[str, str]` maps a target session id to the one message id currently occupying its turn
  (`server.py:204`, set at `server.py:1091`). A second `peer_send` at the same target while one is still
  outstanding is refused synchronously with `ERR_SESSION_DEAD, "peer @{handle} is already processing a peer
  request"` (`server.py:1046-1047`). The gate releases when the target's turn ends
  (`_complete_peer_request`, `server.py:445-451`) or when a 10 minute reply timeout fires with the target
  now idle (`_arm_peer_timeout`, `server.py:368-397`, `_PEER_REPLY_TIMEOUT_S = 10 * 60` at `server.py:145`).
- **File races are handled separately, by optimistic concurrency on the write itself**, not by anything
  peer-MCP-shaped. See question 6.

**Delivery and reply, precisely.** `peer_send` wraps the message in an XML-ish envelope
(`<charon-peer-message from="@handle" ... expects-reply="true">`, `server.py:1065-1074`) and calls
`target.send_input(envelope, peer_request_id=message_id)` — it is injected as if it were the next thing
typed into that session, tagged with the message id so the daemon can correlate the target's *next turn's
own stop event* back to this request (`_observe_peer_event`, `server.py:405-443`, wired through
`peer_request_id`). The daemon buffers the target's `assistant_text` deltas for that turn
(`row["_reply_buffer"]`, `server.py:425-429`) and on `stop` calls `_complete_peer_request`
(`server.py:445-489`), which marks the message `replied`, stores the captured text as `reply`, and then
*re-injects that reply back into the source session* as another synthetic input, wrapped in a
`<charon-peer-reply>` envelope (`_inject_peer_reply`, `server.py:503-544`). The model on the sending side
never calls a tool to fetch the answer; the daemon delivers it proactively, waiting up to another 10
minutes for the source session to be idle and available before injecting (`_inject_peer_reply`, the
`while` loop at `server.py:511-517`). `get_message_status` / `get_conversation` / `list_inbox` exist only so
a model can check "did they answer" without guessing, per the tool descriptions.

**Verdict: adopt the shape, not the code.** This is the one mechanism `CLAUDE.md` says atrium has "no answer
for at all," and it is worth building, close to as-is:

- The handle-as-routing-key, one MCP server registered per session, discovery via a `list` tool, is a clean
  fit for atrium's `wire_name`/task model. Atrium already has a stable per-card identity; it would need to
  expose it as an MCP tool surface the same way, likely as a small stdio (or better, streamable-HTTP, since
  atrium already runs an HTTP surface) MCP server that calls back into `/v1/tasks` the way `charon_peer`
  calls back into the Unix socket.
  - **Adapt, don't copy, on transport.** Charon's peer MCP dials a Unix socket per call because the daemon
    already listens there. Atrium's daemon already listens on loopback HTTP (`:7777`), so the natural
    analog is a small POST to a new agent-facing endpoint, not a new socket.
- **Adapt the busy gate as a status transition, not a bespoke dict.** Atrium already has a `status` column
  and a permission-style block/unblock discipline (`CLAUDE.md` "The permission chain"). A peer message
  arriving at a task that is mid permission-request, shelved, or already receiving one peer message should
  reuse those same primitives (a queued message, or a new event kind) rather than inventing a second
  in-memory gate. Charon's `peer_target_active` is a single global dict with no persistence: it is rebuilt
  from `state.json` on restart (`_restore_peer_messages`, `server.py:305-327`) rather than living in the
  durable store. Atrium's `event` table and durable `task` row are already the better foundation for this;
  copying Charon's in-memory-plus-best-effort-snapshot approach would be a regression against
  `docs/architecture-v2.md`'s halt guarantees.
- **Refuse the auto-delivery-into-the-conversation trick outright, or gate it hard.** Injecting a peer
  message as though the human typed it (`send_input`) works for Charon because its "sessions" are
  SDK-driven conversational turns with no human in the interactive loop at that moment. Atrium supervises
  the *actual* `claude` CLI a human may be looking at or typing into right now (`docs/supervision-design.md`
  "Input is not fanned out": two humans typing into one pty is expected). Silently injecting a peer's
  message into a live terminal a human is mid-command in would be actively harmful. The safe atrium analog
  is closer to atrium's own **message back channel** (`docs/architecture-v2.md` "Built since this document
  was written"): queue it, deliver it the way a human's message is delivered (typed when idle, carried on
  the next tool call, or via the Stop hook), and let the normal `needs-input` mechanics surface it —
  not force an extra unsolicited turn.
- **The reply-capture-by-turn-boundary trick is Claude-Agent-SDK-specific and doesn't transfer.** Charon can
  buffer `assistant_text` because it drives the SDK's fine-grained event stream directly. Atrium supervises
  a PTY and, separately, gates tool calls through hooks; it has no equivalent "this turn's final text" event
  today. Building a peer-reply feature honestly for atrium's PTY-supervised runners means treating a reply
  as "the next chunk of terminal output after the injected message, up to the next prompt," which is much
  fuzzier than Charon's SDK-native turn boundary, or restricting peer messaging to cooperative runners only.

## 2. The approval flow

**How the request is captured — this is the header finding.** Charon does **not** use Claude Code's
`PreToolUse` hook mechanism the way atrium does (a hook command declared in `.claude/settings.json` that the
CLI shells out to and that atrium's own binary answers). Charon runs the **Claude Agent SDK**
(`claude_agent_sdk.ClaudeSDKClient`, imported at `agent/charon_agent/session.py:30`) directly inside its own
Python process and registers **in-process Python callables** as hooks and as the SDK's `can_use_tool`
callback:

```python
hooks={
    "PreToolUse": [HookMatcher(hooks=[self._pre_tool_use])],
    "PostToolUse": [HookMatcher(hooks=[self._post_tool_use])],
    ...
    "Stop": [HookMatcher(hooks=[self._on_stop_hook])],
},
can_use_tool=self._can_use_tool,
```
(`session.py:2001-2011`)

There is no external process, no JSON-over-stdio hook contract, and no PTY output scraping for permission
capture. `_pre_tool_use` (`session.py:1165-1263`) runs synthetic auto-allow rules first (plan-mode safe
tools, snapshot bookkeeping, an explicit `auto` mode that is a total bypass) and only falls through to
`_ask_dashboard_permission` (`session.py:1265-1302`) when none apply. That function does the actual
"ask a human" step: it emits a `permission_request` event (which the hub relays to the browser over SSE and,
if configured, to Telegram) and `await`s an `asyncio.Future` that some other code path resolves.
`_can_use_tool` (`session.py:1304-1354`) is the separate path the SDK's own tool-classifier reaches when
Claude's built-in permission logic decides to ask, and it delegates to the same
`_ask_dashboard_permission` helper. Codex's equivalent is a JSON-RPC method dispatch inside
`agent/charon_agent/codex_session.py` reading `item/commandExecution/requestApproval`,
`item/fileChange/requestApproval`, `item/permissions/requestApproval` and MCP elicitation requests from the
Codex app-server protocol (`codex_session.py:1780-1899`), the same future-based await pattern.

**How the answer is routed back.** The browser posts a decision to the hub, the hub calls into
`AgentClient`/`sessionOps` (`getOrCreateStream(sessionId).respondPermission(...)`, referenced from
`lib/server/claude/telegram.ts:315-329`), which is an RPC call over the same SSH-carried Unix-socket
connection down to the daemon, which resolves the pending `asyncio.Future` keyed by the permission id in
`self._pending_perms` (`session.py:1280`, `:1312`). Telegram is not a separate channel into the daemon: it
is one more UI on the hub side that calls the exact same `respondPermission`/`respondQuestion` hub API a
browser click would (`lib/server/claude/telegram.ts:322-324`, `:360`), via a Telegram bot long-polling
`getUpdates` (`pollLoop`, `telegram.ts:255-285`) and rendering an inline keyboard
(`sendPermissionToTelegram`, `telegram.ts:148-183`). The chat id is pinned in settings and every inbound
update is checked against it (`telegram.ts:300`, `:370`) — this is the whole of Charon's Telegram auth: one
allow-listed chat id, no token exchange, no per-request signature.

**Timeouts, precisely — the brief's "10 and 30 minutes" needed correcting.** There are three distinct
timeouts, not two:

| Path | Timeout | Where |
| --- | --- | --- |
| Claude tool permission (`_ask_dashboard_permission`, both hook and `can_use_tool` paths) | 10 min | `session.py:1281` `timeout_seconds = 600` |
| Claude `AskUserQuestion` | 30 min | `session.py:1313` `timeout_seconds = 1800` |
| Codex command/file/permission approval and MCP elicitation | 30 min | `codex_session.py:1816`, `:1857` (or `autoResolutionMs` from Codex itself), `:1886` |

**What it sends when it fires.** On expiry the pending future is never resolved by a human; the `await
asyncio.wait_for(fut, timeout=...)` raises `asyncio.TimeoutError`, the code path returns a **deny**
(`PermissionResultDeny(message="timeout (10min without response from the dashboard)")`,
`session.py:1351`; the `_pre_tool_use` hook path returns `permissionDecision: "deny"`,
`permissionDecisionReason: "timeout/cancellation"`, `session.py:1249-1253`) and emits an
`interaction_resolved` event with `outcome: "expired"` (`session.py:1290-1293`, `:1322-1325`) so the UI can
show it was a timeout rather than a real denial. The tool call does not proceed; the model receives the
deny reason as its `PermissionResult`/hook output exactly as if a human had clicked deny, and continues its
turn from there.

**What breaks if atrium adopts a timeout — named as the brief asked.** Atrium's whole permission chain is
built on the invariant stated in `CLAUDE.md`: "there is no timeout on the agent side, by design, because a
timeout would either wake the model or guess at an answer," and `docs/architecture-v2.md` "A waiting card
cannot be put down without answering" spells out why: a stale auto-deny is materially different from a
human's deliberate no, and a model that receives a deny it did not ask for and cannot distinguish from a
real one may retry, route around it, or (per the same doc) explore a different tool to get the same result.
Charon accepts this cost deliberately and the ADR is honest about it being a real trade — a VPS-scale coding
agent left running for 10+ minutes unattended is treated as a normal outcome, not a bug. Atrium's design
explicitly refuses that trade for a documented reason (`docs/backlog.md`: "Atrium blocks until answered,
which is the correct default for something whose answer is a decision"). **Refuse this one.** It is a clean
example of a place Charon's answer does not survive contact with atrium's own stated invariants, and the
`CLAUDE.md` non-goals section is explicit enough that adopting a timeout would need to argue against a
documented decision, not just add a feature.

**What atrium should actually take from this section: the reachability channel, not the timeout.**
`docs/backlog.md` already names this ("Approvals reachable from a phone... this is the gap that matters
most: a gate you cannot answer from away is a gate you turn off") and it holds up under reading the code.
Charon's Telegram integration is small (`lib/server/claude/telegram.ts` is under 500 lines) *because* it
never becomes a second source of truth: it is a thin client of the same `respondPermission`/`respondQuestion`
calls the browser makes, gated by one allow-listed chat id, with the inline keyboard doing the entire UI.
Atrium's permission surface (`POST /v1/permissions/{id}/decide`) already has the shape this would sit on top
of. A Telegram (or generic webhook/push) bot that polls or subscribes to `GET /v1/permissions` and posts
decisions back through the existing endpoint would not need to touch the permission chain in
`onPermRequest` at all — it is a new client of an interface that already exists, which is exactly the kind
of addition `docs/architecture-v2.md`'s client/core separation was built to make cheap. Adopt this, scoped as
"one more client of `/v1/permissions`," not as a redesign.

## 3. Transport to many machines

**Exactly what it is.** The hub shells out to the operating system's own `ssh` binary as a child process
(`spawn('ssh', args, { stdio: ['pipe', 'pipe', 'pipe'] })`, `lib/server/agent/AgentClient.ts:474`). It is not
a Go/Node SSH library, not `socat`, and it opens no listening TCP port on the VPS beyond the sshd that was
already there for admin access. The remote command executed over that connection is `exec ~/.charon/venv/bin/
python3 ~/.charon/charon-agent.pyz --connect` (`lib/server/agent/sshShared.js:115-121`), and `--connect` is a
small stdin/stdout-to-Unix-socket proxy in front of the already-running daemon (ADR, "Transport and
protocol"). Every JSON-RPC call, every session's event stream, every shell's I/O, and separate one-shot SSH
invocations for byte-range file streaming and directory zip streaming (`buildAgentFileStreamSshArgs`,
`buildAgentZipStreamSshArgs`, `sshShared.js:133-164`) all ride SSH.

**Multiplexing.** One SSH `ControlMaster=auto` per VPS, keyed by a sha256 of `user@host:port`
(`controlPath`, `sshShared.js:59-66`), with `ControlPersist=120`. The first connection to a VPS pays the TCP
and auth handshake; every later channel — the persistent RPC client, per-shell WebSocket proxies, ad hoc
file streams — reuses that master and costs close to nothing (`sshShared.js:16-25`). `BatchMode=yes`,
key-only auth, and a **Charon-scoped** `known_hosts` file (`~/.ssh/charon_known_hosts`, `sshShared.js:43`)
keep it out of the operator's personal SSH trust store, with `StrictHostKeyChecking=accept-new` so a first
connection is trusted on sight and a later key change hard-fails rather than silently reconnecting.

**Honest comparison against `docs/forum-implementation.md`.** These solve *different* halves of the
reachability problem, and conflating them would be a mistake:

- **Direction is opposite.** Charon's hub dials *out* to each VPS. Atrium's forum design has the leaf dial
  *out* to the forum. Charon's shape requires the control plane to have a route to every managed machine;
  atrium's forum shape requires every managed machine to have a route to one central point. Atrium's shape
  is strictly better for the cases `docs/federation-design-v2.md` names as the reason hub-out was chosen —
  a laptop behind NAT, a container on a bridge network, a kubernetes pod — because none of those expose an
  inbound port at all, whereas Charon's VPS targets are, definitionally, machines that already run sshd and
  already accept inbound connections. **Charon's transport choice is well suited to Charon's fleet (rented
  VPSes an operator provisions with SSH access) and would not work unchanged for the environments atrium's
  forum design explicitly targets.** This is not a case where "SSH is simply better"; it is a case where the
  two projects are solving different reachability shapes and it would be a mistake to import one's answer
  wholesale into the other.
- **What Charon gets for free that atrium's forum design has to build.** SSH's `ControlMaster` multiplexing
  is exactly the connection-pool problem `docs/forum-implementation.md` spends a whole section on
  ("The connection pool, concretely," `watchedConn`/`dialledListener`) and solves by hand with first-byte
  detection and a spare-connection budget. If atrium ever needs a leaf-dials-out-over-SSH mode (say, for a
  VPS-shaped leaf that already has sshd and where the operator is fine with the hub dialing in), `ssh -o
  ControlMaster=auto` is a smaller, better-tested piece of machinery than the hand-rolled pool, and reusing
  it there would be reasonable. It answers nothing about the container/pod case, where there may be no sshd
  to dial at all.
- **Auth model is genuinely simpler and it is the right comparison point for atrium's forum stage 7.**
  Charon's whole trust boundary is "possession of Charon's SSH key" (ADR, "Security properties"). No token,
  no identity file the daemon reads, no controller. `docs/federation-design-v2.md` section 4 spends
  considerable effort justifying why Agora is the *only* acceptable way to get a verified per-connection
  identity without minting a homegrown credential, specifically because a forum's connections arrive over
  plain TCP with no inherent identity. SSH sidesteps this because the transport itself carries
  authentication. This is worth naming as a real, if narrow, argument for an SSH-based leaf-to-forum
  transport as an *alternative* stage 7 to the Agora path: it would satisfy the same "never mint a
  credential" rule atrium already holds, using host-managed `authorized_keys` as the authorization boundary
  instead of an overlay's router-verified identity. It would need its own writeup rather than papering over
  `federation-design-v2.md` section 4, since it trades OpenZiti's "atrium never handles an identity" posture
  for "atrium's forum now needs an SSH server and authorized_keys management," which is a different set of
  operational costs, not a strictly smaller one.

**Verdict: adapt one idea (SSH as an alternative dial-in transport for VPS-shaped leaves, worth a line in
`docs/federation-design-v2.md` section 4's transport options), refuse the rest.** The core shape (hub dials
out) is wrong for atrium's stated targets and correctly rejected already by `docs/federation-design-v2.md`
section 3's table.

## 4. Persistent terminals

**Mechanism, precisely: process detachment, not tmux, not a daemon-owned PTY.** Before Charon's own
version 0.10.0 the agent process owned the PTY master fd directly via `pty.fork()`, and a shell died with
the agent (`agent/charon_agent/holder.py:1-11`, the module's own "why this exists" note). The fix: a shell's
PTY is owned by a separate, tiny, detached child process — the **holder** — spawned with
`start_new_session=True` so it survives the parent daemon's exit, and the systemd unit that runs the daemon
uses `KillMode=process` so `systemctl restart` does not sweep the holder along with it (`holder.py:14-19`).
The holder does the actual `pty.fork()` and `execvpe('bash', ['bash', '-l'], env)` (`holder.py:135-153`) and
listens on a per-shell Unix socket, `~/.charon/shells/<id>.sock`, chmod 0600 (`holder.py:117`, `:459`). The
agent talks to it as a client over that socket with a five-message line-JSON protocol
(`hello`/`output`/`spool_end`/`exit` one way, `input`/`resize`/`kill` the other, `holder.py:30-42`). On agent
startup it scans `~/.charon/shells/*.sock` and reattaches to whatever holders it finds
(`agent/charon_agent/shell.py:17`), and "exactly one agent connection at a time; a new connection replaces
the old one" (`holder.py:59-60`) is what makes a fast agent-crash-and-restart race safe.

**Offline spool.** While no agent is attached, the holder appends PTY output to a capped file,
`<id>.spool`, 8 MiB with newest-output-wins truncation (`SPOOL_MAX_BYTES`, `holder.py:83`, `:197-212`). On
reattach the holder pauses its own PTY reader, replays the whole spool to the freshly connected agent so
ordering (spool strictly before live) is guaranteed, deletes the spool file, then resumes
(`_replay_spool`, `holder.py:222-241`). This closes the exact gap atrium's own supervision design names as
unsolved.

**Direct answer to the question `docs/supervision-design.md` asks.** That document's open question 2 is
"what happens to a supervised runner when the daemon dies unexpectedly" and names window mode's survival as
"a real argument for keeping window mode." Charon's holder is precisely the missing third option: neither
"the daemon owns the pty and dies with it" (atrium's pty mode today) nor "the runner detaches from atrium
entirely and atrium loses control" (atrium's window mode), but "the pty is owned by a small process the
daemon spawns detached, and the daemon reattaches when it comes back." And `docs/architecture-v2.md`'s "Open
risks" already names the specific Windows blocker: "ConPTY offers no reattach... The answer is resume ids,"
i.e. atrium's stated plan is to solve this at the *provider* level (a runner's own resume/session id), not at
the pty level, because on Windows there may be no OS primitive for a detached process to keep holding a
ConPTY across the owning process's exit the way `start_new_session` plus a chmod-0600 Unix domain socket
does on Linux.

**Verdict: adapt the pattern for Linux/macOS leaves, and name the platform gap rather than pretend it
transfers.** Charon's holder pattern is a real, working answer to reattach, but it is a POSIX answer:
`pty.fork()`, Unix domain sockets, `start_new_session=True`, SIGHUP-based teardown. Atrium's supervision
runs on Windows first (`docs/architecture-v2.md`'s "ConPTY was validated" section), and go-pty's Windows
backend is ConPTY, which the same doc already states has no reattach primitive at all — there is no Windows
equivalent of "the child keeps running and I can hand its pty to a new parent process" the way POSIX process
groups and Unix sockets provide. So: worth building for a Linux-hosted atrium daemon (a small detached helper
process holding the PTY, reattached over a Unix socket, chmod 0600, matching `docs/architecture-v2.md`'s own
open risk almost exactly), and worth writing down explicitly in `docs/supervision-design.md` that this
pattern does not close the Windows gap, which stays dependent on runner-level resume ids as already planned.
This is not a case of atrium already doing better; it is a real, adoptable answer for half of atrium's
supported platforms and a non-answer for the other half.

## 5. File transfer and clipboard paste

**File upload / drag-drop.** `app/ClaudeSessionView.tsx` wires window-level `dragenter`/`dragover`/`drop`
listeners scoped to whenever a chat session is open (`:1271-1332`), distinguishing an OS file drop from an
in-app "drag a path from the explorer into the chat" drag (`isPathDrag`, `:1278-1283`, `:1308-1317` — the
explorer-path case skips upload entirely and just splices the existing remote path into the textarea at the
caret). An actual file drop or picker selection calls `onUploadFiles` (`handleFiles`, `:1259-1264`), which
streams the bytes to the hub and from there to the VPS's session workspace directory, appending the
resulting remote path into the message text at the caret as each file's upload lands (`:1261-1263`,
comment: "the path is appended as each upload lands, so the user sees progress"). A blocking overlay covers
the input while any upload is outstanding, explicitly to stop the caret moving under the user mid-upload
(`:1344-1349`, `:1537-1550`).

**Clipboard/image paste, precisely.** `onPaste` on the message textarea reads `e.clipboardData?.files`
(`:1336-1342`). If the clipboard event carries files (this is how a screenshot copied with a system
screenshot tool actually arrives in a browser paste event — no `files: true` in the input needed, the
browser synthesizes a `File` from image clipboard data automatically) it calls `preventDefault()` and routes
into the exact same `handleFiles` pipeline as a drag-drop. There is no separate "paste an image" code path
and no base64-inline-into-the-prompt shortcut: **a pasted screenshot becomes a file on the VPS, uploaded like
any other, and the model sees a file path in its prompt text**, which it then reads with its own file-reading
tool exactly as if a human had said "look at `/path/to/screenshot.png`." This is a meaningfully different
answer than "attach an image to the API call," and it is why the mechanism generalizes to files of any kind
rather than needing special-casing for images.

**Verdict: adopt the pattern, and note atrium already has the harder half done.** Atrium's backlog already
lists file transfer and clipboard paste as undesigned. Charon's answer is genuinely simple because it reuses
one pipeline (upload → remote path → splice into text) for drag, paste, and picker, and because it never
tries to be smarter than the model's own file-reading tool. The piece atrium is missing is the upload
transport and a workspace directory to land files in; atrium already has the harder counterpart working,
`/v1/browse` (listing the *daemon's* filesystem so a browser picker works over a remote board,
`internal/api/browse.go`, named in `CLAUDE.md`), which is the read side of the exact problem Charon's upload
solves on the write side. Building "upload to a spot under the task's cwd, then splice the resulting path
into the prompt box" on top of the existing attach WebSocket or a small new endpoint is a bounded addition
that fits atrium's shape without requiring the supervised runner or the permission chain to change at all.

## 6. The embedded editor

**Correction to the brief and to `docs/backlog.md`'s guess.** The editor is **CodeMirror 6**
(`@codemirror/state`, `@codemirror/view`, `@codemirror/language`, `@codemirror/search`,
`@codemirror/theme-one-dark`, imported at `app/CodeEditor.tsx:1-16`), not Monaco. `docs/backlog.md`'s "An
editor in the board" section guesses Monaco as "the obvious candidate... which vendors as static assets."
That guess does not match what Charon shipped, and it is worth correcting before anyone plans against it:
CodeMirror 6 is smaller, its language modes are genuinely lazy per-file (`@codemirror/language-data`'s
entries are dynamic imports, so opening a `.py` file pulls only the Python mode, `CodeEditor.tsx:118-121`),
and it does not carry Monaco's dependency on the Monaco web worker infrastructure. If atrium builds this,
CodeMirror 6 is worth evaluating first, precisely because it is lighter and still vendors cleanly as a
static asset the same way xterm.js already does.

**How files are read and written.** Reads go through `fs_read`/`fs_stat` (`agent/charon_agent/fsnav.py:226`,
`:248`), each returning a `version` token derived from the file's mtime/size/inode
(`_version_from_stat`, referenced at `fsnav.py:341`, `:568`) or a sha256. Writes go through `fs_write`
(`fsnav.py:503-572`), which is atomic (temp file in the same directory, `fsync`, then `os.replace`,
`:544-557`) and preserves the existing file mode rather than resetting permissions on save (`:550-556`).

**What happens on a same-file race — this is the concrete, citable answer.** There is no lock. `fs_write`
takes an optional `expected_sha256` and treats it as "a precondition, not a hint"
(`fsnav.py:508-513`, the function's own docstring): if the file's current sha256 does not match what the
editor last saw, the write is refused *without writing*, and the response carries `reason: "stale"` plus the
file's actual current hash (`fsnav.py:532-540`). The caller — the browser — decides what to do next: reload
and lose the local edit, or force an overwrite by resending with `expected_sha256=None`, which the docstring
explicitly flags as "a deliberate force-overwrite." The identical stale-check pattern also guards deletion,
specifically so that reverting a snapshot-diffed edit cannot delete work a coding agent produced after the
snapshot was taken (`fsnav.py:697-725`). So: **optimistic concurrency, not a lock**, and the daemon never
tries to arbitrate "who was editing this file" between a human in the browser and an agent mid-turn; it just
refuses to silently clobber.

**Verdict: adopt the concurrency model, evaluate CodeMirror 6 over the Monaco assumption, defer the feature
itself.** `docs/backlog.md` already asks the right open questions ("what writes back, whether a save races
the agent editing the same file, and whether a read-only view answers most of the want"). Charon answers the
race question cleanly and it is a small, well-understood pattern (the same shape as HTTP's `If-Match`) that
would sit naturally on top of atrium's existing `/v1/browse` read path and a new write endpoint, both scoped
to the daemon's own filesystem the way `browse.go` already is. The editor UI itself remains a genuinely large
addition and nothing here changes that calculus.

## 7. Anything else worth stealing

- **The durable per-session event log, and why it does NOT transfer directly.** Charon keeps a second,
  separate durable store beyond its hub SQLite: a JSON-Lines log per session on the VPS itself
  (`agent/charon_agent/event_log.py`), with a monotonic `seq`, rotation at 10 MiB times 3 files, and a
  `subscribe({session_id, after_seq})` resume contract (docstring, `event_log.py:1-44`). The reason it exists
  is specific to Charon's split-process shape: the hub can be down or disconnected while the *agent* (a
  separate process on a separate machine) keeps producing events, and those events would otherwise vanish
  once the in-memory ring buffer wraps. **Atrium does not have this problem in the same shape.** Atrium's
  daemon and its store are the same process; there is no remote agent producing events while the durable
  writer is unreachable. Atrium's `event` table already is the single durable log, written by the same
  process that would otherwise need a second one. Adopting Charon's two-store split into atrium would be
  solving a problem atrium's architecture does not have, and would go against `CLAUDE.md`'s "one fact, one
  source" principle applied elsewhere in this codebase. Correctly refused, and worth stating explicitly so a
  future reader who skims the ADR does not propose it by analogy.
- **`send_input`'s idempotency key is a smaller, worth-stealing idea, already partly present in atrium.**
  The ADR notes "`send_input` accepts a client message id and remembers a bounded set of accepted ids...
  therefore retry a response-lost timeout without sending the human prompt twice." Atrium already has the
  identical concept for permissions (`dedup_key` on the `permission` table, documented at length in
  `docs/architecture-v2.md`'s schema section and its failure-posture section). Charon independently arrived
  at the same idea for a different write path (sending a prompt, not deciding a permission). Worth a note in
  `docs/backlog.md` that `POST /v1/tasks/{id}/prompt` does not currently carry an equivalent dedup key and
  probably should, for the same "the human retried after a lost response" reason `dedup_key` exists at all.
- **The `charon-agent.pyz` zipapp-plus-venv split is a clean pattern for atrium's own build story if atrium
  ever ships provider SDKs directly, but atrium does not need it today.** Atrium never embeds provider SDKs
  (`CLAUDE.md`: "Atrium does not replace the runner... it also holds the subscription credentials"), so the
  problem this split solves (reproducible interpreter-plus-stdlib zipapp, external venv for SDK churn) does
  not arise. Named for completeness, not recommended.
- **Standing rules versus Charon's per-request-only approvals is already covered and atrium is ahead.**
  `docs/backlog.md`'s existing Charon section already states this correctly ("Charon's approvals are per
  request. Atrium answers once and remembers... That is the whole reason atrium exists") and nothing found
  while reading the code changes that conclusion. Charon's permission UI has no equivalent of atrium's
  `perm_rule` table or prefix/glob matching; every request, even an identical repeated one, goes back to a
  human or to Charon's much coarser `permission_mode` (`normal`/`plan`/`acceptEdits`/`auto`, a total bypass
  switch, `session.py:465`) with no per-command memory. Confirmed, not merely repeated.

## What was not verified

- Charon's Git integration (`agent/charon_agent/git.py`) and LSP support (`agent/charon_agent/lsp.py`) were
  located but not read in depth; they were out of scope for the seven questions above and are not claimed
  here one way or the other.
- The Codex-side persistent-terminal and file-transfer paths were assumed identical to the Claude-side ones
  described above (both share `fsnav.py`, `holder.py`, and the same upload UI) but were not independently
  traced through `codex_session.py`.
- No code was run. Every claim above is a static read of the cloned tree at the commit fetched during this
  spike (shallow clone, `git clone --depth 1`), so no commit hash is pinned here; re-cloning later may see a
  different `HEAD`.
- Docker/systemd install and update flow (ADR "Installation and updates") was read only from the ADR's prose
  summary, not from `scripts/` or `docker/` directly.
- Whether Telegram's inline-keyboard "Always" button (`⏵ Always`, `telegram.ts:166`) writes any form of
  standing rule was not traced into `sessionOps.ts`/`respondPermission`; from the permission-mode evidence in
  `session.py` (no rule table found anywhere in `agent/`) it is assumed to mean "allow, and flip this
  session to `acceptEdits`/`auto`-like behavior for the rest of the turn," not a persisted rule, but this was
  not confirmed by reading `respondPermission`'s implementation.

## Summary

**Single mechanism most worth stealing:** the peer-to-peer MCP handle/discovery/message shape (question 1),
adapted to atrium's HTTP-based daemon and its existing task/status/message primitives rather than copied
verbatim — this is the one feature `CLAUDE.md` says atrium has no answer for at all, and Charon's version is
concrete enough to build from directly.

**Single thing atrium already does better:** standing rules with a durable, replayable audit log
(`docs/architecture-v2.md`'s `perm_rule` table and `permission.dedup_key`) against Charon's per-request-only
approvals with a coarse four-mode bypass switch and no persisted rule matching at all. `docs/backlog.md`
already said this and the code confirms it exactly as stated.

**What could not be verified:** listed in full above; the two most consequential gaps are (1) whether
Charon's "Always" Telegram button persists anything beyond the current session, and (2) that nothing here was
run, only read, so behavior under real network/process failure (the daemon's actual halt and reconnect
behavior under load) is asserted by the ADR's prose and not independently exercised.
