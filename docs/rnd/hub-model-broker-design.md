# Hub model broker: a local model on one room, used by runners on every room

Status: design draft, 2026-10-25. Nothing here is built. It changes no code. Written for
`rnd-hub-brokers-local-ai-models`.

Clint, 2026-10-21: "i want the hub to be a broker to other rooms too for local ai models if it can".

Read for this design:
- `internal/link/AGENTS.md` and the package comment in `internal/link/link.go` (the room dials, the hub holds no work)
- `internal/link/stream.go` and `docs/rnd/ssh-via-hub.md` (bytes from a caller, through the hub, to a loopback port on
  another room, on one borrowed data connection)
- `internal/link/git.go`, `internal/gitsync/forward.go` and `internal/gitsync/cardtoken.go` (a room's loopback
  forwarder, a card token on it, a link connection kind per request)
- `internal/link/proxy.go:642` (the `/v1/git/` path the board may never reach)
- `internal/store/harness.go:36` and `:85` (a runner row's `env`, `model_env`)
- `docs/rnd/gateway-fit.md`, `docs/rnd/small-model-orchestration.md`, `docs/rnd/opencode-token-routing.md`.

## 0. The answer

- **Yes, and it is the ssh stream again, one layer up.** A card on room A talks HTTP to its own room's loopback port.
  Room A dials a new link connection kind, `model`, to the hub. The hub borrows one data connection from room B's pool,
  which room B dialled, and room B reverse-proxies the request to its model server on `127.0.0.1`. No port opens on
  the model machine, the hub dials nobody, and the hub keeps nothing past the request.
- **The bytes are an OpenAI-compatible HTTP API, passed through untranslated.** `/v1/chat/completions`,
  `/v1/models`, and `/v1/messages` where the model server speaks it. Server-sent events stream because nothing is
  translated, the same reason the board's websockets work over the link today.
- **A runner is pointed at it by an env variable on its runner row.** The room fills
  `{atrium_model_base}` with `http://127.0.0.1:<agent port>/model/<card token>/<room>/v1` at launch. opencode and codex
  take a base URL today. Claude Code needs a server that speaks Anthropic Messages, so it is a later step, not the
  first.
- **Who may use it is a hub setting, set from the hub's machine only.** A model room offers. The hub lists which rooms
  may use which offer. A card proves itself to its own room with a card token, the room proves itself to the hub with
  its certificate, and the model room trusts only requests the hub carried. Atrium holds no model credential.
- **The model room owns the queue.** One GPU is one queue, so the room with the GPU counts slots and holds the line,
  and the board reads the line from there.
- **The prototype is one offer, one asking card, a fake model server in a test, then Ollama on sgg.** Section 8.

## 1. What a room advertises

A room offers a model server by configuration, never by discovery. A port that happens to answer on loopback is not an
offer.

```toml
[[model_offer]]
name     = "ollama"                    # unique on the room, the path segment after the room
base     = "http://127.0.0.1:11434"    # loopback only, checked by the room at load
kind     = "openai"                    # what the server speaks. "openai", later "anthropic" as well
slots    = 1                           # concurrent requests the room lets through
queue    = 8                           # requests that may wait for a slot
wait     = "120s"                      # how long one may wait before a 503
models   = []                          # empty means whatever the server lists
```

- `base` must resolve to a loopback address. Anything else is refused at load with the sentence why, so an offer can
  never become a way to reach the model room's LAN.
- The room reads `GET <base>/v1/models` every 30 seconds, and `GET <base>/api/ps` when the server is Ollama, to learn
  which models exist and which are loaded in memory. A failed read marks the offer `down`, never removed.
- The room tells the hub on the existing `announce` kind (`internal/link/announce.go`): offer name, models, slots,
  queue length, in flight, the cards in flight, loaded models, and `up` or `down`. The hub keeps the latest in memory
  only, as it does the cache. A hub restart forgets it and the next announce brings it back.
- The hub lists offers at `GET /_hub/models` and adds them to `atrium_resources`, so a director picking a runner sees
  `qwen3-coder:30b on sgg (1 slot, idle)` beside the machines.

A model is named `<model>@<room>`, the same shape as a card's `name@room`. The hub never picks a room for a bare model
name in the first cut. Two rooms offering the same model is a choice a person makes, and section 9 asks whether the
hub should load-balance later.

## 2. Transport

```
card -(A)-> room A :agent port /model/<token>/<room>/<offer>/...
       room A -(B)-> HUB    a `model` connection room A dialled
              HUB -(C)-> room B   one data connection from room B's pool, room B dialled it
                     room B -(D)-> 127.0.0.1:11434   plain HTTP
```

- **(A)** is a path on the room's agent port, the port that already serves the git forwarder at `/git/`. The card
  token in the path decides which card is asking. A request without a live token is a 404 and never reaches the link,
  as `gitsync.Forwarder` does.
- **(B)** is a new hello kind, `model`, beside `git` and `stream`. After the welcome the connection is plain HTTP/1.1
  with the hub answering. One connection per request, closed after it, so no link connection outlives the call. The
  hub says `Model: true` in its welcome and a room never dials the kind at a hub that did not, as with `Git`.
- **(C)** reuses the pool exactly as the board does. The hub sends `POST /v1/modelbroker/<offer>/<rest>` with the
  headers `X-Atrium-Model-Room` (from room A's certificate) and `X-Atrium-Model-Card` (as room A said). The hub
  strips both from any request that did not arrive on a `model` connection.
- **(D)** is `httputil.ReverseProxy` to the offer's `base`, with `FlushInterval: -1` so each server-sent event leaves
  as it arrives.

**Why not the generic board proxy.** `/v1/modelbroker/` must never be reachable from the board, for the same reason
`/v1/git/` is not (`proxy.go:642`). A board reached over a share would otherwise be a free GPU for anyone with the
share. `proxy.go` refuses the prefix, and only the `model` kind's handler writes it.

**Why not an overlay service.** A ziti or zrok service per model server works, and clint's overlay can already do it.
It needs an identity on every asking machine, a service per offer, and policy kept outside atrium, and the board
cannot show who is using it. The link already proves both rooms. An overlay is the right answer for a model server
used by things that are not atrium cards, and is out of scope here.

**Why not raw bytes like ssh.** A raw stream would carry the bytes but hide the request. The model room needs the
path to pick the offer, the body's `model` field to check it against the offer's list, and the response's `usage`
for accounting. HTTP through the pool gives all three at no extra cost.

**Timeouts.** A completion can run for minutes. The link's control timeouts do not apply. The bound is the offer's
`wait` for a slot, then an idle read timeout of 120 seconds between bytes, so a stalled server frees its slot and
its data connection. A data connection spent on a model request is not returned to the pool while the request runs,
and `MaxModelCalls` (16, like `MaxStreams`) caps how many one model room may have open at once.

## 3. Pointing a runner at it

A runner row already has `env` (`internal/store/harness.go:36`). The room fills one placeholder in it at launch:

| placeholder | becomes |
| --- | --- |
| `{atrium_model_base}` | `http://127.0.0.1:<agent port>/model/<card token>/<room>/<offer>/v1` |

The room and offer come from the launch, `atrium launch --runner opencode --model qwen3-coder:30b@sgg`. The room splits
`@sgg`, checks the hub lists that model there, puts the bare model in the runner's `model_env` or args as today, and
fills the base. A launch naming a model no offer lists is refused at launch, not at the first request.

Per runner:
- **opencode**: a provider block in its config with `baseURL` from the env. Works today with any OpenAI-compatible
  server.
- **codex**: `model_providers.<name>.base_url` in `config.toml`, or `OPENAI_BASE_URL`. Codex speaks Responses as well
  as chat completions, and Ollama answers both (unverified for LM Studio).
- **claude**: `ANTHROPIC_BASE_URL`. This needs the model server to speak Anthropic Messages. Recent Ollama releases
  say they do (unverified here, not run on sgg). The broker passes `/v1/messages` through unchanged, so if the server
  speaks it the broker needs nothing more. Atrium's own hooks and transcript reading are unchanged, since the runner is
  still Claude Code.
- **a plain HTTP client in a card** (a script, an eval): reads `ATRIUM_MODEL_BASE` the same way.

The card token is the git card token (`gitsync.CardTokens`), so it dies with the card, and a paused or culled card
loses the model with its git. One token per card, two uses. If the security design's 2c card tokens replace it, both
uses move together.

## 4. Auth and who may use it

Four checks, each at the hop that can make it:

1. **Room A, hop (A):** the card token is live. This says which card. Anything else on room A's machine without the
   token gets a 404.
2. **The hub, hop (B):** room A's name comes from its certificate, never from the hello. The hub reads a hub setting,
   `model_grants`, rows of `(offer room, offer, asking room or *)`. No row, no carry, and the answer names the setting.
   The default is no rows: an offer is visible on the board and usable by nobody until a person grants it.
   `model_grants` is set from the hub's machine only (`edge.LocalOperator`), like the notify command and launch caps,
   because granting is giving away a machine's GPU.
3. **The hub, hop (B):** the `model` field of a JSON body is checked against the offer's announced list when the list
   is not empty. A body the hub cannot parse as JSON passes for `GET` and is refused for `POST`.
4. **Room B, hop (D):** `/v1/modelbroker/` answers only on a link data connection, never on the room's own loopback
   listener, and only with the hub's headers. The offer must exist in room B's own config. The hub cannot add an
   offer, and cannot point room B at a base it did not configure.

**Credentials.** Atrium holds none. A loopback Ollama or llama.cpp server has no key. LM Studio and vLLM can be run
with one. If a server needs a key, room B's config names an environment variable of room B's own process
(`key_env = "LMSTUDIO_KEY"`) and room B adds the header on hop (D). The key never crosses the link, never reaches the
hub, and is never logged. This is the "atrium holds the NAME" rule from `notify.go`. Section 9 asks whether even that
is wanted.

**What the hub may not do.** Stop a model, load one, pull one, or change an offer. The hub carries requests. The
model room owns its server. This keeps the rule that the hub cannot stop a room's work.

## 5. Queueing and concurrency on one GPU

- **The model room is the only place that counts.** Each offer has `slots` (default 1) and a FIFO line of up to
  `queue`. A request takes a slot or joins the line. A full line, or a wait past `wait`, is a 503 with `Retry-After`
  and the sentence `sgg/ollama is busy: 1 running, 8 waiting`.
- **Slots are per offer, not per model.** One GPU serves one offer. Two models on one 24 GB card swap each other out
  of memory, and a swap costs more than a wait, so the default of one slot is also the right one for mixed models.
  Ollama's own `OLLAMA_NUM_PARALLEL` and `OLLAMA_MAX_QUEUE` stay as the server's settings. Atrium's line sits in front
  so it can be seen and so a request that will time out is refused early.
- **The model room's own cards go through the same line.** A card on sgg using its own Ollama through the broker path
  waits like everyone else. A process on sgg that calls Ollama directly bypasses the line, and the board says so by
  showing Ollama's `/api/ps` beside atrium's count.
- **Fairness is FIFO in the first cut.** Per-room or per-card shares are a later question (section 9). A single
  runaway card is bounded by its own runner, which sends one request at a time.
- **A client that goes away frees its place.** A cancelled request in the line is dropped. A cancelled request in a
  slot cancels the upstream request, which Ollama honours by stopping generation.

## 6. What the board shows

A **Models** strip on the Rooms view, one row per offer:

| room | offer | state | models | in use | waiting | by |
| --- | --- | --- | --- | --- | --- | --- |
| sgg | ollama | up | qwen3-coder:30b (loaded), gemma3:27b | 1 / 1 | 2 | `rnd-x@m1mini` |

- `state` is `up`, `down` (the server did not answer the last read), or `offline` (the room is not attached, with when
  it was last heard, as for an ssh stream).
- `by` lists the cards in a slot. A card in line shows as waiting on its own card.
- A card launched with a brokered model has a chip `qwen3-coder:30b@sgg` where the model chip goes today, and the
  chip turns amber while the card waits in a line.
- Grants are shown read-only on the board and edited only from the hub's machine.

All of this comes from the announce. The board asks the hub, and the hub answers from memory, so the strip costs room
B nothing per viewer.

## 7. Failure

- **Model room offline.** The hub answers hop (B) with 503 and `sgg is offline (last heard 14:02)`. Nothing is queued
  on the hub, because the hub holds no work. The runner sees an HTTP error and does what it does with one, which for
  opencode and codex is a retry with backoff.
- **Server down on an online room.** Room B answers 502 with `sgg/ollama did not answer at 127.0.0.1:11434`.
- **The link drops mid-stream.** The response ends early. The broker never retries a request once its body was sent,
  since a completion is not free and may have had effects through tools. Same rule as a cross-room launch.
- **Hub restart mid-stream.** Same as a link drop. The asking card's next request goes on a new `model` connection
  once room A has redialled.
- **The asking card ends.** Its token dies, its open request is cancelled at room A, and the cancel walks down to
  room B and frees the slot.

## 8. The smallest prototype

One branch, three steps, each with an integration test in `internal/link` using the two-room harness the
`modelroute_integration_test.go` file already uses.

1. **Room B serves an offer on the link only.** Config `[[model_offer]]` with loopback check, the
   `/v1/modelbroker/<offer>/` handler as a reverse proxy with a one-slot semaphore and a line, and `proxy.go` refusing
   the prefix from the board. Test: a fake OpenAI server on loopback streams three SSE chunks, and the test reads them
   through room B's link handler. A board request to the prefix is refused.
2. **The hub carries a `model` connection.** The hello kind, the welcome flag, the `model_grants` check (one
   in-memory setting for the prototype), the headers set and stripped. Test: room A's `DialModel` reaches room B's
   fake server and streams. Without a grant it is refused with the sentence. With room B detached it is a 503 naming
   the room.
3. **Room A's agent port and one runner row.** `/model/<token>/<room>/<offer>/` on the agent port, the
   `{atrium_model_base}` placeholder, and an opencode row on the hub's seeded list left disabled. Test: two cards each
   send a request, the second waits for the first, and the waiting count in the announce is 1 while it does.

Then by hand: sgg offers Ollama with `qwen3-coder:30b`, m1mini launches an opencode card at `qwen3-coder:30b@sgg`, and
the card writes a file. That proves the bytes, the auth, the line and the runner together.

**Not in the prototype:** usage accounting, the board strip (the hub's `/_hub/models` JSON is enough to watch),
Anthropic Messages for Claude, keys on the model server, more than one offer per room, and load balancing.

**Accounting, the step after.** Room B reads `usage` from a non-streaming response and from the last chunk of a
streaming one (it asks for `stream_options.include_usage` when the client did not), and Ollama's `eval_count` and
`prompt_eval_count` otherwise. It records `(card, room, model, prompt tokens, completion tokens, seconds in slot)` in
its own store, a new table and so a new migration, and announces per-card totals. The asking room reads them back
into the card's usage tab beside Claude spend, marked `local, no marginal cost`. The time in slot is the honest cost
on a shared GPU, and is the number the board should rank by.

## 9. Open questions for clint

1. Which machine and which server first: Ollama on sgg, or LM Studio or llama.cpp somewhere else? And which model
   should the prototype's hand test use?
2. Should a grant be per asking room, per department, or per card? The draft is per room, with `*` for every room.
3. Should the model room's own cards be allowed to bypass the line and call the server directly, or should the
   broker path be the only path on a model room too?
4. When a model server needs a key, may room B read it from a named env variable of its own process, or should
   atrium refuse to offer any server that needs a key?
5. Is FIFO fair enough, or should a room or a director's card jump the line?
6. When two rooms offer the same model, should the hub pick one for a bare model name, or must every launch name the
   room?
7. Is Claude Code on a local model wanted at all, given that `docs/rnd/small-model-orchestration.md` puts local models
   at Haiku tier for the minion's errands only? If not, the prototype targets opencode and codex alone.
8. Should usage on a local model count against anything, such as the freeze budget, or be shown only?
9. Should the broker also serve things that are not cards, such as a script on the hub's machine, or is a card token
   the only way in?
