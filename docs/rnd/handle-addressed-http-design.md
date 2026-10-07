# Handle-addressed HTTP: a script names a card the way an agent does, and the hub routes it

Status: built. A room resolves a card's name on `/v1/tasks/<name>` (`internal/api/cardnames.go`), the hub routes it
(`internal/link/cardroute.go`), and `atrium task`, `exit` and `new-context` take a name.

Origin: design, 2026-09-30, @rnd. Item `docs/backlog/runtime/r-new-handle-addressed-http.md`. Built by @runtime. Nothing
here is built.

## 1. The answer in seven lines

1. **One place per process turns a name into an id, before routing.** On the hub that place is `placeCard`, which
   already sees every `/v1/tasks/<segment>` request. On a room it is one wrapper in front of the mux. No handler
   changes, and every `{id}` route takes a name at once.
2. **One resolver.** The matching the MCP tools do in `resolvePeer` becomes a pure function over a card list. The MCP
   tools and the HTTP path both call it. `SplitAddress` stays the one parser of `name@room` and `room~id`.
3. **The hub rewrites the path to the bare id before it forwards.** So a room on an older build is reached by handle
   with no room deploy, and the room keys its cards by the id it minted, as it always has.
4. **Ambiguity is a 409, never a guess.** A bare name that two live cards on two rooms answer to comes back 409 with
   both, spelled `alias@room`, so a script never reaches the wrong agent.
5. **A miss is a 404 that says what would have worked**, the way `atrium_say` does. The room's 500 for an unknown id
   is fixed first and on its own.
6. **Every answer names what it reached**, in `X-Atrium-Card` and `X-Atrium-Handle`, so a script can check before it
   acts again.
7. **A small CLI on top**: `atrium task`, `atrium exit`, `atrium new-context`, and `atrium launch --onto`. Nobody
   hand-writes curl.

## 2. What is there today

| piece | where | what it does |
| --- | --- | --- |
| address parser | `internal/link/address.go` `SplitAddress` (copy in `internal/daemon/address.go`, same table test) | `name`, `@name`, `name@room`, `@name@room` split on the last `@`, and `room~id` as `id@room` |
| MCP resolver | `internal/link/control_mcp.go` `resolvePeer`, `resolveCard` | asks the room for `/v1/tasks`, then matches: wire name or id exactly, then alias (live before done, then newest, never dead). A miss lists the handles that would have worked |
| hub card routing | `internal/link/cardroute.go` `placeCard` | `room~id` names the room. A plain id with two or more rooms is looked up with `roomHolding`, which fans `GET /v1/tasks/<id>` out to every room and takes the first 200. With one room it passes straight through |
| room card routes | `internal/api/api.go`, about sixty `/v1/tasks/{id}...` routes | `r.PathValue("id")` goes straight to `st.Get`. No name is accepted |
| room name lookup | `internal/daemon/peers.go`, `relay.go`, `roomhold.go` | the room's own order for a name: `GetByWireName(Qualify(name))`, then `GetByAlias(name)` |
| the 500 | `internal/api/api.go` `getTask` then `fail` | `st.Get` returns `sql.ErrNoRows` for an unknown id, and `fail` answers every error that is not a halt with 500 |

## 3. The hazard that decides the order

`roomHolding` asks each room `GET /v1/tasks/<segment>` and believes the first 200. Today a name gets 500 from every
room, so a name is simply not found. **The moment a room learns to answer a name, that changes.** Two rooms with a card
aliased `rnd` both answer 200, the first to answer wins, and the hub then caches `rnd` as living on that room for
`cardRoomTTL`. A script says `rnd` and reaches whichever room was fastest.

So two rules, and the order of the stages follows from them:

- **`roomHasCard` checks the id it got back.** It already reads the body to drain it. It decodes `id` and answers yes
  only when that equals the segment it asked about. A name can then never be "held" by a room through this path.
- **The hub resolves a name before it ever calls `roomHolding`.** `roomHolding` is for ids and stays for ids.

The first rule ships in the hub stage (H1), which lands before the room stage (R2). A hub that checks ids is safe
against rooms of any build. A room that answers names in front of an old hub is not, so R2 must not be deployed
to a room while its hub is older than H1. The room deploy runs from the hub build, so this holds if R2 merges after H1.

## 4. What counts as a name, and how it resolves

A path segment is resolved in this order. The first that matches wins.

1. **`room~id`**: a tagged id. Routed to that room as today.
2. **`name@room`** (and `@name@room`): resolved against that room's list only.
3. **An id a room holds**: the existing `roomHolding`, now checking the returned id. An id is a ULID, a handle is a
   name somebody chose, and the two cannot collide, so trying the id first costs the board nothing: every request the
   board makes names an id and takes this path exactly as it does now.
4. **A bare wire name or alias** (`rnd`, `@rnd`, `atrium-87300`): resolved against every attached room.

Step 4 runs only after step 3 missed, so a name costs the fan-out that a miss costs today, plus one list request per
room. That is a script's cost, not the board's.

### The shared resolver

`resolvePeer`'s loop moves into `internal/link/resolve.go`:

```go
// matchCard finds `who` in one room's list: wire name or id exactly, then alias,
// live before done, then newest, never dead. The rule resolvePeer has always had.
func matchCard(cards []ctlCard, who string) (ctlCard, bool)

// resolveAcross finds `who` on every room in `lists`. Exactly one live match wins.
// No live match and exactly one done match wins. Anything else is ambiguous.
func resolveAcross(lists map[string][]ctlCard, who string) (room string, card ctlCard, all []candidate, err error)
```

`resolvePeer` becomes `matchCard` plus the "these would have worked" error it already builds. `resolveCard` is
unchanged in shape. The HTTP path calls `resolveAcross` for step 4 and `matchCard` for step 2. The lists are fetched
live, one `GET /v1/tasks` per attached room in parallel, bounded at the 10 s `roomHasCard` uses, because an alias set a
second ago must resolve and the hub's announcement cache can be two seconds behind. A room that does not answer is
left out and named in the error when nothing matched.

### Across rooms: live beats done, two live is a 409

Inside one room the store already keeps two LIVE cards from holding one alias (`SetAlias`), and a done card answers
behind a live one. Across rooms nothing keeps two apart, so:

| live matches | done matches | answer |
| --- | --- | --- |
| 1 | any | that card |
| 0 | 1 | that card |
| 0 | 0 | 404, with the handles that would have worked |
| 2 or more | any | 409, every live match as `alias@room` and `room~id` |
| 0 | 2 or more | 409, every done match |

A wire name is room-qualified (`atrium name` prefixes it), so a bare wire name never matches twice in practice. It
goes through the same table anyway.

**The MCP aggregate caller changes a little.** A caller with no room asks the aggregate list today and takes the best
alias match across rooms. With `resolveAcross` it gets the same 409 wording for two live holders. A caller on a room is
unchanged: a bare name means its own room, as `SplitAddress` documents.

## 5. What the hub does with it

In `placeCard`, before the id lookup, and regardless of how many rooms are attached (the one-room early return stays
for ids only):

1. Resolve the segment as in section 4.
2. Rewrite `r.URL.Path` and `r.URL.RawPath` so the segment is the bare id. Set `cardRoomKey` to the owning room.
3. When the caller used a room-qualified form, set `taggedKey` so the answer is retagged, as a `room~id` request is
   today (`retagCard`).
4. Set `X-Atrium-Card` (the id as the caller's scope names it, `room~id` when two or more rooms are attached) and
   `X-Atrium-Handle` (`handle@room`) on the response before proxying.

`POST /v1/launch` with a `task_id` in the body (launching onto an existing card) takes the same resolution. That path
already reads and rewrites the body in `launchCardIn`. It rewrites a name to the bare id the same way it rewrites a tag.

Errors from the hub are JSON like `cardUnplaced`'s, with a list:

```json
{"error": "no card called \"rnd\" on room claude-sg4, room sgg", "would_work": ["rnd-director@claude-sg4 (@rnd)"]}
{"error": "\"rnd\" is on two rooms. name the room", "candidates": ["rnd@claude-sg4", "rnd@sgg"]}
```

## 6. What the room does with it

**R1, the 500.** `fail` maps `errors.Is(err, sql.ErrNoRows)` to 404 with `{"error": "no such card"}` before the
halt check falls through to 500. One change fixes every route, not only `getTask`. Four handlers already test for
`ErrNoRows` themselves (`api.go` 1151 and 1292, `replies.go`) and are left alone. A route where "no rows" means
something other than a missing card still means "not found", so 404 is not wrong there either. The regression test
walks every `GET /v1/tasks/{id}...` route with an unknown id and expects no 5xx.

**R2, names on the room's own port.** One wrapper in front of the mux, for paths under `/v1/tasks/<segment>` whose
segment is not in the hub's `notCards` set (`prune`, `pin-order`):

1. `st.Get(segment)`. Found: pass through untouched. This is every request the board makes.
2. Otherwise the room's own order, the one `peers.go` and `relay.go` already use: `GetByWireName(Qualify(seg))`, then
   `GetByAlias(seg)`. A leading `@` is accepted.
3. Found: rewrite the path to the id, set the two response headers, pass through.
4. Not found: 404 with the live handles that would have worked, as `resolvePeer` lists them.

A `name@room` naming another room is a 404 on a room's own port that says to ask the hub. A room does not forward.

## 7. Escaping

- `@` and `~` are legal in a path segment (RFC 3986: `@` is a `pchar`, `~` is unreserved). Neither needs escaping,
  and curl sends both as typed: `curl -X POST http://127.0.0.1:7778/v1/tasks/rnd@claude-sg4/exit`.
- Percent-encoded forms are accepted the same way, since Go decodes `r.URL.Path`: `%40` for `@`, `%7E` for `~`.
- A handle never holds `/`, `?`, `#` or a space. The alias rules (`NormalizeAlias`) and wire names already rule them
  out. A script building a URL from something else should `PathEscape` it.
- `localhost` costs two seconds on this machine (IPv6 tried first). The CLI and every documented curl line use
  `127.0.0.1`.

## 8. The CLI

These routes are on the human API (the board's port), not the agent listener that `atrium tell` and `atrium peers`
talk to. So the CLI reads the board address from what `whereami.go` writes down, with `--url` to override, and prefers
the hub's board when one is running. Every verb prints what it reached, from `X-Atrium-Handle`.

| command | route | notes |
| --- | --- | --- |
| `atrium task <who> [--json]` | `GET /v1/tasks/<who>` | a short summary: handle, room, status, doing, waiting. `--json` prints the card |
| `atrium exit <who>` | `POST /v1/tasks/<who>/exit` | asks the runner to leave, the way `atrium_exit` does |
| `atrium new-context <who>` | `POST /v1/tasks/<who>/new-context` | what the scripts in `D:\tmp` do by hand to cycle a director |
| `atrium launch --onto <who>` | `POST /v1/launch` with `task_id` | extends the existing `launch` command |

`atrium tell` already covers say. Patch stays HTTP only in this item (section 11, item 2).

## 9. Which routes are in stage 1

**All of them.** The item suggested five routes (task read, exit, new-context, say, patch). Resolving in `placeCard`
and in one room wrapper means there is no per-route work, so a list would be extra code to hold routes out. The CLI
covers four verbs, and every other route takes a name through curl.

One route is watched: `GET /v1/tasks/{id}/attach`, the terminal websocket. The board always attaches by id, which is
step 3 and unchanged, so the upgrade is gated on no new work (the hub-side attach flicker was a fan-out in front of this
upgrade, fixed in 3bf0f30). A name on attach resolves like any other and costs a script the list requests.

## 10. The build

Sizes: S is up to half a day for one worker, M is a day.

| stage | owner | size | what | deploy |
| --- | --- | --- | --- | --- |
| R1 | @runtime | S | `fail` maps `sql.ErrNoRows` to 404. Test: every `GET /v1/tasks/{id}...` with an unknown id answers no 5xx | room |
| H1 | @runtime | M | `internal/link/resolve.go` with `matchCard` and `resolveAcross`, and `resolvePeer` rebuilt on it. The shared table test with `internal/daemon/address.go` stays. `roomHasCard` checks the returned id. `placeCard` resolves names (section 5) including a launch body `task_id`. 404 with `would_work`, 409 with `candidates`, the two headers. Tests: two rooms with one alias live on each (409), live on one and done on the other (the live one), a room not answering, an old room that 500s on names (still resolved by the hub), and the board's id path untouched | hub only |
| R2 | @runtime | S | the room wrapper (section 6). Merged after H1 (section 3) | room |
| C1 | @runtime | S | `atrium task`, `atrium exit`, `atrium new-context`, `atrium launch --onto` | none (CLI) |

R1 and H1 are independent and can run at once. **H1 alone gives clint what was asked**: curl to the hub by handle,
against rooms as they are today. R1 and R2 ride the next room deploy rather than asking for one. C1 needs H1 or R2.

Test plan: a new section under the most recent letter in `docs/test-plan.md`, with the curl lines from section 7 run
against the hub with two rooms attached, the 409 case, and the room's own port by alias after R2.

## 11. Decided, and questions for later

Decided by the orchestrator on the doc's defaults, 2026-09-30, while clint is away. Each can be reversed.

1. **Destructive verbs take a bare alias**, the same as `atrium_exit`, and print what they reached. An alias moves to a
   new card once the old one is done, so `atrium exit sa12` meant for a done `sa12` reaches a new live `sa12` if one
   exists. For later: should `exit`, `kill`, `cull` and `DELETE` refuse a bare alias and require a handle or an id?
2. **No CLI patch verb.** Patch is `curl -X PATCH` with a JSON body. For later: is `atrium card set <who> --alias ...
   --tags ...` wanted?

Readable card URLs for the board, the phone and lent sessions are `docs/rnd/card-urls-design.md`, built on this
resolver.
