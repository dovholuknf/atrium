# Hub blob storage: durable artifacts, and one store with the file drop

Status: design draft, 2026-10-10. Nothing here is built. It changes no code. Written for f-hub-blob-storage.

## The problem

Clint, 2026-10-09: durable text artifacts go in the room's git checkout, committed and pushed to the hub. "If there are
large BINARY files we just need 'hub storage' for something like that right?" A build output, a capture, a recording or
a dump is too big and too binary for git, and today it lives only on the disk of the room that made it. If that machine
is wiped, offline or closed, the artifact is gone, and nobody can say which card made it.

Two things are missing. A place on the hub that keeps a large file and names the card and room it came from. And one
list per card and per room of every durable thing the card produced, commits and blobs together.

## What exists today

`atrium_publish` stores a file on the hub as `/d/<slug>` (`internal/link/docs_mcp.go`, `internal/hubstore/docs.go`). It
is versioned, rendered for readers, 5 MiB of text and 20 MiB of anything else (`DefaultDocTextCap`,
`DefaultDocOtherCap`), 30 an hour per card, with blobs content-addressed under the hub's docs dir (`blobPath`,
`writeBlob`, `hexSHA`) and checked by `SecretFileName` and `SecretContent` (`docs_secrets.go`). Nothing larger fits,
and no tool lets a card fetch one back. Git pushes are already logged with room and card in the hub's `git_push` table
(`internal/hubstore/schema.go`, `gitpush.go`). A card's inventory of what the ROOM made for it exists in the room's
store: `card_resources` (`internal/store/resources.go`, read and measured in `internal/api/resources.go`), one row per
worktree, branch, ref, overlay or stash, and `ResStash` is already the kind for work held on the hub instead of freed.
A card's files never leave the card (resilience rule 8), and every path a room reads or writes is bounded by
`internal/safepath`. The file-drop draft, `docs/rnd/hub-file-drop.md` (branch `hub/claude/file-drop-design`), adds
`atrium_put`, `atrium_files`, `atrium_get`, a `drop_files` table and 7-day expiry. It overlaps this item almost
entirely.

## The recommendation in one paragraph

ONE store, called the hub file store, with ONE table, ONE blob directory and ONE set of three tools. A file has a
`class`, either `drop` (short-lived hand-off between rooms) or `artifact` (kept for what the card produced). The class
decides limits and retention and nothing else. A worker calls the same `atrium_put` for both and says `keep: true` for
an artifact. The hub inventory of a card is a read-only join of that table and the `git_push` log, so no second
inventory is invented. The room's own `card_resources` gets one small row per upload so the close flow can say "this is
safe on the hub".

## The shared store

### Table and blobs

A table in `hub.db`, named `hub_files` (the file-drop draft's `drop_files`, renamed and widened):

| Column | Meaning |
| --- | --- |
| `id` | row id, so a name can repeat across versions of a clash and a delete is exact |
| `name` | file name on the hub, cleaned as `cleanName` does, server-chosen on a clash, never an overwrite |
| `class` | `drop` or `artifact` |
| `size`, `sha` | bytes and sha256 of the bytes, the blob key |
| `mime` | sniffed type, shown only, never trusted for serving |
| `by`, `card`, `room` | putter handle, `room~id`, room name. These are the card and room the file came from |
| `repo`, `ref`, `commit` | optional: the branch and commit the card was on, so a build output points at its source |
| `note` | free text, the putter's hint |
| `at`, `expires` | put time and expiry. `expires` is empty for an artifact whose card is open or pinned |
| `pinned` | 1 when an operator asked for it to stay |
| `downloads`, `last_get` | counts for the board |

Bytes are `<hub dir>/files/<sha256>`, one directory for both classes, written to a temp file in the same directory and
renamed, as `writeBlob` does. Equal bytes share one blob. A blob is removed only when no row of ANY class names it. A
drop and an artifact of the same bytes therefore cost the disk once, and promoting a drop to an artifact is a row
update, not a copy.

Hub documents keep their own `docs` dir and tables. Folding documents in is not part of this design (see rejected
alternatives). They share the helpers (`hexSHA`, the secret checks, `DocError` kinds), not the rows.

### What differs between the two classes

| | `drop` | `artifact` |
| --- | --- | --- |
| Purpose | hand a file to another room | keep what a card produced |
| One file | 32 MiB (phase 2), raised with streaming | 1 GiB |
| Class total | 512 MiB | 20 GiB, and 4 GiB per card |
| Lifetime | 7 days, `ttl_days` up to 30 | no date while the card is open, then 30 days after it closes, unless pinned |
| Counts in inventory | listed, marked short-lived | listed, the card's record |
| Board delete | any board user | any board user, with a confirm that names the card |
| Rate | 30 puts an hour per card | 10 puts an hour per card |

All limits are hub settings in the shape `DocSettings` uses, and a bad value reads as the default so a bad write cannot
turn the store off. Caps are enforced per class, so a full drop never blocks an artifact and the reverse. A put over a
cap is a 413, a full class is a 507, over the rate is a 429. They are tool errors the agent reads.

### Tools a worker sees

Three tools, in the worker set and the full set as `atrium_publish` is (`internal/link/ctlclass.go`). The test that
pins the sets grows by three names, as the file-drop draft already says.

- `atrium_put {path | content, name, note?, keep?, ttl_days?}`. `path` is a file in the caller's OWN card directory.
  `keep: true` makes it an `artifact`, and `ttl_days` applies to a drop only. The answer is `{name, size, class,
  expires}`. A worker never chooses between two tools, only says whether it wants the file kept.
- `atrium_files {scope?, class?}`. `scope` is `mine` (default, this card), `room` or `all`. It answers the rows and the
  class usage. With `mine` it also answers the card's pushed commits, so one call is the card's durable inventory.
- `atrium_get {name, dest?}`. Lands in the caller's own card as the file-drop draft says, and keeps rule 8, because a
  get moves a file INTO a card and never out of one.

`atrium_publish` is unchanged. It is for people to read a document at `/d/<slug>`. A big binary is not published, it is
put.

### How a large file gets from a room to the hub

The hub never touches a room's disk, so the hub pulls through the room's safepath-bounded read (`readCardFile`,
`internal/api/files.go`) as the draft says. The draft reads a whole file into memory, which is fine at 32 MiB and not at
1 GiB. For an artifact the hub pulls in ranges of 8 MiB into a temp file in `files/`, hashing as it goes, and renames it
into place only when the length and the hash agree. A pull that stops leaves a temp file the sweep removes, and no row.
The room endpoint must answer byte ranges. Whether `downloadFile` already does (it may serve through
`http.ServeContent`) is the first thing phase 2 checks, and if not, adding it is part of that phase. Secret checks run
over the stream as it passes (below).

### Secret checks

`SecretFileName` on the asked name and on the resolved target, the room's `X-Atrium-Real-Path`, so a link named
`notes.md` that points at `.env` is refused. `SecretContent` over the whole file for text types up to the 5 MiB text
cap. For a binary it scans the first 4 MiB and the last 4 MiB, plus a strings pass over what it can see, because a
secret in a binary format cannot be found reliably and a full scan of 1 GiB per put is not free. The board says plainly
when a file was only partly scanned. The tool text says to put outputs and not working directories. This is the same
speed bump publish gives and no more.

### Where nothing valuable lives only on one disk

The inventory is the answer.

```
 card fix-login on room m1mini
   commits   repo atrium   fix-login   a1b2c3 -> d4e5f6   pushed to the hub   Oct 9 14:02   (git_push)
   commits   repo atrium   fix-login   d4e5f6 -> 9f8e7d   pushed to the hub   Oct 9 16:40   (git_push)
   blob      build-out.tgz    412 MiB   artifact   kept   Oct 9 16:41                       (hub_files)
   blob      trace.pcapng      88 MiB   artifact   kept   Oct 9 17:03                       (hub_files)
   blob      usage.sqlite      14 MiB   drop       expires in 5 days                        (hub_files)
```

`GET /_hub/inventory?card=` and `?room=` answer this. It is a read-only union of `hub_files` and `git_push`, both of
which already carry `room` and `card`. There is no third table, nothing to keep in step, and deleting the answer's
cache loses nothing. The board shows it in each card's detail and on the room's page. A card's rows are listed with
`room~id` as the key, the same key `card_resources` uses.

The room's own inventory reuses what shipped. After a successful put the room writes one `card_resources` row of a new
kind `ResBlob`: `ref` the hub file name, `detail` the sha, `bytes` the size. It is held off every room as `ResStash`
is, so closing the card does not free it, and it is never read back by anything that matters. Its use is the close
flow: "this card has 2 blobs on the hub, 500 MiB, the local copies may go". The hub table is the truth. If the room row
is lost, nothing is lost.

## How it merges with the file drop (a section the file-drop doc can point to)

The file-drop doc's store becomes this store. Concretely, when its phases are built:

1. Its phase 1 builds `hub_files` with a `class` column, not `drop_files`. The `drop/` blob directory is `files/`.
   Rows from a drop have `class = 'drop'` and everything else in that draft holds unchanged: names, 7-day expiry, the
   hourly sweep, the secret checks, the audit lines (`ctl-put`, `ctl-get`, `ctl-file-delete`), the Files page, `dest`
   on a get, `atrium export`.
2. Its three tools are the three tools above. They gain `keep`, `scope` and `class`. A call that sets none of those
   behaves exactly as that draft describes.
3. This item adds, on top of it: the `artifact` class and its caps, retention tied to the card closing, ranged pulls for
   large files, the inventory join with `git_push`, the `ResBlob` row, and the housekeeping lines.
4. Its open question 3 (are 32 MiB a file and 512 MiB in all right) is answered per class here. The drop keeps 32 MiB
   and 512 MiB, and the 1 GiB limit applies only to artifacts.
5. Its decision 3 (is this the same as f-hub-blob-storage) is settled: yes, one store, and the drop is a retention
   class of it. Its phase 6 (streaming) moves earlier, into this item's phase 3, because artifacts need it.

If the file-drop doc is built first, its phase 1 needs only the `class` column and the shared `files/` name to avoid a
migration later. That is the one cheap thing to do in advance.

## Retention, cleanup and housekeeping

- A drop expires by date and is swept hourly, as the draft says. A file past its date is refused by get before the
  sweep.
- An artifact has no date while its card is open. When the card closes the room tells the hub, which sets `expires` to
  30 days on. If the hub never hears (the room was wiped), the row has no date and the housekeeping report lists it as
  "card unknown for 60 days". It is never deleted on a guess.
- A pin clears `expires` until unpinned. Pin and unpin are board actions and operator only.
- The sweep deletes expired rows, then blobs no row names. A restored older hub store brings back rows whose bytes may
  be gone, and get answers 410, "the bytes are missing", the words documents use.
- The hub NEVER deletes an artifact because the disk is full. It refuses new artifacts with a 507 and says so. The
  daily housekeeping report (`docs/rnd/daily-housekeeping-report.md`, a suggestion and never state) gets a Hub line:
  "3 artifacts of closed cards expire within 7 days, 6.2 GiB", and "artifact store at 80 percent". Its buttons are the
  existing flows: Show opens the Files page filtered, Pin and Delete are the board's own actions each with its own
  confirm. There is no "clean everything".
- Every put, get, pin and delete is audited through the existing `audited` wrapper, and the audit line outlives the
  file.

## Resilience

The store is never state a room depends on, the same as the drop. No room startup, hook, permission check or message
delivery reads it. A put or get that fails is a tool result the agent reads, and nothing runs inside a hook. A hub with
the store full or broken stops serving files and nothing else, except that a store failure of `hub.db` stops the hub as
every table there does. Rule 8 holds: the hub reads a card's file only through the room's safepath endpoint and writes
only through the room's upload endpoint, and a board download goes from the hub to a browser, not between cards.

The one honest weakness: for an artifact the hub disk is now the only durable copy of a file that left its room. That is
the point of the item, and it makes the hub's disk a thing worth backing up. See open question 5.

## Rejected alternatives

- Two stores, a drop and a blob store. Two tables, two blob dirs, two sweeps, two sets of tools for a worker to choose
  between, and a file that matters both ways would be copied. The only real difference is a retention class.
- Put large binaries in git (LFS or plain). Git on the hub carries code by branch. A recording in a branch is in every
  clone forever, and cannot be expired.
- Raise the `atrium_publish` caps. Documents are versioned, rendered and permanent, and a 1 GiB hub document would sit
  in the viewer shell and the version history. Documents are for readers.
- A second inventory table for artifacts. `git_push` and `hub_files` already carry the card and room, and a union is
  one read. A table that mirrored them could only drift.
- Store the artifact in the room and keep only a pointer on the hub. That is the situation today, and a wiped room
  loses the file.
- Let the hub delete artifacts under disk pressure. It is a guess about what clint still needs.
- External object storage (S3 or the like). A new dependency, a new credential and a new network path for a hub that
  today needs none. Reachable later: all bytes go through `blobPath` and an open call, so a backend could be swapped.

## Phased build

Each phase is one worker and ends with its tests passing and the earlier phases working on their own.

1. **file store (fabric).** `hub_files` table and migration, `class`, the `files/` blob dir, add, list, open and delete,
   the settings and caps per class, the hourly sweep, secret checks, 413 and 507 and 429, the audit lines. Done when
   integration tests beside `docs_integration_test.go` cover both classes, caps per class, a shared blob that survives
   the delete of one of two rows, and the sweep. If the file-drop phase 1 is already built, this phase is its rename
   and the `class` column.
2. **the three tools, small files (fabric).** `atrium_put`, `atrium_files`, `atrium_get` with `keep`, `scope` and
   `class`, in the worker and full sets, audited, and the check whether the room answers byte ranges. Done when
   `ctlclass_integration_test.go` is updated, a put by path and a get land a file between two test rooms, and
   `website/docs/control-mcp.md` describes the tools.
3. **large files (runtime and fabric).** Ranged pull into a temp file, hashing and secret checks over the stream, the
   1 GiB artifact cap, temp-file cleanup, and the room's byte-range answer if phase 2 found it missing. Done when a test
   puts a file over 100 MiB with the hub's memory use bounded, and a pull cut half way leaves no row and no stale temp
   file after the sweep.
4. **inventory (fabric).** `GET /_hub/inventory?card=` and `?room=` as the union with `git_push`, `scope: mine` on
   `atrium_files`, the `repo`, `ref` and `commit` columns filled on a put. Done when a test card that pushed twice and
   put two blobs gets four lines in order, and a different room's card gets none of them.
5. **the board (ui).** The Files page with both classes, per-card and per-room inventory views, usage bars per class,
   pin, unpin, keep (drop to artifact), delete with a confirm. Done when the page lists, downloads, pins and deletes
   against a test hub and nothing is rendered, only downloaded.
6. **retention and the room side (runtime).** The room tells the hub when a card closes, the hub sets the 30-day
   `expires`, the room writes the `ResBlob` row, the close flow shows "N blobs are on the hub". Done when closing a test
   card with an artifact dates it, a card that is never closed leaves it undated, and the close flow prints the line.
7. **housekeeping lines (ui).** The Hub lines in the daily report: expiring artifacts, store near its cap, artifacts
   of a card unknown for 60 days. Done when each line appears on a seeded hub and no line appears on a clean one.

## Open questions for clint

1. A card uploads a 3 GiB recording, then? Recommend: refused with a 413 that says the limit is 1 GiB and to compress or
   split it. 1 GiB covers a build output or a capture, and the operator can raise the setting if a real case turns up.
2. A card is closed and its artifacts are still wanted a month later, then? Recommend: they expire 30 days after the
   close, the housekeeping report warns a week before, and pinning keeps one for good. Never expire while the card is
   open, and never expire on a guess.
3. The artifact store reaches its 20 GiB cap, then? Recommend: new artifacts get a 507 and the board and the report say
   so. Nothing is deleted for room. Drops are not affected, since they have their own cap.
4. A binary artifact contains a token the content scan could not see, then? Recommend: accept that, as publish does. The
   scan covers the first and last 4 MiB and the file name, the board says "partly scanned", and the tool text tells
   workers to put outputs and not working directories. A guarantee is not on offer for binary formats.
5. The hub's disk dies, then? Recommend: artifact blobs are NOT in the hub's backup by default, since 20 GiB of binary
   would swamp it, and the board says the hub disk holds the only copy. A setting copies them if clint wants it. Is
   losing an artifact with the hub disk acceptable, or should the backup include them?
6. Anyone who can reach the board can download any artifact, then? Recommend: yes, as with documents and the drop.
   A card name is a header anything on loopback can set, so a per-card lock would only look like access control.
   A private file is not put here. A share widens that reach, so say so on the Files page.
