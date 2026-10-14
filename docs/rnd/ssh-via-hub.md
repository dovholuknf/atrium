# ssh to a room's machine through the hub

Status: design, closed 2026-10-14. Nothing here is built. Every point is decided below with its reason.

## The ask

clint wants to ssh to a room's machine THROUGH the hub, over the links that room already holds (direct mTLS, OpenZiti,
zrok, as `docs/rnd/room-transport-order.md` and `docs/rnd/room-transport-all-at-once.md` describe). m1mini and sg3
are the case in point: the hub machine cannot reach either by TCP, and both reach the hub.

```
ssh -o ProxyCommand="atrium ssh-proxy sg3" claude@sg3
```

His constraints:

- **The room decides.** A room setting, remote access, off by default, that only the room's own machine can turn on.
  Neither the hub nor the board can.
- **When it is on, the hub forwards a TCP stream to that room's own sshd** (`127.0.0.1:22`).
- **Atrium carries bytes only.** It never holds an ssh key, password or agent socket. ssh runs end to end inside the
  stream and the login is sshd's.

## How this relates to ssh across rooms

`docs/rnd/ssh-across-rooms.md` (2026-10-12) designed a `tunnel` connection kind for a worker on one room to reach
TCP targets that ANOTHER room can reach (a VM, WSL), declared in `ssh_targets.json`. Its phase 1 is built
(`internal/sshtarget`, `atrium ssh add|rm|ls`). Phases 2 to 5 are not.

This design is the same wire with a different authority and a different caller:

| | ssh across rooms | ssh via hub (this) |
| --- | --- | --- |
| what is reached | any `host:port` the target room lists | the room machine's own sshd, loopback only |
| who decides | `ssh_targets.json`, per target `from` list | `remote_access.json`, one switch and one `from` list |
| who may call | another room, by certificate | the hub machine's operator, or another room, by certificate |

Decision 1 below makes them one mechanism, so the across-rooms phases 2 to 4 shrink to "add a target name to the
stream request" once this lands. This design is built first because it is the smaller grant (one port, on the
machine itself) and the one clint asked for now.

## Decisions

### The stream

1. **One stream rides one link connection, with no multiplexing.** The hub takes a data connection from the target
   room's pool (`Hub.Dial`), as every board request does, and spends it on the stream. A room keeps every transport it
   has connected at once (`room-transport-all-at-once.md`), so its pool can hold connections on more than one, and the
   one the hub takes decides the transport. The stream inherits it for free: mTLS over TCP on direct, mutual TLS
   inside a ziti or zrok connection on an overlay (`link.Proven`). Nothing in this design names or picks a transport. A multiplexed channel on the control connection was
   rejected: it would need atrium's own flow control windows, and one slow `scp` would stall the room's beats.

2. **Framing is one HTTP upgrade per hop and then raw bytes.** There are three hops:

   ```
   ssh client --stdio--> atrium ssh-proxy --(A)--> HUB --(B)--> room process --(C)--> sshd 127.0.0.1:22
   ```

   - **(A) caller to hub.** Either a loopback HTTP request on the hub's board listener, or a connection of a new kind
     `stream` that the caller's own room dials to the hub (decision 6).
   - **(B) hub to room.** On the borrowed data connection the hub writes:

     ```
     GET /_link/ssh HTTP/1.1
     Host: atrium-link
     Connection: Upgrade
     Upgrade: atrium-stream
     X-Atrium-Caller: hub            (or the calling room's certificate name)
     X-Atrium-Stream: 01JA...        (the stream id the hub minted, for both audits)
     ```

     The room answers `101 Switching Protocols` or a status with `{"error": "<sentence>"}`.
   - **(C) room to sshd.** A plain TCP dial to `127.0.0.1:<port>` with a ten second bound.

   After each `101` the connection carries ssh's own bytes and nothing else. ssh's first bytes are sshd's banner,
   which is how the caller sees the far end is real. Bytes a hop's `bufio.Reader` already holds are written onward
   first, as `buffered` does for the git kind.

3. **Backpressure is the transport's, never a buffer of atrium's.** Each hop is spliced with two `io.Copy` loops on a
   fixed 32 KiB buffer. A slow reader blocks the copy that writes to it, which stops that copy reading, which fills
   the window of the connection behind it (TCP's on direct, the edge circuit's on ziti, the share's on zrok), until
   the ssh sender blocks. Memory per stream is four buffers on the hub and two on the room, whatever the volume. One
   stream per connection means a stalled stream stalls only itself.

4. **Teardown follows half close, then a bound.** When a copy reads EOF it calls `CloseWrite` on the other side
   where the connection has one (`*net.TCPConn`, `*tls.Conn` which sends close_notify, the ziti edge connection),
   so ssh's own shutdown passes through intact. When both copies have ended, both connections close. If one
   direction has ended and the other is still open 30 seconds later, both close, so a peer that never finishes its
   half cannot hold a data connection forever. Any read or write error closes both at once. A stream never goes back
   to the pool.

5. **What ends a stream, and what does not.**
   - **A stream rides one transport and ends with it.** TCP cannot migrate between transports, so if the transport
     carrying a stream drops, that stream ends and ssh says `Connection closed`, even though the room is still
     attached on its other transports. Nothing retries or moves it. Whoever ran ssh runs it again, and the new stream
     takes a data connection on whichever transport is up. A hub restart or a room restart (with or without
     `pty_host`) ends every stream on that room.
   - **There is no idle timeout.** An ssh session may sit quiet for an hour. `ServerAliveInterval` in the caller's
     ssh config finds a dead path, and the link's own beats find a dead link.
   - **Nothing waits on a stream.** Every transport is already connected, so there is no climb to a better one and
     nothing for an open stream to hold back.
   - Turning remote access off ends every open stream on that room within ten seconds (decision 11).
   - Deadlines set on the data connection while it was pooled are cleared before the splice.

### Who may open one

6. **There are two doors for the caller, and the board is not one of them.**
   - **The hub machine's operator.** `atrium ssh-proxy sg3` on the hub machine sends `POST /_hub/ssh/sg3` with
     `Connection: Upgrade` and `Upgrade: atrium-stream` to the hub's loopback board listener. The hub answers only
     when `edge.LocalOperator` holds (loopback peer, loopback Host, no forwarding header), the same rule as every
     hub-machine-only setting (notify command, extra hosts, launch caps). The caller is recorded as `hub`.

   Loopback on the hub machine is enough to call as `hub`, and that is decided. The point is a bridge, so that sshd
   never opens through a firewall, and sshd owns all the auth: a stream still needs a key sshd accepts. Atrium adds no
   second auth layer to the hub-loopback door. A token file readable only by the hub's account was considered and
   rejected, as one more thing to set up that guards nothing sshd does not already guard.
   - **Another room's machine.** `atrium ssh-proxy sg3` on m1mini sends the same upgrade to its own room's agent
     listener (`:7777`, `POST /stream`, loopback, the door every agent command already uses). The room dials the hub
     with its own dialer, says `hello{Kind: "stream"}`, then one JSON line `{"room": "sg3"}`, and the hub answers one
     line `{"ok": true}` or `{"ok": false, "error": ...}`. The caller is the room's name FROM ITS CERTIFICATE, never
     from the hello. The room process holds the transport because it already has the ziti identity loaded, and a CLI
     loading its own per ssh connection would cost seconds per `scp`.

   When both doors are open (the hub machine also runs a room, as sg4 runs claude-sg4), the hub door wins, since it
   is one hop shorter. `--via room` or `--via hub` forces one.

7. **The board can never open a stream.** The route is an upgrade with `Upgrade: atrium-stream`, and a browser cannot
   set `Connection` or `Upgrade` on `fetch` or `XMLHttpRequest` at all, and a WebSocket says `websocket`. On top of
   that `/_hub/ssh/` refuses any request carrying `Origin` or `Sec-Fetch-Site`, so a page open on the hub machine
   cannot reach it either. It is served only on the hub's loopback board listener. On a share or overlay listener
   (`--board-transport zrok|ziti`) it answers 404 whatever the share's login said: `share_auth` is a browser login
   for reading the board, and reach into a machine is a larger grant than that. The proxy refuses every path under
   `/_link/` with 404 and strips `X-Atrium-Caller` and `X-Atrium-Stream` from everything it forwards, so no board
   request can reach the room's route or forge its headers. The room serves `/_link/ssh` on its link server only,
   wrapped around `Room.Handler` in `room.go`, never on its own loopback board (`:7781`).

8. **The control MCP has no ssh tool.** An agent that needs ssh runs `ssh` through Bash, under the permission chain
   like any other command, with whatever keys its machine already has. A tool would be a second door with a weaker
   audit (`ctlclass.go` is not a boundary).

9. **Authorisation is the room's `from` list, checked twice, decided once.** `from` names who may open the room's
   sshd: `hub`, room names, or `*` for every proven room. Default when remote access is turned on without `--from`:
   `*`, the hub plus every proven room. Authentication is sshd's and atrium only bridges, so the list narrows who can
   reach the port and is not what keeps anyone out. The hub checks the room's last reported switch and list before borrowing a connection, so a refusal is
   quick and costs the room nothing. The ROOM checks again against its file at the moment of `/_link/ssh`, and the
   room's answer is the one that counts: a hub with a stale or wrong view can only refuse early, never grant. sshd's
   `authorized_keys` is the third, independent yes.

10. **Every room involved must have a proven name.** The target, and the calling room on the room door, must be
    attached with a hub-signed certificate. A room on the old certificate-less overlay path is refused with
    `RejoinSentence`, as the `roomspec` kind refuses it, because a `from` list is only as good as the name it is
    checked against.

### The room's switch

11. **It lives in `remote_access.json` in the room's key directory**, beside `room.json`, `transports.json` and
    `ssh_targets.json` (default `~/.atrium/room/`, `%USERPROFILE%\.atrium\room\` on Windows), mode 0600:

    ```json
    {"version": 1, "on": true, "port": 22, "from": ["hub", "m1mini"],
     "set_at": "2026-10-14T18:02:11Z", "set_by": "claude@sg3"}
    ```

    - Not the room's store `setting` table: the board writes that through `/v1/settings`, and the hub proxies it.
    - Not `room.json`: `atrium room join` rewrites it, and a re-join must not silently change access.
    - Not `room.yaml` or the room spec: the hub owns those.
    - The host is always `127.0.0.1`. Only the port can be changed (`--port 2222` for an sshd on another port). Reaching
      any other address is `ssh_targets.json`'s job, with its own per target list.

    The room reads the file at every stream open and checks its modification time every ten seconds. A change is
    told to the hub at once, and a switch to off, or a `from` list that drops a caller, ends that caller's open
    streams.

12. **Only the CLI on the room machine writes it.**

    ```
    atrium room remote-access                       prints the state, the port, from, and open streams
    atrium room remote-access on [--from hub,m1mini|*] [--port 22]
    atrium room remote-access off
    atrium room remote-access log [--n 50]          the room's own record of streams, newest first
    ```

    Every form takes `--dir` like every other `atrium room` command. The CLI writes the file directly through a temp
    file and a rename, and nothing on the link, the agent listener, the room's api or its loopback board can write
    it. There is no board toggle, not even on the room's own loopback board, because that handler is also served to
    the hub over the link and one mistake in telling the two apart would hand the switch to the hub.

    `on` refuses to run inside a session atrium supervises (`ATRIUM_TASK_ID` or `ATRIUM_AGENT_NAME` set) with
    `remote access is turned on by a person at this machine, not by an agent`. That is a speed bump and not a
    boundary, since an agent could edit the file. It is the same exposure as an agent editing `sshd_config`, which the
    permission chain already stands in front of. `off` is allowed from anywhere on the machine.

13. **The environment can only turn it off.** `ATRIUM_REMOTE_ACCESS=off` in the room's environment forces it off
    whatever the file says, and the room reports `off (forced by ATRIUM_REMOTE_ACCESS)`. No value turns it on. An
    environment is set by whatever started the process (a service, a live script, a deploy) and is invisible
    afterwards, which is the wrong place for a grant and a fine place for a kill switch.

14. **The hub shows it and cannot change it.** The room says its state at attach and on every change as a control
    `note` line, `{"access": {"on": true, "from": ["hub", "m1mini"], "port": 22, "open": 1}}`. Observed, never
    trusted for granting (decision 9). The hub keeps it on the attached room, in memory only, and adds
    `"remote_access"` to `/_hub/rooms`, `/_hub/inventory` and the `rooms` event. An older room that never says reads as
    unknown, and the hub refuses streams to it with `room sg3 is older than remote access. update it`.

    The Rooms tab draws an `ssh` badge beside the transport badge when it is on, with the tip `remote access on, from
    hub, m1mini`, and the open stream count when it is not zero. The room's facts table gets a row:

    - on: `remote access: on, port 22, from hub and m1mini. 1 stream open`
    - off: `remote access: off. turned on at the room's machine with atrium room remote-access on`

    No button, no checkbox. The room's own loopback board shows the same row.

### Audit

15. **Both ends record every stream, and neither records content.** The hub mints a stream id and passes it to the
    room in `X-Atrium-Stream`, so the two records join.

    The hub writes through `Proxy.RecordAudit`, under the target room:

    | kind | detail |
    | --- | --- |
    | `ssh-open` | `<stream id> hub -> sg3 over ziti` or `m1mini -> sg3`, plus ` claims claude@sg4` if sent |
    | `ssh-close` | `<stream id> 4m12s, 1.2 MiB to sg3, 38 MiB from sg3, ended by the caller` |
    | `ssh-refused` | `<stream id> m1mini -> sg3: sg3 does not offer ssh to m1mini` |

    The room appends one JSON line per open, close and refusal to `remote_access.log` in its key directory (rotated
    at 1 MiB, one old file kept) and logs the same sentence to its log. Fields: `id`, `caller`, `via` (the transport),
    `start`, `end`, `bytes_in`, `bytes_out`, `ended_by`, `error`. Not the room's store, so a store halt never blocks
    reading what was reached, and the record sits beside the switch it is a record of.

    - **The caller is a fact or a claim, and says which.** `hub` and a room name are the hub's own observation.
      `atrium ssh-proxy` sends `X-Atrium-Claim: <os user>@<host>` and the audit prints it as `claims ...`, the
      `ctlaudit.go` rule that a claim is recorded as a claim. The ssh user is inside the encrypted stream and atrium
      never sees it.
    - **Bytes are counted, not read.** The splice counts what each `io.Copy` returns. Nothing parses, samples or
      logs the bytes themselves, and the counts are only written at close.
    - **Refusals at the CLI or the caller's room** (no room running, not attached) are said on stderr and not audited,
      since nothing was asked of the hub.

16. **Sixteen open streams per target room at most**, shared with the across-rooms tunnels when they land. A stream
    holds a data connection for its whole life and the pool keeps 64 idle at most. The seventeenth is refused with
    `sg3 already has 16 open ssh streams`.

### The caller's side

17. **`atrium ssh-proxy <room>` writes nothing to stdout but stream bytes.** ssh reads stdout as the protocol. Every
    refusal is one sentence on stderr and exit 255, which ssh prints above its own `Connection closed`:

    | What | Sentence |
    | --- | --- |
    | neither door answers | `no atrium hub or room is running on this machine` |
    | the caller's room is not attached | `this room is not attached to a hub right now` |
    | the hub predates streams | `the hub does not carry ssh streams yet. update the hub` |
    | the room is offline | `room sg3 is offline (last seen <time>)` |
    | remote access is off | `sg3 has remote access off. turn it on at that machine: atrium room remote-access on` |
    | caller not on `from` | `sg3 does not offer ssh to m1mini` |
    | unproven room | `ssh streams need rooms that proved their names. <RejoinSentence>` |
    | sshd does not answer | `sg3 could not reach its sshd on 127.0.0.1:22: <error>` |

    The room name in `ssh-proxy sg3` is taken literally, so `%h` works when the ssh `Host` alias is the room name.
    `atrium ssh-proxy --print-config <room>` prints a block to paste, with the absolute binary path quoted, because
    Windows OpenSSH runs `ProxyCommand` without a shell:

    ```
    Host sg3
      ProxyCommand "C:\Users\claude\.atrium\bin\atrium.exe" ssh-proxy %h
      HostKeyAlias sg3.atrium
      ServerAliveInterval 30
      CheckHostIP no
    ```

    On macOS and Linux ssh runs `ProxyCommand` through the user's shell, so the path is plain and unquoted. The
    command picks the form from the OS it runs on, so the block is right for the machine that will use it:

    ```
    Host sg3
      ProxyCommand /Users/claude/.atrium/bin/atrium ssh-proxy %h
      HostKeyAlias sg3.atrium
      ServerAliveInterval 30
      CheckHostIP no
    ```

    No port is opened on the calling machine: `ProxyCommand` speaks stdio. ssh has no SNI, so nothing on the wire
    tells rooms apart, and one `Host` block per room is how they are told apart.

    Atrium never edits `~/.ssh/config`. `HostKeyAlias` keeps the room's host key apart from a LAN entry for the same
    machine.

## Settings at a glance

| where | what | how it is set |
| --- | --- | --- |
| room `remote_access.json` | on or off, port, `from` | `atrium room remote-access on --from hub,m1mini`, there only |
| room environment | force off | `ATRIUM_REMOTE_ACCESS=off` |
| caller's ssh config | how ssh reaches it | `atrium ssh-proxy --print-config sg3`, pasted by the operator |
| hub | nothing | the hub stores no part of it and has no setting for it |

## Build outline

For whoever plans the build. Each step lands alone with its tests.

1. `internal/remoteaccess`: the file, `Load`, `Save`, `Allows(caller)`, the env override. `atrium room remote-access`
   with its four forms and the supervised-session refusal.
2. `internal/link`: a shared `splice` with half close, the 30 second linger and byte counts. The room's `/_link/ssh`
   route around `Room.Handler`. The proxy's `/_link/` refusal and header strip.
3. `internal/link`: the hub side. The `stream` kind in `hearHello` and the kind switch, the loopback
   `/_hub/ssh/<room>` route behind `edge.LocalOperator` with the `Origin` refusal and 404 off loopback, the cap, the
   `access` note and `remote_access` in `/_hub/rooms`, the audit.4. `internal/daemon` and `internal/cli`: `POST /stream` on the agent listener, `atrium ssh-proxy` with both doors and
   `--print-config`.
5. The board: the `ssh` badge and the facts row in `rooms.js` and `rooms-dash.js`.
6. `docs/changes/ssh-via-hub.md` with a manual scenario: `ssh`, `scp` and `ssh -L` from sg3 as target, run as the
   caller from each OS in turn: Windows (sg4), macOS (m1mini) and Linux (sg4-wsl, test only). Paste the
   `--print-config sg3` block that machine prints, and check it is the Windows quoted `.exe` form on sg4 and the
   plain path on m1mini and sg4-wsl. Then `remote-access off` mid-session from each.

Tests are Go on one machine with a fake sshd (a listener that writes `SSH-2.0-fake\r\n` and echoes), the real `Hub`
and `Room`s over loopback mTLS, and the overlay pipe in `overlaytls_integration_test.go` with `stream` added to its
kinds. The proxy guard is an `httptest` request for `/_hub/ssh/x` with an `Origin`, and for `/_link/ssh`, both
refused without the room seeing anything.

## Out of scope

- Holding, minting, moving or installing any ssh credential, and editing `authorized_keys` or `~/.ssh/config`.
- Any address but the room machine's own loopback sshd. Other hosts are `ssh_targets.json` and the across-rooms design.
- Turning remote access on from the hub, the board, the room spec, an environment variable or the control MCP.
- An ssh terminal in the browser.
- A caller reaching the hub over a share or overlay board listener, or from a machine with neither a hub nor a room.
- Per card or per agent limits on who may ssh. Every agent on a calling machine can already use that machine's keys.
- Reconnecting a dropped stream, or keeping one across a hub or room restart.
- Throughput tuning or compression. Every byte crosses the hub, so a multi gigabyte copy is better done another way.
- Protocols other than ssh. The stream would carry them and nothing here is built or tested for them.

## Open for clint

Nothing. The default `from` is `*`, decided by clint: auth is sshd's, atrium only bridges.
