# ssh across rooms: a worker on any room reaches another room's VMs and WSL

Status: design, 2026-10-12. Nothing here is built. Written for backlog r-002 (high). Every question is decided below
with its reason, because clint is away and the brief says to decide. The build is five phases, filed as
f-ssh-rooms-1 to f-ssh-rooms-5, each one a Sonnet worker can follow without a judgment call.

## The problem

A VM on claudevm (`ssh claudevm`) and WSL (`ssh localhost` on claude-sg4) are usable only from the room that holds the
ssh config. A worker on m1mini or a linux room cannot drive them, so a mac room cannot run a Windows test on a claudevm
VM without relaying through a Windows room's worker. clint wants rooms to ssh into each other's VMs and WSL, and into
any room added later.

clint's constraints:

- It must work over OpenZiti and over private zrok shares, not only a flat LAN.
- Atrium may hold the NAME of a command that has a credential, never somebody else's credential. Rooms dial the hub,
  never the reverse.

## What exists

- **Every room already holds one authenticated link to the hub**, over direct mTLS, a private zrok share or OpenZiti
  (`internal/link`, `docs/fabric/hub-and-rooms.md`). The room's name comes from the certificate the hub signed, on all
  three transports (`docs/rnd/overlay-room-identity.md`). A connection says hello with a kind (`control`, `data`,
  `relay`, `git`, ...) and the hub switches on it in `hub.go`.
- **The hub already reaches any attached room** by taking a connection from that room's data pool (`Hub.Dial`). A data
  connection is plain HTTP/1.1 after its hello, served by the room's own board handler (`room.go`, `Room.Run`). A
  websocket upgrade already rides it, which is how the board's terminal works.
- **A room already asks the hub to carry something to another room.** The `relay` kind (`relay.go`) dials the hub,
  sends one request and reads one answer, and the hub answers by asking the target room over the link it holds. The
  `git` kind (`git.go`) dials the hub and then speaks HTTP on that connection for as long as git wants it.
- **A worker on a room has a local door into its room process**: the agent listener on `:7777`, a separate mux in
  `internal/daemon/daemon.go` (`agentMux`, line 1138) that hooks and `atrium tell` reach through the machine's location
  file.
- **`atrium_resources`** (`internal/link/resources_mcp.go`) lists `resources.md` from the hub's state dir and the rooms
  the hub knows. It names hosts and commands and never a credential.
- **The 2026-10-01 spike** (`docs/rnd/room-to-room-access-spike.md`) recommended hub-run ops first and raw reach
  second. It found that atrium runs no ssh itself, that the boundary between machines is where keys sit, and that zrok
  and OpenZiti give reach and never a login. This design is the second half, made concrete, and it keeps that finding:
  it moves REACH and never a key.

## Decisions

1. **The ssh bytes ride the existing room link, through the hub, as a new connection kind `tunnel`.** Not an overlay
   service per target. The room link already works over direct, zrok private and OpenZiti, so a tunnel inherits every
   transport any room has, for free, and a room added later needs nothing beyond joining. An overlay per target would
   need zrok or ziti installed and enrolled on both ends of every pair, a share or a service per target, and a dial
   policy per source on a network atrium does not administer (`docs/fabric/overlays.md`: atrium never creates a service
   or writes a policy). See "Approaches compared".
2. **What crosses is a TCP stream to the target's sshd, and ssh runs end to end over it.** The hub and both rooms see
   ciphertext. The login is ssh's own: the source machine's key must be in the target's `authorized_keys`. Atrium moves
   reach and holds no key, no password and no agent socket. This is the AGENTS rule as written: atrium holds the name of
   a target and the name of a command (`atrium ssh proxy`), and every credential stays where ssh keeps it.
3. **A target is declared on the machine that can reach it, by its operator, in a local file.** `atrium ssh add wsl
   127.0.0.1:22` on claude-sg4 writes `ssh_targets.json` beside the room's key (`~/.atrium/room/`). Not on the hub,
   and not in `room.yaml`: the hub sets `room.yaml`, and a target the hub could add would let the hub open a TCP path
   into any machine. The machine that holds the route decides what it offers.
4. **A target is named `<target>.<room>.atrium`**, such as `wsl.claude-sg4.atrium` and `win11.claude-sg4.atrium`. The
   room is in the name because target names are chosen per machine and two rooms may both have a `wsl`. The `.atrium`
   suffix is one wildcard `Host` block on every source machine, so a room added later needs no change anywhere else.
   Target names match `[a-z0-9][a-z0-9-]{0,31}`, which keeps them valid DNS labels for ssh's `%h`.
5. **What a worker types is plain ssh.** `ssh wsl.claude-sg4.atrium uname -a`. One `Host *.atrium` block sets
   `ProxyCommand "<abs path to atrium>" ssh proxy %h`, and ssh does the rest. scp, sftp, `rsync -e ssh`, `ssh -L`,
   `ssh -R` and `ssh -D` all work with no atrium code, because they are features of the ssh session that runs inside the
   stream. This is why the stream is to sshd and not a command atrium runs.
6. **The ssh config is atrium's own file plus one line the operator adds.** `atrium ssh config` writes
   `~/.atrium/ssh_config` and prints the `Include ~/.atrium/ssh_config` line to put at the top of `~/.ssh/config`.
   Atrium never edits `~/.ssh/config`, which is the operator's. `ProxyCommand` carries the absolute path of the atrium
   binary, quoted, because Windows OpenSSH runs it without a shell and a bare name may not resolve. The block also sets
   `ServerAliveInterval 30` so a dead tunnel is noticed, and `CheckHostIP no` since there is no IP. ssh keys the host
   key by `%h`, so each target has its own `known_hosts` line.
7. **Who may reach which target is the target's `from` list, checked on the target room, plus sshd itself.** Each
   target lists the rooms allowed to open it, or `*`. Default when `--from` is not given: nobody, and `atrium ssh add`
   says so. The hub passes the source room's name from its CERTIFICATE, never from anything the source sent, and the
   target room checks it. The list is reach, like a firewall rule the operator writes. The login stays sshd's, so a
   room on the list still needs a key the target accepts. Two independent things must both say yes. Per card limits
   are not added: every agent on a room can already use that machine's ssh keys with plain Bash, so a per card rule
   would look like a boundary and not be one (the spike's section 0 says the same).
8. **Both rooms must have proven names.** A room attached over the old overlay path with no certificate is refused as a
   source and as a target, as the `roomspec` kind refuses it, because the `from` check is only as good as the name.
9. **The source end goes through the source room process, not straight to the hub.** `atrium ssh proxy` opens an
   upgrade on the room's agent listener (`:7777`, `POST /tunnel`), and the room dials the hub with its own `Dialer`.
   The room process is what holds the transport. Over OpenZiti or zrok that is a loaded SDK and identity, and a CLI
   that loaded its own per ssh connection would cost seconds per `scp`. The agent listener is loopback and is the door
   every agent command already uses.
10. **The target end is a route that only the link serves.** The target room serves `/_link/tunnel/<target>` on its
    link server and nowhere else. Its loopback board on `:7781` shares the same handler and must never offer it, so
    the route is added in `room.go` around `Room.Handler`, not to the api mux. The hub's board proxy forwards every
    unknown path to a room, so it refuses any path under `/_link/` with 404 and strips `X-Atrium-From-Room` from every
    proxied request. A browser past the board's login can then never open a tunnel.
11. **The hub only splices.** It checks the source, takes one data connection from the target room's pool with
    `Hub.Dial`, writes one upgrade request on it, reads `101`, and copies bytes both ways until either side closes. It
    keeps nothing and joins no address. The data connection is spent: it never returns to the pool, and `Hub.Dial`
    already asks the room for a replacement when the pool is short.
12. **Sixteen open tunnels per target room at most**, a hub constant `maxTunnelsPerRoom`. A tunnel holds a data
    connection for its whole life and the pool keeps 64 idle at most. A seventeenth is refused with a sentence. Sixteen
    is four parallel `scp` streams from each of four rooms, more than any test run needs.
13. **No idle timeout on a tunnel.** An ssh session may sit quiet for an hour, as a terminal does. ssh's own
    `ServerAliveInterval` finds a dead peer, and a link drop closes every tunnel on it.
14. **Every open is audited on the hub**: `tunnel-open` with `from@room -> target.room` and the card id when the
    caller had one, `tunnel-refused` with the reason. Close is not logged per tunnel. Byte counts are not recorded.
15. **Listing is a fan-out from `atrium_resources`**, not a hello field. The hub asks each online room `GET /v1/ssh`
    (on the shared api mux, so the hub's existing `controlMCP.ask` with `X-Atrium-Room` reaches it) with a 3 second
    bound, and keeps the last answer per room in memory so an offline room's targets are still listed, marked offline.
    A hello field would go stale after `atrium ssh add` until the room restarted.
16. **A target is any TCP address the holding room can open**, checked as `host:port`. ssh is the documented use. The
    claudevm VM is a target on claude-sg4 at the VM's address, or at `claudevm:22` used as a `ProxyJump` when only the
    host reaches the VM. Nothing stops an operator naming RDP or WinRM, and nothing in this design is built for them.

## Approaches compared

**A. A tunnel over the room link (chosen).** Works over every transport a room link already uses, with nothing new to
install or enrol. One wildcard config covers rooms added later. scp and forwards are free. The cost is that every byte
crosses the hub, so throughput is the slower of the two links to the hub, and a hub restart drops every open session.
For test runs and file pushes measured in megabytes that is fine. A multi gigabyte copy is better done another way.

**B. An overlay service per target (rejected).** A zrok private share of each sshd (`zrok share private
--backend-mode tcpTunnel localhost:22`) with `zrok access private` on each source, or a ziti service per target with a
dial policy per source. Bytes would skip the hub. Rejected because it needs zrok or ziti set up on both ends of every
pair, a running `zrok access` with a local port per source and target, and policies on a network atrium does not
administer. It does nothing for rooms on direct mTLS. Each source also holds a zrok environment or ziti identity that
any agent there can use with plain Bash. Operators may still do this by hand, outside atrium.

**C. The holding room runs ssh for the caller (rejected).** A hub op `atrium_ssh {room, host, command}` where claude-sg4
runs `ssh claudevm <command>` with its own config. It satisfies the name rule and needs no key off sg4, which is its
appeal. Rejected because it lends claude-sg4's keys to every agent on every room, which is a larger grant than
reach, and it gives no interactive session, no scp and no port forward. A command string sent into a remote shell is
also the injection surface the spike's section 3.1 forbids.

**D. The hub as an ssh jump host (rejected).** `ProxyJump hub` with sshd on the hub machine. The hub dials no room, and
a room behind NAT or an overlay is not reachable from the hub machine by TCP, so this only works on the flat LAN clint
said is not enough. It also puts every target's reach on the hub machine's own network stack and keys.

## The wire

```
 m1mini (source room)                    HUB                               claude-sg4 (target room)
 +---------------------------+                                          +----------------------------+
 | ssh wsl.claude-sg4.atrium |                                          | ssh_targets.json           |
 |   ProxyCommand:           |                                          |   wsl  127.0.0.1:22        |
 |   atrium ssh proxy %h     |                                          |   from [m1mini, sg3]       |
 |      | stdin/stdout       |                                          |                            |
 |      v                    |   kind "tunnel"     +---------------+    |  link server, /_link/      |
 | agent listener :7777      |   hello + request   | serveTunnel:  |    |  tunnel/wsl only           |
 |   POST /tunnel  (101) ----+-------------------->| cert = m1mini |    |      ^                     |
 | room process, its Dialer  |   mTLS inside       | target online,|    |      | from-room check,    |
 |   (direct, zrok, ziti)    |   direct/zrok/ziti  | proven, < 16  |    |      | dial 127.0.0.1:22   |
 +---------------------------+                     | Hub.Dial(sg4) +----+------+  (101)             |
                                                   | GET /_link/   |data|                            |
                                                   | tunnel/wsl    |conn|  sshd (WSL) <-- TCP        |
                                                   | splice bytes  |    |                            |
                                                   +---------------+    +----------------------------+

 ssh's own encryption runs end to end, from the ssh client on m1mini to sshd in WSL.
```

Both rooms dialled the hub. The hub dialled nobody: the data connection to claude-sg4 is one claude-sg4 opened earlier
for its pool.

### The source side

`atrium ssh proxy <host>` parses `<target>.<room>.atrium` (refusing any other shape with a sentence on stderr and exit
255, which ssh shows), then opens the agent listener from the location file and sends:

```
POST /tunnel HTTP/1.1
Connection: Upgrade
Upgrade: atrium-tunnel
Content-Type: application/json

{"room": "claude-sg4", "target": "wsl", "card": "<ATRIUM_TASK_ID or empty>"}
```

The room answers `101 Switching Protocols` and the socket is the stream, or an error status with a JSON
`{"error": "<sentence>"}`, which the CLI writes to stderr before exit 255. On 101 it copies stdin to the socket and the
socket to stdout until either side closes.

### The link

The room dials the hub, says `hello{Kind: "tunnel", Room, Session}`, reads the welcome, then writes one JSON line:

```json
{"room": "claude-sg4", "target": "wsl", "card": "x7k2"}
```

The hub answers one JSON line, `{"ok": true}` or `{"ok": false, "error": "<sentence>"}`, and on `ok` the connection
is the stream. Both lines use `writeJSON` and `readJSON`, bounded as every frame is.

### The target side

The hub takes a data connection with `Hub.Dial(ctx, "claude-sg4")` and writes on it:

```
GET /_link/tunnel/wsl HTTP/1.1
Host: atrium-link
Connection: Upgrade
Upgrade: atrium-tunnel
X-Atrium-From-Room: m1mini
```

The room's link server looks the target up, checks `from`, dials the address with a 10 second bound, and answers
`101`, or `403`, `404` or `502` with a sentence in the body. On `101` it splices the hijacked connection with the TCP
connection.

### Files

`ssh_targets.json`, written only by `atrium ssh add` and `atrium ssh rm`, mode 0600:

```json
{"version": 1, "targets": [
  {"name": "wsl", "addr": "127.0.0.1:22", "from": ["m1mini", "sg3"], "user": "clint", "note": "Ubuntu 24.04 WSL"},
  {"name": "win11", "addr": "192.168.1.90:22", "from": ["*"], "user": "", "note": "claudevm Windows 11 test VM"}
]}
```

`user` is a hint written into the generated `User` line, not a credential. `~/.atrium/ssh_config`, written by
`atrium ssh config`:

```
# written by atrium ssh config. atrium rewrites this file, edit ~/.ssh/config instead.
Host *.atrium
  ProxyCommand "/usr/local/bin/atrium" ssh proxy %h
  ServerAliveInterval 30
  CheckHostIP no

Host wsl.claude-sg4.atrium
  User clint
```

The per target `User` blocks come from the hub's list at the time of writing. The wildcard block alone is enough to
reach a target added later.

### Offline and refusals

Each refusal is a sentence, and the CLI prints it to stderr so ssh shows it above its own `Connection closed`:

| What | Where it is caught | Sentence |
| --- | --- | --- |
| no room process running | `atrium ssh proxy` | `no atrium room is running here. start one with atrium room` |
| the source room is not attached to a hub | source room | `this room is not attached to a hub right now` |
| the hub predates tunnels | hub's `hearHello` refusal | `the hub does not carry ssh tunnels yet. update the hub` |
| the target room is not attached | hub | `room claude-sg4 is offline (last seen <time>)` |
| either room has no certificate | hub | `ssh tunnels need rooms that proved their names. <RejoinSentence>` |
| 16 tunnels already open to it | hub | `room claude-sg4 already has 16 open ssh tunnels` |
| target room too old | hub, a bare 404 | `room claude-sg4 is older than ssh tunnels. update it` |
| no such target | target room | `claude-sg4 has no ssh target named wsl. atrium_resources lists them` |
| not on the `from` list | target room | `claude-sg4 does not offer wsl to m1mini` |
| the address does not answer | target room | `claude-sg4 could not reach 127.0.0.1:22: <error>` |

An open tunnel ends when the hub restarts, when either room restarts, or when either link drops, and ssh reports the
connection closed. A room restart with `pty_host` keeps agents alive and still ends their tunnels. Nothing is retried.
ssh is rerun by whoever ran it.

## How it is tested without the live hub

Every phase is Go tests on one machine, with no network beyond loopback and no real sshd:

- **The link** is the real `Hub` and two real `Room`s over loopback mTLS, as `internal/link`'s attach tests build them.
  The target is a TCP listener on `127.0.0.1:0` that writes `SSH-2.0-fake\r\n` and then echoes, which is enough to
  prove the bytes cross both ways and that the first bytes arrive from the target, as an sshd's banner would.
- **The overlay case** is the pipe standing in for an overlay in `overlaytls_integration_test.go`. Adding `tunnel` to
  its list of kinds (line 436) proves the kind rides the wrapped dial like every other.
- **The CLI** runs `ssh proxy` against an in-process agent listener, with stdin and stdout as pipes.
- **The proxy guard** is an `httptest` request to the hub's board for `/_link/tunnel/x`, which must be 404 and must
  never reach a room.
- What these cannot prove is a real ssh client. Phase 4 adds a scenario to `docs/test-plan.md` for the operator to run
  once by hand: `ssh`, `scp` and `ssh -L` from m1mini to `wsl.claude-sg4.atrium`, then the same over a zrok private room
  link.

## Build plan

Five phases. Each lands alone with its tests passing, and nothing earlier breaks if a later one never lands. Phase 1
comes first. Phase 2 needs 1. Phase 3 needs 2. Phase 4 needs 3. Phase 5 needs 2 and may run beside 3 and 4.

Every new Go file follows the comment style of the file it is modelled on. No new dependency.

### Phase 1, f-ssh-rooms-1: the target file and `atrium ssh add|rm|ls` (fabric, `internal/sshtarget`, `internal/cli`)

1. New package `internal/sshtarget`, file `sshtarget.go`, with a package comment stating the credential rule in the
   words `internal/resources/resources.go` uses.
   - `const File = "ssh_targets.json"`, `const Version = 1`, `const MaxTargets = 64`.
   - `type Target struct { Name, Addr string; From []string; User, Note string }` with JSON names `name`, `addr`,
     `from`, `user`, `note`. `type Set struct { Version int; Targets []Target }`, JSON `version`, `targets`.
   - `var nameRE = regexp.MustCompile("^[a-z0-9][a-z0-9-]{0,31}$")`.
   - `func Validate(t Target) error`: name matches `nameRE`. `net.SplitHostPort(t.Addr)` succeeds with a non-empty host
     and a port `strconv.Atoi` reads as 1 to 65535. Every `From` entry is `*` or matches
     `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`. `User` matches `^[A-Za-z0-9._-]{0,64}$`. `Note` is at most 200 runes and has
     no newline. Each failure is a sentence naming the field.
   - `func Path(dir string) string` is `filepath.Join(dir, File)`.
   - `func Load(dir string) (Set, error)`: a missing file is an empty set and no error. A file whose `version` is not 1
     is an error saying so. Every target is validated, and the first bad one is an error naming it.
   - `func Save(dir string, s Set) error`: writes through a temp file in `dir` and a rename, mode 0600.
   - `func Add(dir string, t Target, replace bool) error`, `func Remove(dir, name string) error` (missing name is an
     error), `func Find(s Set, name string) (Target, bool)`.
   - `func Allows(t Target, from string) bool`: true when `From` holds `*` or holds `from` compared with
     `strings.EqualFold`.
2. New `internal/cli/sshcmd.go`: `atrium ssh` with subcommands `add`, `rm`, `ls`, registered beside `resources` in
   `internal/cli/cli.go`. The directory is the room key directory the room already uses (the `room/` dir that holds
   `room.json`, the same default and `--dir` override `atrium room` takes).
   - `atrium ssh add <name> <host:port> [--from a,b|*] [--user u] [--note text] [--replace]`. With no `--from` it saves
     the target and prints `wsl is offered to no room yet. add --from <room> or --from '*'`.
   - `atrium ssh rm <name>`.
   - `atrium ssh ls` prints one line per target: name, address, from, user, note.
3. Tests, new `internal/sshtarget/sshtarget_integration_test.go`: `TestSSHTargetAddLoadRemove`,
   `TestSSHTargetRejectsBadNameAddrFromUser` (table: `WSL`, `-x`, a 33 character name, `host`, `host:0`, `host:70000`,
   `:22`, from `a b`, user `a;b`), `TestSSHTargetMissingFileIsEmpty`, `TestSSHTargetWrongVersionIsAnError`,
   `TestSSHTargetAllowsStarAndCase`, `TestSSHTargetReplaceNeedsTheFlag`. New
   `internal/cli/sshcmd_integration_test.go`: `TestSSHAddWithoutFromSaysNobody`, `TestSSHLsListsWhatAddWrote`.

   Run: `go test ./internal/sshtarget ./internal/cli -run 'TestSSHTarget|TestSSHAdd|TestSSHLs' -count=1`

### Phase 2, f-ssh-rooms-2: the target room's side (fabric, `internal/link`, `internal/api`)

1. `internal/link/room.go`: add a field to `Room`:
   `Tunnels func(ctx context.Context, from, target string) (net.Conn, int, error)`, commented: opens the named target
   for a proven source room, answering the connection or an HTTP status and a sentence. Nil serves no tunnels.
2. New `internal/link/tunnel_room.go`:
   - `const TunnelPrefix = "/_link/tunnel/"`, `const FromRoomHeader = "X-Atrium-From-Room"`,
     `const tunnelUpgrade = "atrium-tunnel"`.
   - `func (r *Room) linkHandler() http.Handler`: a request whose path starts with `/_link/` goes to `serveTunnel`
     when it starts with `TunnelPrefix` and `r.Tunnels` is set, otherwise 404. Every other request goes to
     `r.Handler` unchanged.
   - `func (r *Room) serveTunnel(w, req)`: method must be GET, `Upgrade` must equal `atrium-tunnel` (case folded),
     else 400. `target := strings.TrimPrefix(req.URL.Path, TunnelPrefix)`, `from := req.Header.Get(FromRoomHeader)`,
     an empty one is 400. Call `r.Tunnels(ctx, from, target)` with a 10 second context. On an error write the status
     and the sentence as `{"error": ...}` JSON. On success hijack (`http.Hijacker`), write
     `HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: atrium-tunnel\r\n\r\n`, then splice with a
     new `splice(a, b net.Conn)` that runs two `io.Copy` goroutines, half closes with `CloseWrite` where the
     connection has it, and closes both when both copies end. Any bytes the hijack's `bufio.Reader` already holds are
     written to the target first.
   - In `Room.Run`, set `srv.Handler` to `r.linkHandler()` instead of `r.Handler`.
3. New `internal/cli/sshtunnels.go`: `func sshTunnels(dir string) func(ctx context.Context, from, target string)
   (net.Conn, int, error)`. It calls `sshtarget.Load(dir)` on every call (so an edit needs no restart), answers 404 with
   the "has no ssh target named" sentence, 403 with the "does not offer" sentence when `Allows` is false, and dials
   `net.Dialer{}.DialContext(ctx, "tcp", t.Addr)`, a failure being 502 with the "could not reach" sentence. Wire it
   into `link.Room{...}` in `internal/cli/roomrun.go` (line 375) as `Tunnels: sshTunnels(<the room key dir>)`.
4. New `internal/api/sshlist.go`, `func (s *Server) sshList(w, r)` answering
   `{"targets": [{"name", "user", "note", "from"}]}` from a new `Server.SSHTargets func() ([]sshtarget.Target, error)`
   field. Addresses are not sent: the hub has no use for them. Register `GET /v1/ssh` in `api.go` beside
   `GET /v1/peers/rooms` (line 774), inside `if s.SSHTargets != nil`. Wire the field in the room's api setup with a
   closure over `sshtarget.Load(<room key dir>)`.
5. `internal/link/proxy.go`, `Proxy.ServeHTTP` (line 526): first thing, a path starting with `/_link/` answers
   `http.NotFound`. In the `Rewrite` func (line 191), `r.Out.Header.Del(FromRoomHeader)`.
6. Tests:
   - New `internal/link/tunnel_room_integration_test.go`, driving `linkHandler` through an `httptest.Server`:
     `TestTunnelRouteSplicesToTheTarget` (fake target writes `SSH-2.0-fake\r\n` then echoes, the client reads the
     banner and an echo), `TestTunnelRouteNeedsUpgradeAndFrom` (400 each), `TestTunnelRouteRefusalCarriesTheSentence`
     (Tunnels answers 403, the body has the sentence), `TestTunnelRouteIsAbsentWithoutTunnels` (404),
     `TestOtherPathsStillReachTheHandler`.
   - `internal/link`: `TestTheBoardProxyRefusesLinkPaths`: a request to the hub's board for `/_link/tunnel/x` is 404 and
     the fake room handler sees nothing. `TestTheBoardProxyStripsFromRoom`: a `/v1/x` request carrying
     `X-Atrium-From-Room` reaches the room without it.
   - New `internal/cli/sshtunnels_integration_test.go`: `TestSSHTunnelsRefusesAnUnlistedRoom`,
     `TestSSHTunnelsUnknownTargetIs404`, `TestSSHTunnelsDialsTheAddress`, `TestSSHTunnelsReadsTheFileEachCall`.
   - New `internal/api/sshlist_integration_test.go`: `TestSSHListHasNoAddresses`.

   Run: `go test ./internal/link -run 'TestTunnelRoute|TestOtherPathsStill|TestTheBoardProxy' -count=1`,
   `go test ./internal/cli -run 'TestSSHTunnels' -count=1` and `go test ./internal/api -run 'TestSSHList' -count=1`

### Phase 3, f-ssh-rooms-3: the `tunnel` kind on the hub, and the room's dial (fabric, `internal/link`)

1. New `internal/link/tunnel.go`:
   - `const tunnelKind = "tunnel"`, `const maxTunnelsPerRoom = 16`, `const tunnelOpenWait = 15 * time.Second`.
   - `type tunnelRequest struct { Room, Target, Card string }`, JSON `room`, `target`, `card`.
   - `type tunnelAnswer struct { OK bool; Error string }`, JSON `ok`, `error`.
   - Room side: `func (r *Room) DialTunnel(ctx context.Context, room, target, card string) (net.Conn, error)`, shaped
     as `DialGit`: refuse when not up (`this room is not attached to a hub right now`), dial with `r.Dial.Dial`, say
     hello with `Kind: tunnelKind` and the current session, write the request with `writeJSON`, read the answer with
     `readJSON` under `tunnelOpenWait`, clear the deadline, and on `ok` return `&buffered{Conn: conn, br: br}`. A
     refusal in `sayHello` whose text says the kind is unknown becomes `the hub does not carry ssh tunnels yet. update
     the hub`.
   - Hub side: `func (h *Hub) serveTunnel(ctx context.Context, from, session string, conn net.Conn, br *bufio.Reader)`:
     1. The session must be the source room's current one, as `serveGit` checks, else the welcome refuses.
     2. Write `welcome{OK: true}`, then read the `tunnelRequest` under `tunnelOpenWait`.
     3. Refuse when `identify(conn)` is empty and `h.Authenticated != nil` (the sentence with `RejoinSentence(from)`).
     4. Look up the target `attached` by `keyOf(req.Room)`. None is the offline sentence. An attached room with
        `unproven != ""` is the certificate sentence. The source and target being the same room is allowed.
     5. Count: a new `tunnels int` field on `attached`, under `a.mu`. At `maxTunnelsPerRoom` refuse. Otherwise
        increment, and decrement when the splice ends.
     6. `dc, err := h.Dial(ctx, req.Room)` under `tunnelOpenWait`. Write the GET from "The target side" with
        `FromRoomHeader: from`, read the response with `http.ReadResponse(bufio.NewReader(dc), nil)`. A 404 whose body
        is not JSON is the older room sentence. Any status but 101 answers the `error` from its JSON body, or the
        status text.
     7. Write `tunnelAnswer{OK: true}`, clear deadlines, and `splice` the source connection (with `br`'s buffered
        bytes first) and the data connection (with the response reader's buffered bytes first).
     8. Call `h.OnTunnel(from, req, err)` once, after the open succeeds or is refused, when it is set.
   - Add the field `OnTunnel func(from string, req tunnelRequest, err error)` to `Hub`, exported through a small
     `TunnelOpen` struct with `From, Room, Target, Card string` if the field type must be exported for the wiring.
2. `internal/link/protocol.go`: add `tunnelKind` to the `hearHello` switch and to its refusal sentence.
3. `internal/link/hub.go`: in the kind switch (line 351), `case tunnelKind:` with a comment in the style of its
   neighbours, `defer conn.Close()`, `h.serveTunnel(ctx, name, hi.Session, conn, br)`.
4. `internal/cli/atrium_run.go`: set `hub.OnTunnel` to record `tunnel-open` or `tunnel-refused` through the proxy's
   `RecordAudit`, detail `<from> -> <target>.<room>` plus ` card <card>` when set, plus `: <error>` on a refusal.
5. Tests, new `internal/link/tunnel_integration_test.go`, with a hub and two rooms over loopback mTLS as the attach
   tests build them, `Tunnels` on the target set to a closure over a fake target listener:
   - `TestTunnelCarriesBytesBothWays`: banner arrives, 64 KiB round trip echoes intact.
   - `TestTunnelSaysTheSourceRoomFromTheCertificate`: the source's hello says `beta`, its certificate says `alpha`, and
     `Tunnels` sees `alpha`.
   - `TestTunnelToAnOfflineRoomSaysSo`, `TestTunnelRefusedByTheTargetCarriesItsSentence`,
     `TestTunnelToAnOlderRoomSaysSo` (target's `Tunnels` nil, so `/_link/` answers 404).
   - `TestTunnelCapIsSixteen`: sixteen open, the seventeenth refused, one closed, the next opens.
   - `TestTunnelEndsWhenTheTargetLinkDrops`: close the target room's link, the source read returns an error.
   - `TestTunnelIsAudited`: `OnTunnel` sees one open and one refusal.
   - `internal/link/overlaytls_integration_test.go` line 436: add `tunnelKind` to the kinds list.

   Run: `go test ./internal/link -run 'TestTunnel|TestOverlay' -count=1`

### Phase 4, f-ssh-rooms-4: the source end, `atrium ssh proxy` and `atrium ssh config` (runtime, `internal/daemon`)

1. `internal/daemon`: add an interface `type TunnelDialer interface { DialTunnel(ctx context.Context, room, target,
   card string) (net.Conn, error) }` and an option field `Tunnels TunnelDialer` on the daemon's options, beside the
   relay option. In `daemon.go` beside `agentMux.HandleFunc("/tell", ...)` (line 1160) register
   `agentMux.HandleFunc("/tunnel", d.handleTunnel)`.
2. New `internal/daemon/tunnel.go`, `func (d *Daemon) handleTunnel(w, r)`: POST only, `Upgrade: atrium-tunnel`, body
   at most 4 KiB of `{"room","target","card"}`. Nil `Tunnels` is 503 `this room is not attached to a hub right now`.
   `DialTunnel` under a 20 second context, an error is 502 with `{"error": err.Error()}`. On success hijack, write the
   101 lines as phase 2 does, and splice. Use `link`'s splice by exporting it as `link.Splice`, or copy its ten lines,
   whichever keeps `internal/daemon` free of a new import cycle (check with `go build ./...`).
3. `internal/cli/roomrelay.go`: `type linkTunnels struct{ room *link.Room }` with `DialTunnel` calling
   `l.room.DialTunnel`. Wire it into the daemon options where the relay is wired in `roomrun.go`.
4. `internal/cli/sshcmd.go`, `atrium ssh proxy <host>`:
   - `<host>` must be three or more dot separated labels ending in `atrium`. The first label is the target, the rest
     before `atrium` joined by dots is the room. Anything else writes
     `atrium ssh proxy wants <target>.<room>.atrium, got <host>` to stderr and exits 255.
   - Find the agent listener the way `atrium tell` does (`recordedAgentAddr` in `internal/cli/join.go`). None is the
     "no atrium room is running" sentence, exit 255.
   - Dial it, write the POST from "The source side" with `card` from `ATRIUM_TASK_ID` when set, read the response with
     `http.ReadResponse`. Not 101: write the `error` from the JSON body to stderr, exit 255. On 101 copy `os.Stdin` to
     the connection and the connection (with the reader's buffered bytes first) to `os.Stdout`, and exit 0 when the
     connection closes. Write nothing else to stdout ever, since ssh reads it as the protocol.
5. `atrium ssh config [--print]`: ask the hub for the target list (phase 5's tool when it exists, else only the
   wildcard block), then write `~/.atrium/ssh_config` as shown under "Files" through a temp file and a rename, with
   `os.Executable()` resolved by `filepath.EvalSymlinks` and quoted in `ProxyCommand`. Print
   `add this line at the top of ~/.ssh/config: Include ~/.atrium/ssh_config`, and print `it is already there` instead
   when `~/.ssh/config` already has an `Include` naming that file. `--print` writes the file to stdout and nothing to
   disk. Never write `~/.ssh/config`.
6. `docs/test-plan.md`: a scenario "ssh across rooms" with the manual steps from "How it is tested without the live
   hub".
7. Tests:
   - New `internal/daemon/tunnel_integration_test.go`: `TestAgentTunnelSplicesWhatTheDialerGives` (a fake
     `TunnelDialer` over `net.Pipe`), `TestAgentTunnelWithoutAHubIs503`, `TestAgentTunnelDialErrorIs502WithTheSentence`.
   - New tests in `internal/cli/sshcmd_integration_test.go`: `TestSSHProxyParsesTheHost` (table:
     `wsl.claude-sg4.atrium`, `wsl.a.b.atrium` gives room `a.b`, `wsl.atrium` and `wsl.sg4.example` refused),
     `TestSSHProxyCarriesStdio` (an in-process agent listener answering 101 and echoing, stdin and stdout as pipes),
     `TestSSHProxyRefusalGoesToStderrAndExits255`, `TestSSHConfigWritesTheWildcardAndAbsolutePath`,
     `TestSSHConfigNeverTouchesSSHConfig` (a fake home with `.ssh/config`, its bytes unchanged after the run),
     `TestSSHConfigSeesAnExistingInclude`.

   Run: `go test ./internal/daemon -run 'TestAgentTunnel' -count=1` and
   `go test ./internal/cli -run 'TestSSHProxy|TestSSHConfig' -count=1`

### Phase 5, f-ssh-rooms-5: listing in `atrium_resources`, and the docs (fabric, `internal/link`, docs)

1. `internal/link/resources_mcp.go`:
   - `type resourceSSH struct { Room, Name, Host, User, Note string; From []string; Online bool; Seen string }`, JSON
     `room`, `name`, `host` (the `<name>.<room>.atrium` form), `user`, `note`, `from`, `online`, `last_seen`.
   - `resourcesOutput` gains `SSH []resourceSSH \`json:"ssh"\``.
   - The handler, after rooms: for each online room, `c.ask(ctx, "GET", "/v1/ssh", room, nil, &ans)` under a 3 second
     context, one room at a time. A failure or a 404 (older room) skips that room. Each answer is stored in a new
     `sshSeen map[string]sshSeenEntry` on `controlMCP` under its mutex (targets plus the time). Offline rooms listed
     from that map with `online: false` and `last_seen`.
   - The text gains `ssh targets:` with one line per target: `- wsl.claude-sg4.atrium, online, from m1mini sg3, Ubuntu
     24.04 WSL`.
   - `resourcesToolDesc` gains: `ssh targets are reached with plain ssh, scp or ssh -L to the host shown, once this
     machine has run atrium ssh config. Your machine's key must be authorized on the target, atrium holds none. A
     target only answers rooms on its from list.`
2. `atrium ssh config` (phase 4 step 5) reads this list over the room's `hub` relay op if phase 4 landed first without
   it: change its source to the relay and add the per target `User` blocks.
3. `docs/fabric/hub-and-rooms.md`: a section "ssh between rooms" of at most 15 lines, pointing here.
4. `website/docs/control-mcp.md`: extend the `atrium_resources` entry with the `ssh` field.
5. `resources.Starter` (`internal/resources/resources.go`): one comment line saying ssh targets are added with
   `atrium ssh add` on the machine that reaches them, not in this file.
6. Tests, in `internal/link/resources_mcp_integration_test.go`: `TestResourcesListsSSHTargetsFromOnlineRooms` (two
   rooms answer `/v1/ssh`, both listed with their `.atrium` hosts), `TestResourcesKeepsAnOfflineRoomsTargets` (answer
   once, detach, listed offline with `last_seen`), `TestResourcesSkipsARoomWithoutSSH` (404 answers no targets and no
   error), `TestResourcesSSHHasNoAddress` (the answer JSON has no `addr`).

   Run: `go test ./internal/link -run 'TestResources' -count=1`

## Security notes

- The target's `from` list and sshd's `authorized_keys` both have to say yes. A leaked room certificate gives reach to
  what that room may reach and no login. A leaked ssh key gives nothing without a room on the `from` list.
- The hub learns which room opened which target and when, and nothing of what was typed. Hub admins can see that, and
  it is what the audit is for.
- The board can never open a tunnel: `/_link/` is refused by the proxy, the `from` header is stripped, and the room's
  loopback board has no such route.
- Any agent on a source room may run `ssh <target>` with that machine's key, through the ordinary Bash permission
  gate. That is the same exposure the machine's keys already had. Making it narrower needs a key per card, which is out
  of scope.

## Out of scope

- Moving, minting or installing ssh keys, and editing `authorized_keys`. The operator puts the source machine's public
  key on the target, once per pair. `atrium ssh ls` on the target and the resources text both say this.
- Per card or per role limits on who may ssh.
- A remote command tool (`atrium_ssh`) for agents that cannot run ssh. Approach C, rejected above.
- The hub-run ops of the 2026-10-01 spike (`room-check`, `provision-room` and friends as typed hub ops). That is its
  own item.
- RDP, WinRM or any other protocol as a documented target. The tunnel carries any TCP, and nothing here is built or
  tested for those.
- Reconnecting a dropped tunnel, or keeping a session across a hub restart.
- Throughput tuning, compression or byte accounting on the hub.
- Editing `~/.ssh/config`. Atrium writes its own file and the operator adds one `Include` line.
