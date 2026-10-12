# A hub file drop: a card on any room hands files to a card on any other

Status: design, 2026-10-12. Nothing here is built. Written for f-hub-file-drop (high). It replaces the 2026-10-10 draft
of this file, which left six questions open for clint and planned six phases. Every question is decided below, with
its reason, and the build is four phases a worker can follow without a judgment call.

## The problem

On 2026-10-06 a usage worker on m1mini needed `session_usage` and task data from claude-sg4, sg4-control and sg3.
m1mini cannot ssh to those machines. The orchestrator copied raw sqlite files by scp through sg4, by hand. clint: "this
is why a hub filesystem is useful so rooms can push to the hub... i thought we had that feature but i guess not?"

Every room already dials the hub, so the hub is the one place every room reaches. What is missing is a place on the hub
where a card can leave a file, a way for a card on another room to take it into its own directory, and a way to pull a
room's numbers without a person or a card on that room.

## What exists, and why it is not this

- `atrium_publish` (`internal/link/docs_mcp.go`, `internal/hubstore/docs.go`) puts a document at `/d/<slug>`, 5 MiB
  of text or 20 MiB of anything else, 30 an hour per card. It is versioned, permanent and rendered for readers, and no
  tool fetches one back into a card.
- Card upload and download (`internal/api/files.go`) move bytes between the board and one card on one room. Upload
  also follows `paste_keep`, and in `scrap` mode it lands in the scrap dir, which is outside the card.
- Hub git carries code between rooms by branch. A sqlite file or a capture is not a commit.
- `GET /v1/usage*` on each room feeds the board's usage charts. It answers buckets and summaries, not rows, and nothing
  stores the answer.

The parts a drop needs all exist. A content-addressed blob writer (`writeBlob`, `hexSHA`), caps that read a bad value
as the default (`DocSettings`), the secret checks (`SecretFileName`, `SecretContent`), the refusal kinds and their
statuses (`DocError`, `docStatus`), a safepath-bounded read of a card file through its room (`readCardFile`), the
caller's card (`callerCard`), the audit wrapper (`audited`), and the hub reaching any room over the link by setting
`X-Atrium-Room` on a request to its own board (`controlMCP.ask`). The drop joins them and adds one write endpoint on
the room.

## Decisions

Each is decided here, because clint is away and the brief says to decide.

1. **Every drop file is visible to every card and to the board.** No recipient list. A card's identity on the control
   server is `X-Atrium-Agent`, a header anything on loopback can set (`ctlclass.go` says so), so "only card X may get
   this" would look like access control and would not be one. A free `note` says who the file is for. The tool text
   says plainly that anything put here is readable by everyone past the board's password, and that a private file does
   not belong in the drop.
2. **The export is a hub fan-out, as one tool, `atrium_export`.** The 2026-10-06 case had no card on sg3 to ask, and
   launching a card on every room to run one query is the slow path the drop is meant to remove. The hub already reads
   every room's `/v1/usage*` and `/v1/tasks` for the board, through the same proxy and link, so the hub gains no new
   reach over a room's data. One new read-only room route, `GET /v1/usage/export`, answers rows instead of buckets. The
   hub writes each room's answer into the drop and answers the names. It is a read the caller is waiting for, not work
   the hub holds, so it keeps the rule that the hub holds no work (`hub-and-rooms.md`). A room that is offline or too
   old is listed as skipped, with the reason, and the rest still land.
3. **Exports are rows, never a raw database.** `session_usage` rows carry counts, models, causes and prices, no prompt
   text. A raw sqlite file can hold anything a session held, and the content check cannot read a binary database. A
   card that has a file it must hand over still can, with `atrium_put` by path, and the secret checks run on it as they
   do on publish.
4. **One store with the blob-storage design, built drop-first.** The table is `hub_files` with a `class` column and the
   blobs live in `<hub dir>/files/`, as `docs/rnd/hub-blob-storage.md` asks, so f-hub-blob-storage later adds the
   `artifact` class without a migration that renames anything. These four phases build and allow only `class = 'drop'`.
5. **A get writes only inside the getter's own card, through a new room endpoint, never through upload.** Upload
   follows `paste_keep`, and in `scrap` mode it writes to `api.ScrapDir`, outside the card. The new
   `PUT /v1/tasks/{id}/files/drop` always resolves against the card's own directory with `safepath.Contained`, before
   and after creating any directory, refuses an absolute `dest` and any `..` segment, and refuses to overwrite unless
   asked. The hub never joins a path. Rule 8 (files never leave a card) is untouched: a get moves bytes into a card,
   and the only read out of a card is the existing safepath-bounded `GET /v1/tasks/{id}/files` that publish already
   uses.
6. **Names are chosen by the hub on a clash, never overwritten.** `usage.jsonl` taken gives `usage-2.jsonl`, and the
   answer carries the real name. Replacing a file another room is about to fetch is the one surprise worth ruling out.
7. **Limits.** 32 MiB a file, because the room's own upload limit (`maxUploadFile`) is 32 MiB and the whole file is held
   in memory once per request, as publish does. 512 MiB for the drop in all. 30 puts an hour per card, the publish
   number. Lifetime 7 days, and `ttl_days` may set 1 to 30. All are hub settings in the `DocSettings` shape, and a bad
   value reads as the default. No rate on gets: a get only writes into the caller's own card, bounded by the file cap.
8. **No backup of drop blobs.** A drop file is disposable. The ten-minute snapshots cover the rows, `CopyDocs` is not
   extended, and a restored hub answers 410 "the bytes are missing" for a row whose blob is gone, the words documents
   use.
9. **The board may list, download, delete and upload.** Upload is in, because it is the half of
   f-spike-no-shared-workspace the drop can serve today (see the next section). Anyone past the board's password may
   upload and delete, as with documents. The caps are changed by the operator only (`edge.LocalOperator`).
10. **A file is always a download, never a preview.** `application/octet-stream`, `nosniff`, `attachment`, as
    `docsRaw` does, so a hostile HTML file cannot run in the board's origin.

## Relation to f-spike-no-shared-workspace

That spike wants clint and claude to stop sharing a checkout on D:\ and both go through the hub. Code goes through hub
git, which exists. What a shared worktree also carries is everything that is not code: a log clint wants to look at, a
screenshot clint hands to a card, a dump a card leaves for clint. The drop is that channel. With it, clint takes a file
from a card by asking the card to `atrium_put` it and downloading it from the Files page, and hands one to a card by
uploading it on the Files page and telling the card its name, which the card fetches with `atrium_get`. No path on
either machine is shared. The spike decides whether clint also wants a shell command for this, and the HTTP routes
below are what such a command would call. The drop does not decide anything the spike owns.

## The wire

### Where bytes go

```
 card A on m1mini                       HUB                                   card B on sg4
 +--------------+  atrium_put {path}  +----------------------+  atrium_get  +------------------+
 | out.jsonl    |-------------------->| hub_files (rows)     |------------->| .atrium/incoming |
 | (own dir)    |  hub asks room A:   | <hub dir>/files/<sha>|  hub asks    |   or dest, inside|
 +--------------+  GET /v1/tasks/{id} | hourly sweep         |  room B:     |   the card       |
                   /files?path=       +----------------------+  PUT /v1/    +------------------+
                                        ^            |          tasks/{id}/files/drop
         atrium_export: GET /v1/usage/  |            | board: /_hub/files list, raw,
         export, GET /v1/tasks on each  |            | delete, upload, settings
         room, stored as drop files ----+            v
```

Every arrow from the hub to a room is a request to the hub's own board with `X-Atrium-Room` set, which the proxy
already carries over the room's link. No new listener, port or link kind. A session on a room reaches the tools through
the control MCP at `/_hub/mcp`, as it reaches `atrium_publish`.

### Hub store

Migration `0017_hub_files`, appended at the end of `migrations` in `internal/hubstore/schema.go`:

```sql
CREATE TABLE IF NOT EXISTS hub_files (
    name      TEXT PRIMARY KEY,
    class     TEXT NOT NULL DEFAULT 'drop' CHECK (class IN ('drop','artifact')),
    size      INTEGER NOT NULL,
    sha       TEXT NOT NULL,
    origin    TEXT NOT NULL CHECK (origin IN ('local','share','card')),
    by        TEXT NOT NULL DEFAULT '',
    card      TEXT NOT NULL DEFAULT '',
    room      TEXT NOT NULL DEFAULT '',
    note      TEXT NOT NULL DEFAULT '',
    at        TEXT NOT NULL,
    expires   TEXT NOT NULL,
    downloads INTEGER NOT NULL DEFAULT 0,
    last_get  TEXT NOT NULL DEFAULT ''
)
CREATE INDEX IF NOT EXISTS hub_files_sha ON hub_files (sha)
CREATE INDEX IF NOT EXISTS hub_files_card ON hub_files (card, at)
CREATE INDEX IF NOT EXISTS hub_files_expires ON hub_files (expires)
```

(Four statements in the migration's `stmts` slice, one string each.) `card` is `room~id`, the form publish records.
`room` is the room name, so a list can say where a file came from after the card is gone. Bytes are
`<hub dir>/files/<sha256>`, written through a temp file and a rename. Equal bytes share one blob. A blob is removed only
when no row names it.

The setting is `files_settings` in `hub_setting`, JSON:

```json
{"caps": {"file": 33554432, "total": 536870912, "per_card_hour": 30, "ttl_days": 7, "max_ttl_days": 30}}
```

### MCP tools (hub control MCP, worker set and full set)

```
atrium_put    {path | content, name?, note?, ttl_days?}
              name is needed with content. answers {name, size, expires}
atrium_files  {mine?}
              mine lists only this card's puts. answers, newest first,
              {files: [{name, size, by, card, room, origin, at, expires, note, downloads}],
               usage: {bytes, cap}}
atrium_get    {name, dest?, replace?}
              dest is relative to your own card. answers {path, rel, size}:
              where it landed, absolute and relative to the card
atrium_export {what, since?, rooms?}
              what is usage or tasks. since is RFC 3339, default 7 days ago.
              rooms defaults to every online room.
              answers {files: [{room, name, size}], skipped: [{room, why}]}
```

Audit kinds: `ctl-put`, `ctl-get`, `ctl-export`, through `audited`, which also records refusals. Board deletes and
uploads write `file-deleted` and `file-uploaded` through `p.RecordAudit`.

Refusals reuse `hubstore.DocError` kinds, so `publishError` and `docStatus` map them unchanged: 400 bad input, 404 no
such file, 410 expired or bytes missing, 413 over the file cap, 422 a secret rule, 429 over the hourly puts, 507 the
drop is full.

### Room routes (runtime)

- `PUT /v1/tasks/{id}/files/drop?name=&dest=&replace=` takes the raw bytes, at most 32 MiB, and answers
  `{"path": "<absolute, forward slashes>", "rel": "<relative to the card>"}`. It answers 400 for a card with no
  directory or an archived one, 403 for a path outside the card, 409 for a file already there without `replace=1`,
  and 413 for too many bytes.
- `GET /v1/usage/export?since=<RFC3339>` answers `application/x-ndjson`, one `store.SessionUsage` JSON object per
  line, ordered by `ended_at`.

`GET /v1/tasks` (exists) is what `atrium_export what=tasks` stores, verbatim.

A room older than these routes answers a bare 404. The hub turns that into "room X is older than the file drop, restart
it on a current build", and `atrium_export` lists it under `skipped`.

### Hub board routes (`/_hub/files`)

| Method and path | Answer | Who |
| --- | --- | --- |
| `GET /_hub/files` | `{"files": [...], "usage": {"bytes": N, "cap": N}}` | board |
| `GET /_hub/files/<name>/raw` | the bytes, octet-stream, `nosniff`, `attachment` | board |
| `POST /_hub/files/<name>/delete` | `{"ok": true}` | board |
| `POST /_hub/files` | multipart, field `file`, optional `note`, `ttl_days`. Answers `{name, size, expires}` | board |
| `GET` and `PUT /_hub/files/settings` | the caps, and `operator` | read: board. write: `edge.LocalOperator` |

Every write checks `http.NewCrossOriginProtection` first, as `serveDocs` does. `<name>` is path-escaped by the client
and unescaped by the route.

### Expiry

`FileSweepLoop` runs once an hour on the hub, started beside `VacuumLoop`. It deletes rows whose `expires` is past,
then every blob in `files/` that no row names, then any `.tmp-*` file there older than an hour. A file past its
`expires` is already refused by `FileOpen` with 410, so the sweep is housekeeping and not what enforces the date.

## Security and resilience

- The hub reads and writes no card path of its own. A put by path goes through the room's safepath-bounded read, and
  the name check runs on the resolved target from `X-Atrium-Real-Path`, as publish does, so `notes.md -> .env` is
  refused as `.env`. A get goes through the room's new endpoint, which owns containment.
- `SecretFileName` runs on the stored name, on the resolved source path, and on `dest`. `SecretContent` runs on every
  byte of every way in, board upload and export included. It is a speed bump and cannot see a secret in a binary
  format. The tool text says so.
- The drop is never state a room depends on. No startup, hook, permission check or message delivery reads it. Every
  failure is a tool result. A full disk under `files/` is a 507 on a put and nothing else. The table lives in
  `hub.db`, so a store failure stops the hub as every hub table does.
- Nothing may treat a drop file as kept. It can vanish by expiry, a board delete, or a restored hub.

## Build plan

Four phases. Each lands alone with its tests passing, and nothing earlier breaks if a later one never lands. Phases 1
and 2 touch different packages and may run in parallel. Phase 3 needs both. Phase 4 needs phase 1.

Every new Go file follows the comment style of the file it is modelled on. No new dependency.

### Phase 1: the store (fabric, `internal/hubstore`)

1. `internal/hubstore/schema.go`: append the `0017_hub_files` entry above as the LAST element of `migrations`.
2. `internal/hubstore/store.go`: add a field `filesDir string` to `Store` and set it in `Open` next to `docsDir`
   (line 180): `filesDir: filepath.Join(filepath.Dir(path), "files")`. Add `func (s *Store) FilesDir() string`.
3. `internal/hubstore/docs.go`: change `writeBlob(sha, data)` into a call to a new
   `writeBlobIn(dir, sha string, data []byte) error` holding today's body with `s.docsDir` replaced by `dir`. Publish
   behaves the same.
4. New `internal/hubstore/files.go`, modelled on `docs.go`:
   - Constants `SettingFiles = "files_settings"`, `DefaultFileCap int64 = 32 << 20`,
     `DefaultFileTotalCap int64 = 512 << 20`, `DefaultFilePerCardHour = 30`, `DefaultFileTTLDays = 7`,
     `DefaultFileMaxTTLDays = 30`, `fileNoteMax = 500`.
   - `type FileCaps struct` with fields `File int64`, `Total int64`, `PerCardHour int`, `TTLDays int` and
     `MaxTTLDays int`, JSON names `file`, `total`, `per_card_hour`, `ttl_days`, `max_ttl_days`.
     `type FileSettings struct` with one field, `Caps FileCaps`, JSON `caps`.
   - `func (s *Store) FileSettings() (FileSettings, error)`: as `DocSettings`, every non-positive value reads as the
     default. `func (s *Store) SetFileSettings(p FileCaps) (FileSettings, error)`: a zero field is left alone, a
     negative one is `DocBad`.
   - `type FileRow struct` with the table's columns as JSON `name, class, size, sha, origin, by, card, room, note, at,
     expires, downloads, last_get`, plus `Missing bool \`json:"missing"\`` set when the blob is not on disk.
   - `type FileInput struct` with fields `Name`, `Note`, `Origin`, `By`, `Card` and `Room` (strings), `Data []byte`,
     `TTLDays int` and `Rate bool`.
   - `func (s *Store) FileAdd(in FileInput) (FileRow, error)`, the one way a row is made, in this order: origin is
     `local`, `share` or `card`, else `DocBad`. Empty data is `DocBad`. `name := cleanName(in.Name)`, empty is
     `DocBad`. `SecretFileName(name)` is `DocSecret` with `Rule: RuleFileName`. Over `Caps.File` is `DocTooBig`.
     `SecretContent(in.Data)` non-empty is `DocSecret` with that rule. `TTLDays` of 0 means `Caps.TTLDays`, above
     `Caps.MaxTTLDays` or below 0 is `DocBad`. Then under a new `s.filesMu sync.Mutex` and inside `s.tx`: when `Rate`
     and `Card` are set, count `hub_files WHERE card = ? AND at > <an hour ago>`, at the cap is `DocRate`. Sum
     `MAX(size) GROUP BY sha` over all rows, and if this sha is not already held and the sum plus this size is over
     `Caps.Total`, `DocFull`. Pick the name with a new `freeFileName(t, name)`: `name`, then `stem-2.ext`, `stem-3.ext`
     and on (split with `filepath.Ext`). `writeBlobIn(s.filesDir, sha, data)`, then the INSERT with `class 'drop'`,
     `at = ts(now())`, `expires = ts(now().Add(days * 24h))`, `note` cut to `fileNoteMax` runes.
   - `func (s *Store) FileList(card string) ([]FileRow, error)`: rows not yet expired, newest `at` first, only
     `card` when it is non-empty. `func (s *Store) FileUsage() (int64, error)`: the sum above.
   - `func (s *Store) FileOpen(name string) (*os.File, FileRow, error)`: no row is `DocNone`, `expires` past is
     `DocGone` "that file expired on <date>", a missing blob is `DocGone` "the bytes are missing". On success it runs
     `UPDATE hub_files SET downloads = downloads + 1, last_get = ? WHERE name = ?`.
   - `func (s *Store) FileDelete(name string) error`: no row is `DocNone`. Deletes the row, and under `filesMu`
     removes the blob when no other row names its sha. A failed remove is logged, the sweep retries.
   - `func (s *Store) FileSweep() (rows, blobs int, err error)` as described under Expiry, under `filesMu`.
   - `const FileSweepEvery = time.Hour` and `func (s *Store) FileSweepLoop(ctx context.Context)`, shaped like
     `VacuumLoop`, logging `[hub] file sweep: %v` on an error.
5. `internal/cli/atrium_run.go`: after `go store.VacuumLoop(ctx)` (line 519), add `go store.FileSweepLoop(ctx)`.
6. Tests, new `internal/hubstore/files_integration_test.go`, using the store opener the docs tests use and the `now`
   override as `docs_integration_test.go` line 217 does:
   - `TestFilesMigrationToleratesBeingThere`: run `migrate` twice, as `TestDocsMigrationToleratesBeingThere` does.
   - `TestFilesAddListOpenDelete`: add, list shows it, open reads the bytes and counts a download, delete removes row
     and blob.
   - `TestFilesNameClashGetsASuffix`: two adds of `usage.jsonl` give `usage.jsonl` and `usage-2.jsonl`.
   - `TestFilesSharedBlobSurvivesOneDelete`: two names, same bytes, delete one, the other still opens.
   - `TestFilesCapsAndRate`: over the file cap is 413 kind, over the total is 507 kind, the 31st put in an hour is
     429 kind, a bad setting value reads as the default.
   - `TestFilesSecretChecks`: `.env` by name and a PEM block by content are refused with their rule.
   - `TestFilesExpireAndSweep`: move `now` past `expires`, open is `DocGone`, the sweep removes row and blob and a
     stale `.tmp-` file.
   - `TestFilesTTLBounds`: `ttl_days` 0 gives 7 days, 31 is refused.

   Run: `go test ./internal/hubstore -run 'TestFiles|TestDocs' -count=1`

### Phase 2: the room side (runtime, `internal/api`, `internal/store`)

1. New `internal/api/filedrop.go`, `func (s *Server) dropIn(w http.ResponseWriter, r *http.Request)`:
   - `task, err := s.st.Get(r.PathValue("id"))`, a failure through `s.fail`.
   - `name := safepath.SafeName(r.URL.Query().Get("name"))`, `dest := strings.TrimSpace(r.URL.Query().Get("dest"))`,
     `replace := r.URL.Query().Get("replace") == "1"`.
   - With no `dest`: `dir, err := uploadTargetIn(task)` (NOT `uploadTarget`, so `paste_keep` scrap never applies), a
     failure is 400. The target is `filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+name)`.
   - With `dest`: 400 when `uploadTargetIn` fails for the same card (no directory, archived, missing). 403 with
     `safepath.ErrOutside` when `filepath.IsAbs(filepath.FromSlash(dest))` or any `/`-split or `\`-split segment is
     `..`. Then `target, err := safepath.Contained(root, dest)` with `root := filepath.FromSlash(task.Worktree)`, an
     error is 403 `safepath.ErrOutside`. `parent := filepath.Dir(target)`, check `safepath.Contained(root, parent)`
     BEFORE `os.MkdirAll(parent, 0o700)` and again after, as `uploadTargetIn` does. A target that is a directory is
     400 "that is a directory".
   - `r.Body = http.MaxBytesReader(w, r.Body, maxUploadFile)`.
   - Without `replace`: write as `saveUpload` does, `os.OpenFile(target, O_WRONLY|O_CREATE|O_EXCL, 0o600)`, an
     `os.IsExist` error is 409 "a file is already there, pass replace". With `replace`: write a temp file in `parent`
     named `.atrium-drop-<12 hex>` with `O_EXCL`, then `os.Rename(tmp, target)`. Either way a copy error removes what
     was written, and a `*http.MaxBytesError` is 413. An empty body is 400 and writes nothing.
   - `s.st.AppendEvent(task.ID, store.EventNotified, map[string]any{"by": "drop", "files": []string{path}})`.
   - Answer `writeJSON(w, 200, map[string]any{"path": filepath.ToSlash(target), "rel": rel})`, `rel` from
     `realRelative(task.Worktree, target)`.
2. `internal/api/api.go`: register `mux.HandleFunc("PUT /v1/tasks/{id}/files/drop", s.dropIn)` beside the other
   `/files` routes (line 563).
3. `internal/store/usage.go`: add `func (s *Store) SessionUsageSince(since time.Time, each func(*SessionUsage) error)
   error`, the same column list and `scanUsage` as `SessionUsageOf`, `WHERE ended_at >= ? ORDER BY ended_at`, with
   `since` formatted the way that table's times are written.
4. New `internal/api/usageexport.go`, `func (s *Server) usageExport(w, r)`: `since` parsed as in `roomUsage`, default
   seven days ago. Set `Content-Type: application/x-ndjson`, then `json.NewEncoder(w).Encode(u)` for each row from
   `SessionUsageSince`. An error before the first row goes through `s.fail`, one after is logged and ends the stream.
   Register `mux.HandleFunc("GET /v1/usage/export", s.usageExport)` beside `GET /v1/usage` (line 817), inside the
   same condition that registers that route.
5. Tests:
   - New `internal/api/filedrop_integration_test.go`, using `fileServer` and `cardIn` from `files_integration_test.go`:
     `TestDropInLandsInIncoming`, `TestDropInIgnoresScrapMode` (set `paste_keep` to `scrap` and a `ScrapDir`, the file
     still lands under the card), `TestDropInDestInsideTheCard` (`out/data.jsonl` lands, parents made),
     `TestDropInRefusesAbsoluteAndDotDot`, `TestDropInRefusesALinkOutOfTheCard` (a symlink or junction `out -> <temp
     dir outside>`, then `dest=out/x`, 403, nothing written outside, skip when the OS refuses to make the link),
     `TestDropInRefusesOverwriteWithoutReplace` (409, then `replace=1` replaces), `TestDropInRefusesArchivedAndNoDir`,
     `TestDropInOverTheCapIs413`, and on Windows only `TestDropInCaseFoldedEscapeIsRefused`.
   - New `internal/api/usageexport_integration_test.go`: `TestUsageExportIsOneRowPerLine` (three rows, two after
     `since`, two lines come back in order), `TestUsageExportBadSinceIs400`.
   - `internal/store`: `TestSessionUsageSince` in the usage test file.

   Run: `go test ./internal/api -run 'TestDropIn|TestUsageExport|TestAnUpload' -count=1` and
   `go test ./internal/store -run 'TestSessionUsageSince' -count=1`

### Phase 3: the four tools (fabric, `internal/link`)

1. `internal/link/docs_mcp.go`: split `readCardFile` so its body takes a client and a timeout:
   `readCardFileWith(ctx, client *http.Client, timeout time.Duration, room string, card *ctlCard, p string, limit
   int64)`. `readCardFile` calls it with `c.client, controlTimeout`, so publish is unchanged.
2. New `internal/link/files_mcp.go`:
   - `const dropTimeout = 2 * time.Minute` and `var dropHTTP = &http.Client{}` (no client timeout, the context bounds
     it), because `c.client` cuts every request at 8 seconds and 32 MiB over a link can take longer.
   - Input and output structs for the four tools, in the table above, with `jsonschema` tags as `publishInput` has.
   - Tool texts as constants, each saying: visible to every card and to anyone past the board's password, expires
     after 7 days by default, 32 MiB a file, the secret checks are a speed bump, a get lands only in your own card.
     `atrium_export` adds that it answers rows of numbers and never a raw database.
   - `func (c *controlMCP) registerFiles(s *mcp.Server, class ctlClass)` adding the four tools with `addTool`, each
     wrapped in `audited(c, "ctl-put" | "ctl-get" | "ctl-export", describeX, handler)`. `atrium_files` is not audited,
     as reads are not.
   - `putHandler`: the store from `c.docs()` (the same `*hubstore.Store`), the `content` or `path` checks exactly as
     `publishHandler` writes them, `agentOf`, `roomOf`, `callerCard`. By path: `SecretFileName(in.Path)` first, then
     `readCardFileWith(ctx, dropHTTP, dropTimeout, room, card, path, caps.File)`, then `SecretFileName(rel)` with the
     same link message publish gives. `name` defaults to `path.Base(rel)`. Then `st.FileAdd` with `Origin: "card"`,
     `By: callerName(req)`, `Card: tagFor(room, card.ID)`, `Room: room`, `Rate: true`. Errors through `publishError`.
   - `filesHandler`: `st.FileList(card)` where `card` is the caller's tag when `mine`, else empty, plus `FileUsage` and
     the total cap.
   - `getHandler`: `callerCard`, `card.Worktree` empty is the same 400 publish gives. `SecretFileName(in.Dest)` when
     `dest` is set, a 422. `f, row, err := st.FileOpen(in.Name)`, read it whole (it is at most the file cap), then
     `PUT c.board + "/v1/tasks/" + url.PathEscape(card.ID) + "/files/drop?name=" + url.QueryEscape(row.Name) +
     "&dest=" + url.QueryEscape(in.Dest)` and `&replace=1` when asked, with `RoomHeader` set and `dropHTTP` under
     `dropTimeout`. Map 403, 404, 409, 413 and a bare 404 (older room) to `docsRefusal` sentences, as `readCardFile`
     does. Answer `{path, rel, size}`.
   - `exportHandler`: `what` must be `usage` or `tasks`. `since` defaults to `now - 7 days`, else `time.Parse(time.
     RFC3339)`, a bad one is 400. Rooms are `in.Rooms`, or every `c.rooms()` entry with `Online`. For each room,
     one at a time: an offline or unknown room goes to `skipped`. `GET /v1/usage/export?since=` or `GET /v1/tasks`
     with `RoomHeader`, through `dropHTTP` under `dropTimeout`, read through `io.LimitReader(body, caps.File+1)`, over
     the cap is skipped "over the file cap, narrow since". A bare 404 is skipped "room is older than the file drop".
     The bytes go to `st.FileAdd` with name `usage-<room>-<YYYYMMDD-HHMM>.jsonl` or `tasks-<room>-<YYYYMMDD-HHMM>.json`
     in UTC, `Note: "export of <what> from <room> since <since>"`, origin `card` and the caller's card, `Rate: false`
     (one call is one decision, and the cap on the drop's total still holds). A refusal from `FileAdd` is skipped
     with its sentence.
3. `internal/link/control_mcp.go`: call `c.registerFiles(s, class)` after `c.registerDocs(s, class)` (line 434).
4. `internal/link/ctlclass.go`: add `atrium_put`, `atrium_get`, `atrium_files` and `atrium_export` to `workerTools`,
   with one comment line: workers hand files between rooms, see files_mcp.go.
5. `internal/link/ctlclass_integration_test.go`: add the four names to `workerSeven` and `fullFifteen`, sorted as
   those lists are.
6. `website/docs/control-mcp.md`: one entry per tool, in the style of the `atrium_publish` entry.
7. Tests, new `internal/link/files_mcp_integration_test.go`, modelled on `newPublishHarness` in
   `docs_mcp_integration_test.go`, with a fake room board that also answers `PUT .../files/drop`, `GET
   /v1/usage/export` and `GET /v1/tasks`:
   - `TestFilesToolPutByContentAndList`, `TestFilesToolPutByPathReadsThroughTheRoom`,
     `TestFilesToolPutRefusesASecretNameOnTheResolvedTarget`, `TestFilesToolPutOutsideTheCardIs403`.
   - `TestFilesToolGetSendsToTheCallersOwnCard`: the fake room sees the caller's card id and the name, and nothing in
     the input can name another card.
   - `TestFilesToolGetPassesDestAndReplace`, `TestFilesToolGetFromAnOlderRoomSaysSo`,
     `TestFilesToolGetExpiredIs410`.
   - `TestFilesToolExportStoresOneFilePerRoom` (two online rooms, one offline: two files, one skipped),
     `TestFilesToolExportBadWhatIs400`.
   - `TestFilesToolsAreAudited`: `ctl-put`, `ctl-get`, `ctl-export` lines appear, and a refused put writes one too.

   Run: `go test ./internal/link -run 'TestFilesTool|TestPublish|TestWorkerSees|TestDirectorUntagged|TestUnknownAgent'
   -count=1`

### Phase 4: the Files page (ui, `internal/link` and `internal/api/web`)

1. New `internal/link/files_api.go`, modelled on `docs_api.go`, `func (p *Proxy) serveFiles(w, r, sub string)`
   answering the `/_hub/files` table above, using `p.docStore()` for the store (404 when nil), a package
   `filesCrossOrigin = http.NewCrossOriginProtection()` checked first on every write, `docFail` for refusals, and
   `docOrigin(r)` and `docBy(r)` for an upload's origin and by. Upload reads multipart with
   `http.MaxBytesReader(w, r.Body, caps.File+docFormOverhead)`, field `file`, and calls `FileAdd` with `Rate: false`.
   Raw sets the same headers `docsRaw` sets. Delete calls `FileDelete` and `p.RecordAudit("", "file-deleted",
   name+" by "+docBy(r))`. Upload records `file-uploaded`. Settings `PUT` is refused through `docOperatorOnly`.
2. `internal/link/proxy.go` `serveHubAPI`: route `sub == "files" || strings.HasPrefix(sub, "files/")` to
   `p.serveFiles(w, r, sub)`, beside the docs route (line 1540).
3. New `internal/api/web/m/js/files.js`, modelled on `changereq.js` (a list view with an `openList`), exposing
   `window.mFiles = { init, openList }`. It fetches `/_hub/files`, shows a table (name, size, put by as `by@room`,
   when, expires in, note), marks a row expiring within a day, a download link per row to `raw`, a delete button that
   asks in the page before it posts (the viewer has no `confirm()`), an upload input with an optional note, and a
   usage bar. Nothing is previewed. A 404 from `/_hub/files` hides the button, as documents do on a hub without them.
4. New `internal/api/web/m/files.css`, and in BOTH `internal/api/web/index.html` and `internal/api/web/m/index.html`:
   the stylesheet link beside `docs.css`, the script tag beside `docs.js`, and in `m/index.html` a button
   `<button type="button" id="m-files-btn" class="docsbtn" hidden>files</button>` after `m-req-btn`, plus
   `window.mFiles.init()` with the other `init` calls.
5. `scripts/test-board-headless.js`: answer `/_hub/files` from the mock as `/_hub/docs` is answered (line 931), and
   add one check that the Files list paints two mock rows and that the download link ends in `/raw`.
6. Tests, new `internal/link/files_api_integration_test.go`, modelled on `newDocsHub` in
   `docs_api_integration_test.go`: `TestFilesAPIListUploadRawDelete`, `TestFilesAPIRawIsAlwaysAnAttachment` (an
   HTML file comes back `application/octet-stream`, `nosniff`, `attachment`), `TestFilesAPIRefusesAForeignOrigin` on
   upload, delete and settings, `TestFilesAPISettingsAreOperatorOnly`, `TestFilesAPIUploadRefusesASecret`.

   Run: `go test ./internal/link -run 'TestFilesAPI|TestDocsAPI' -count=1` and, where node is installed,
   `node scripts/test-board-headless.js`

## What this leaves for later

- The `artifact` class, large files with ranged pulls, and the per-card inventory are f-hub-blob-storage. The table
  and blob directory are already the ones that design names.
- A shell command for clint (`atrium files get|put`) belongs to f-spike-no-shared-workspace, which can call the board
  routes above.
- Streaming instead of holding a file in memory, if the 32 MiB cap is ever raised.
