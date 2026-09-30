# The pty host protocol, as built

Package `internal/ptyhost`. Design: `docs/rnd/rolling-restart-design.md` section 3, with the G1 to G7 answers.
Protocol version `Proto = 1`. The frame shapes are in `internal/ptyhost/proto.go` and the Go client is in `client.go`.

## The channel

- Windows: a named pipe `\\.\pipe\atrium-ptyhost-<hash>` through `go-winio`, with the DACL `D:P(A;;GA;;;<your SID>)`
  and nothing else, remote clients rejected. go-winio makes an instance only while an `Accept` is pending, and a
  client that arrives in the gap waits (it does not connect to a dead instance), so the host re-issues `Accept` at
  once.
- Elsewhere: a unix socket `<state dir>/ptyhost-<hash>.sock` at mode 0600. Mind `sun_path`.
- `<hash>` is the first 8 bytes of sha256 over the state dir's absolute path (lowercased on Windows), a NUL, and the
  user's SID (uid off Windows). `ptyhost.Address(stateDir)` computes it.

## Framing

One JSON object per frame, newline after each, `[]byte` fields as base64. A request has `v` and an optional `seq`. A
reply echoes `seq` and has `ok`, and `err` when `ok` is false. A pushed event has `ev` and no `seq`.

## Who may do what

`probe` needs nothing. `hello` makes a connection THE daemon. Every other verb from a connection that is not the
daemon fails with "say hello first". One daemon at a time.

## Requests and replies

| v | request fields | reply fields |
| --- | --- | --- |
| `probe` | none | `proto`, `build`, `pid`, `daemon` (one is connected), `in_job`, `ptys`. Evicts nothing |
| `hello` | `proto`, `build`, `takeover` | `proto`, `build`, `pid`, `in_job`, `took_over`. Refused with "a daemon is already connected" unless `takeover` |
| `spawn` | `id`, `kind` (`runner` or `shell`), `argv`, `env` (whole environment, absent means the host's), `cwd`, `cols`, `rows`, `ring` (bytes) | `pid`, `run_id` |
| `list` | none | `list`: every pty as `{run_id, id, kind, pid, cols, rows, started, exited, exit_code, ring_start, out_offset}`. `exited` and `exit_code` are never omitted |
| `attach` | `run_id`, `from` | `info` (as in list), `from` (effective), `truncated`, `cuts`, `data` (replay bytes). Live events follow |
| `write` | `run_id`, `data` | ok |
| `resize` | `run_id`, `cols`, `rows` | `cut` `{off, cols, rows}`, recorded before the resize is applied |
| `signal` | `run_id`, `sig` (`term` or `kill`) | ok. On Windows `term` is a kill |
| `collect` | `run_id` | ok. Refused while the runner is alive. Then the host forgets the pty |

Every verb after `spawn` is addressed by `run_id` alone. An unknown or empty `run_id` is refused with "unknown run_id".
`run_id` is a 26 character ULID-style id: a 48 bit millisecond clock then 80 random bits.

## Events, after an attach

- `{"ev":"out","run_id","off","data"}` live output at an absolute offset.
- `{"ev":"exit","run_id","exited":true,"exit_code":N}`. `exit_code` is always present, 0 included.

The reply to `attach` is queued ahead of every event. Snapshot, subscribe and reply happen under the pty's lock, so
no byte is missed or repeated. An attach to an exited pty gets the replay and then the exit event.

## The ring and the cuts

Per pty, bounded (`ring`, default 1 MiB, at most 512 MiB), with absolute offsets that never reset. The window is
exactly the last `ring` bytes. `cuts` is a list of `(off, cols, rows)`. In an attach reply the first cut is rebased to
the effective `from`, so the reader knows the width of the first byte. A `from` older than the ring gets the whole
ring and `truncated`.

## Slow readers (G4)

Each client has a 4 MB queue for live output (replies do not count, since a replay may exceed it). On overflow the
host closes that client and logs it, and the drain is never slowed. The client reattaches from its last offset.

## Exit ordering

The exit is published after the ring has drained: the read ended, or no bytes for 200 ms (bounded to 2 s), since a
ConPTY read does not end with the runner. It goes down the same queue as the output, so no client sees it before the
last bytes it was sent. A byte arriving after that is kept in the ring and reaches a later attach.

## Lifetime

The host exits when it holds no pty (collected or not) and no daemon has been connected for `IdleExit` (default ten
minutes). It never exits with a pty in it. `ptyhost.Start` copies the binary to `atrium.ptyhost[.exe]` beside it and
starts that detached with `internal/detach`.

## Closing (G7)

Every handle closes through one `closeOnce`. `Host.Close`, a client's `close` and the client-side `Close` are all safe
concurrently.
