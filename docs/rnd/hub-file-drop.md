# A hub file drop: rooms hand files to each other through the hub

Status: design draft, 2026-10-10. Nothing here is built. It changes no code. Written for f-hub-file-drop.

## The problem

On 2026-10-06 a usage worker on m1mini needed `session_usage` and task data from claude-sg4, sg4-control and sg3. m1mini
cannot ssh to those machines. The orchestrator copied raw sqlite files by scp through sg4, by hand. Clint's reaction:
"this is why a hub filesystem is useful so rooms can push to the hub... i thought we had that feature but i guess not?"

Every room already dials the hub, so the hub is the one place every room can reach. What is missing is a way for a card
to leave a file there and for a card on another room to take it away again.

## What exists, and why it is not this

| Thing | What it does | Why it does not fit |
| --- | --- | --- |
| `atrium_publish` (`internal/link/docs_mcp.go`, `internal/hubstore/docs.go`) | puts a document on the hub at `/d/<slug>` for people to read. 5 MiB of text, 20 MiB of anything else, 30 an hour per card | versioned, permanent, rendered, and written for readers. There is no tool for a card to fetch one back |
| Card file upload and download (`/v1/tasks/{id}/files`, `internal/api/files.go`) | moves a file between the board and one card on one room | files never leave a card (resilience rule 8, bounded by `internal/safepath`). It reaches no other room |
| Hub git (`docs/fabric/git-sync-design.md`) | carries code between rooms, by branch | code only, and a sqlite file is not a commit anyone wants |
| the roomspec link kind (`internal/link/roomspec.go`) | one room reads its own spec | 64 KiB, one room, one purpose |

So the pieces for a drop all exist. A store with blobs, caps and secret checks (hub documents), a way for the hub to
read a file out of a card through safepath (`readCardFile`), and a way to write one into a card through safepath (the
upload handler). The drop joins them.

## The design in one picture

```
   card on m1mini                    HUB                              card on sg4
  +--------------+  atrium_put  +--------------------+  atrium_get  +--------------+
  | usage.sqlite |------------->| drop_files (rows)  |------------->| .atrium/     |
  | (own card    |  hub asks    | <hub dir>/drop/    |  hub writes  |   incoming/  |
  |  directory)  |  its room    |   <sha256> (bytes) |  via its room|   usage.sqlite
  +--------------+  to read it  | expiry sweep       |  (safepath)  +--------------+
        ^                       | audit log          |                     ^
        | GET /v1/tasks/{id}/   +--------------------+                     | POST /v1/tasks/{id}/files
        | files (room, safepath)          ^                                | (room, safepath)
        |                                 | board: list, download, delete  |
   room m1mini  <== room dials hub ==>  board  <== room dials hub ==>   room sg4
```

Both tools are calls to the hub's control MCP, the same door `atrium_publish` uses. The hub never touches a room's disk.
It asks the caller's room to read or write, over requests it already makes to rooms today. No new link kind, no new
port, and nothing is opened from the hub towards a room that the room did not dial first.

## Where files live

Beside hub documents, not inside them.

- A new table in `hub.db`, `drop_files`: `name`, `size`, `sha`, `by` (card handle), `card` (`room~id`), `room`, `at`,
  `expires`, `note` (free text, the putter's hint at who it is for), `downloads` and `last_get`.
- The bytes are a file under `<hub dir>/drop/<sha256>`, written by the same write-then-rename helper hub documents use
  (`writeBlob`, `hexSHA`). Equal bytes share one file. The sweep removes a blob only when no row names it.
- Reused as they are: `hubstore.SecretFileName`, `hubstore.SecretContent`, the `DocError` kinds and their HTTP statuses,
  `cleanName`, the per-card hourly rate count, and the `audited` wrapper that writes `ctl-*` audit lines.
- Not reused: the document tables, slugs, versions, tombstones and the `/d/` shell. A drop file is flat, binary,
  disposable and has no readers to render for. Folding it into documents would give every export a title, a version
  history and a permanent link, which is the opposite of what an export wants.

## Tools

All three go in the worker set and the full set, as `atrium_publish` does (`internal/link/ctlclass.go`). The test that
pins the sets (`ctlclass_integration_test.go`) grows by three names.

`atrium_put {path | content, name, note?, ttl_days?}`. `path` is a file in the caller's OWN card directory, read by the
room through `GET /v1/tasks/{id}/files`, which is the safepath-bounded read `readCardFile` already does. `content` is
text the agent passes. Answers `{name, size, expires}`. `name` is the file name stored on the hub. It is cleaned the way
a document name is. A name already taken is not overwritten, the answer carries the next free one (`usage.sqlite`,
`usage-2.sqlite`), so two rooms never clobber each other and the caller reads the answer, not its own guess.

`atrium_files {}` answers `[{name, size, by, card, room, at, expires, note}]`, newest first, plus `{bytes, cap}` for the
drop.

`atrium_get {name, dest?}` fetches the file into the caller's OWN card directory and answers the path it wrote, relative
to the card. See the next section for what `dest` may be.

## How a get keeps rule 8

Rule 8 says files never leave a card. A get does not break it, because it moves a file INTO a card and never out of one.
What it must not do is write anywhere but inside the caller's own card.

- The hub finds the caller's card the way publish does (`callerCard`: the `X-Atrium-Agent` name, looked up on the
  caller's room). The room's answer for that card decides the directory. Nothing in the call names another card.
- The hub sends the bytes to that room, as a multipart upload to `POST /v1/tasks/{id}/files`. The room, not the hub,
  chooses the landing spot, through `uploadTarget` and `safepath.Contained`, and resolves it before creating anything.
  An archived card or one with no directory is refused there already.
- Phase 1 lands every get in `.atrium/incoming/`, the folder uploads use, with the name sanitised on the room. That
  needs no new room code at all.
- `dest` arrives in phase 4. It is a path RELATIVE to the card's directory. A new room endpoint, `POST
  /v1/tasks/{id}/files/put?path=`, resolves it with `safepath.Contained` (symlinks resolved on both sides, separator
  boundary, case folding on Windows), refuses an absolute path, `..`, and a path through a link that leaves the card,
  and refuses to overwrite a file that exists unless the call says `replace: true`. The hub only ever passes `dest`
  through. It never joins it to a directory itself.
- The name check runs on the way in too: a file called `.env` or `id_rsa` is not written as such. It lands under a
  neutral name and the answer says so, since nobody should have `.env` appear in a repository by accident.

## Limits and expiry

| Limit | Default | Why |
| --- | --- | --- |
| one file | 32 MiB | the card upload handler's own per-file limit (`maxUploadFile`). A get larger than that could not land |
| the whole drop | 512 MiB | a drop is scratch space. The document store's 2 GiB is for things people keep |
| puts per card per hour | 30 | the number publish uses. Gets 120 an hour |
| lifetime | 7 days | a file expires, it is not archived. `ttl_days` may shorten it or lengthen it up to 30 |

All four are hub settings the operator can change, in the same settings shape documents use (`DocSettings`), with the
same rule that a bad value reads as the default so a bad write cannot turn the drop off. A put over a cap is a 413, a
put into a full drop is a 507, a put over the rate is a 429. These are tool errors the agent reads and no more.

Expiry is a sweep on the hub's existing ticker, once an hour. It deletes the rows past `expires` and then the blobs
nobody names. A file past its time that has not been swept yet is already refused by get, so the sweep is housekeeping
and not what enforces the date. A delete from the board is the same operation. A deleted file is gone, with no tombstone
and no restore, because a drop is not where anything is kept.

## Audit trail

Every put, get and delete writes an audit line through the existing `audited` wrapper: `ctl-put`, `ctl-get`,
`ctl-drop-delete`, with the room, the card, the name and the size. A refusal is audited too, with the rule that refused
it. The row also keeps who put the file and when, so the board shows it after the audit line has scrolled. The audit
line outlives the file.

## What the board shows

A Files page, next to Documents. It reads `/_hub/drop`, shaped like `/_hub/docs`:

| Method and path | Answer |
| --- | --- |
| `GET /_hub/drop` | `{"files": [...], "usage": {"bytes": N, "cap": N}}`, newest first |
| `GET /_hub/drop/<name>/raw` | the bytes, ALWAYS `application/octet-stream`, `nosniff`, `Content-Disposition: attachment`, as documents are |
| `POST /_hub/drop/<name>/delete` | `{"ok": true}` |
| `GET` and `PUT /_hub/drop/settings` | the caps and the lifetime. A change is operator only (`edge.LocalOperator`) |

The page is a table: name, size, put by (card and room), when, expires in, with a download link and a delete button per
row, and a usage bar. A file within a day of expiry is marked. Nothing on it is rendered. A file is a download and never
a preview, so a hostile file cannot run in the board's origin.

## Security

Who can read what. The board has no login and sits on loopback, reachable by whoever can reach it, and a share widens
that. Anything in the drop is readable by anybody who can reach the board, exactly as a hub document is. A card's
identity on the control server is a header any process on loopback can set (the note in `docs_mcp.go` says so), so the
drop cannot offer a real "only this card" lock, and it does not pretend to.

Path rules. The hub reads and writes no path of its own. A put reads through the room's safepath-bounded endpoint, and
the room's 403 for anything outside the card, links included, comes back as a refusal. A get writes only through the
room. The name check runs on the RESOLVED target of a put, with the room's `X-Atrium-Real-Path`, so `notes.md -> .env`
is refused as `.env`. A room too old to send that header cannot put by path and can still put by content.

Secret checks. `SecretFileName` on the asked and the resolved name, and `SecretContent` on every byte of every way in.
The same speed bump as publish and no more: it catches a PEM block, a GitHub or Slack token, an AWS key, a JWT and a
zrok token. It cannot see a secret in a binary format. A sqlite file is the usual drop, and it is binary, so the content
check is weakest exactly where the drop is used most. That is why the tool text says to export what is needed and not
the raw file, and why open question 3 is asked.

Size. Bounded per file and in total, in memory once per request as publish is (32 MiB, so one request cannot hold the
hub). A put that fails part way writes nothing a row can name, because the row is written after the blob is renamed into
place.

The origin is decided by the hub from the request. A `card` origin means a session on a machine that said it was that
card, never the operator. A board delete is `local` or `share`, by the rule documents use.

## Resilience

The drop is never state a room depends on.

- No room startup, hook, permission check or message delivery reads it. A room with the hub gone, the drop full or the
  drop broken behaves as it does today.
- Every failure is a tool result. A put or get that cannot finish answers a sentence and the agent carries on. Nothing
  here runs inside a hook, so rule 2 (hooks never fail a session) is not touched.
- The drop can lose files at any time: expiry, an operator delete, a restored older hub store. Nothing may treat a file
  as stored for good. A job that needs a file for a week puts it again.
- The drop table lives in `hub.db`, so the usual hub rule holds: a store failure stops the hub. A full disk on the blob
  directory is not that. It is a 507 on the put and nothing else.
- Hub backups cover the table. The blobs are NOT copied the way `CopyDocs` copies document blobs, because a drop file is
  disposable. A restore brings back rows whose bytes are gone, and get answers 410 with "the bytes are missing", the
  same words documents use.

## Decisions, each with a recommendation

1. Visible to every card, or only to those the putter names. **Recommend: visible to every card and to the board, with a
   free `note` saying who it is for.** A named-recipient list would look like access control and is not one, since a
   card name is a header anything on loopback can set. Better to promise less and say so. If a file must be private it
   should not go in the drop.
2. The export: a room-side tool, or a hub fan-out. **Recommend: a room-side command, no fan-out.** The hub holds no work
   and no authority over a room's data (`hub-and-rooms.md`), and a fan-out would need a new verb on every room that
   answers a request nobody asked that room to serve. Instead a card on the room with the data runs `atrium export usage
   --since X --out file` (phase 5), which writes a consistent copy (for sqlite, `VACUUM INTO`, so no live file is copied
   mid-write), and then calls `atrium_put`. The asker reaches that card with `atrium_say` to `name@room`, as it does for
   anything else. An `atrium_export` MCP tool is not recommended, because the MCP lives on the hub and the data is on
   the room, so it would be the fan-out by another name.
3. Is this the same thing as f-spike-no-shared-workspace and f-hub-blob-storage. **I could not read them.** The backlog
   tool is not offered to this session, and a search of `docs/` for both names finds nothing. By their names, a shared
   workspace is a place several cards work in, and blob storage is a general hub store. The drop is neither. It is a
   small expiring hand-off with names and an audit trail, and it should be built so it could sit on a blob store later:
   the table holds a `sha`, and the bytes are reached only through `writeBlob`, `blobPath` and a `DropOpen`. If
   f-hub-blob-storage lands first, phase 1 uses it instead of a `drop/` directory and nothing above that changes. The
   orchestrator should compare the two items before phase 1 starts.
4. Names. **Recommend: server-chosen on a clash, never an overwrite.** Replacing a file another room is about to fetch
   is the one surprise worth ruling out.
5. Delete from the board. **Recommend: any board user may delete, as with documents,** since the files expire and the
   audit line records it. Settings and caps stay operator only.
6. Expiry. **Recommend 7 days default, 30 at most, sweep hourly,** and an expired file refused by get even before the
   sweep.

## Open questions for clint

1. Everything in the drop is readable by anyone who can reach the board and by every card on every room. Is that right,
   knowing a share widens the board's reach? The alternative is no named-recipient setting at all and a rule that a
   private file is not put here. (Recommend: yes, readable by all.)
2. Raw sqlite files from a room hold whatever the sessions held, which may be prompts and file text. The content check
   cannot read a binary database. Should the first export be aggregates only (usage counts and task rows, no message
   text), with a raw database allowed only when the putter says so? (Recommend: aggregates by default.)
3. Are 32 MiB a file, 512 MiB in all and 7 days the right defaults? A raw session database could be larger than 32 MiB.
4. Should the board let a person upload to the drop, or only list, download and delete? Upload is cheap to add and it is
   a second way in for the hub's own disk. (Recommend: not in the first cut.)

## Phased build

Each phase is a backlog item and each ends with tests passing and the earlier phases working on their own.

1. **drop store (fabric).** `drop_files` table and migration, `DropAdd`, `DropList`, `DropOpen`, `DropDelete`, the
   settings and caps, the hourly sweep, secret checks wired in, 413 and 507 and 429. Integration tests beside
   `docs_integration_test.go`. No tools yet.
2. **the three tools (fabric).** `atrium_put`, `atrium_files`, `atrium_get` in the control MCP, in the worker and full
   sets, audited. A put by path through `readCardFile`. A get through the room's existing upload, landing in
   `.atrium/incoming/`. Update `ctlclass_integration_test.go` and `website/docs/control-mcp.md`.
3. **the Files page (ui).** `/_hub/drop` routes and the board view: list, download, delete, usage bar, the settings
   gear. Follows the Documents page's patterns.
4. **`dest` on a get (runtime).** `POST /v1/tasks/{id}/files/put` on the room with safepath containment and
   refuse-overwrite, then `dest` and `replace` on `atrium_get`. Tests for `..`, absolute paths, a link out of the card,
   and a case-folded path on Windows.
5. **`atrium export` (runtime).** `atrium export usage --since X` on the room: a consistent copy, aggregates by default,
   written to a file a card can then put. The usage worker's case, end to end, as the acceptance test.
6. **larger files and streaming (fabric).** Only if clint raises the caps in question 3: stream a put and a get rather
   than hold the file in memory, and add `atrium drop ls|put|get|rm` for the operator's shell.
