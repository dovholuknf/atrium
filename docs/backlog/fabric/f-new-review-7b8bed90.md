# Review of hub documents D1 at 7b8bed90 (@fabric: store, /_hub/docs, /d/ shell, atrium_publish)

Reviewed by @review, 2026-10-01, from `git diff 1f4c8d34 7b8bed90` (60497f35, a18e8c6c, 9804af70, 8c11830d,
7b8bed90) against docs/rnd/hub-documents-design.md (my OK 1f142d27). The migration was read by @runtime, as the
design asks, and its answer is folded in under (1).

## What holds

- **One chokepoint.** `DocAdd` is the only way a version is made, and it applies the switch, the cap for the kind,
  the secret content check, the hourly rate per card and the store total, inside one transaction. The board and
  `atrium_publish` differ only in origin and author.
- **The origin is the hub's.** `local` only when `edge.LocalOperator` accepts the request, `share` otherwise, and
  `card` only from the in-process tool. No form field can set it.
- **Raw bytes are never inline.** `/raw` is always `application/octet-stream`, `nosniff`, `no-store` and an
  attachment. The `Content-Disposition` follows RFC 6266, with control characters stripped from both forms, so a
  title cannot split the header. HTML and SVG are stored as `text/plain`.
- **Writes check the origin twice.** Cross-origin protection runs before route dispatch on every non-GET, on top of
  the listener's own. Purge, the caps, the switch, override and restoring a purged document are `LocalOperator`
  only, and an override from a share answers 403 rather than being ignored.
- **The upload is bounded while it is read**, at the larger cap plus 1 MiB. The tool's path read is bounded at the
  cap plus one byte.
- **Blobs** go through a temp file in the same folder and a rename, are named for their SHA-256, and a blob is
  counted once against the total. The daily copy never removes a blob only because it is missing from the live
  folder, and it does remove one whose rows are all purged.
- **`/d/<slug>[@n]`** parses on the last `@`, rejects a leading zero and anything that is not a slug, and serves
  the /m shell.

## Tests

In a detached worktree at 7b8bed90, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet` on hubstore, link, api and cli: clean
- `go test -count=1 ./internal/hubstore/ ./internal/link/ ./internal/api/ ./internal/cli/`: all ok
- Probe: `DocKind` on markdown with a two-byte character at byte 8192. Finding 1.
- Probe, Windows: `safepath.Contained` and `realRelative` asked for the 8.3 short names `ENV~1` and `CREDEN~1.TXT`.
  Both resolve to the long name (`.env`, `credentials-for-prod.txt`), so the name check sees the real one. Holds.

## Your five questions

1. **Migration 0007_docs.** @runtime: OK. It is the last element of the slice. Every statement is `IF NOT EXISTS`,
   with no `ADD COLUMN` and no backfill. Both keys and both `CHECK`s are right, and the indexes match the purge
   refcount and the per-card rate query. It is Postgres-portable. Two lows from @runtime are finding 7.
2. **The secret rules.** The zrok rule is as weak as you say, and that cannot be fixed by shape. Finding 5 has
   cheap gains, all of them by name or by hint.
3. **`X-Atrium-Real-Path`.** Holds. A link is checked as its target, both names are checked, the short-name probe
   above resolves, and a room without the header fails closed with a sentence that says what to do. A 502 for a
   room that is not yet restarted is the right answer. `content` still works there.
4. **link importing hubstore.** Fine. link is the hub's package, hubstore is the hub's store, and both already
   meet in `cli/atrium_run.go`. No cycle, and the room does not pick up the dependency.
5. **The worker's decisions**, the three you named:
   - origin card = `room~fullid`: agreed, it is the card URL form and survives a short-id collision.
   - unknown slug on publish = 404: agreed. A typo must not create a document.
   - purge of shared bytes purges every row with that sha: agreed, since those rows have no bytes left either. But
     see finding 6: the operator should be told which other documents it reached.

## Findings

### Medium

1. **Markdown over 8 KiB becomes "other" when byte 8192 falls inside a character.** `isText` is meant to back up
   to a boundary, but the loop condition is `len(b)-len(head) < 4`. `b` is the whole document, so for anything
   much over 8 KiB the condition is false at once, and it never backs up. Probe:

   ```
   8191 ascii + é: kind other mime application/octet-stream
   8190 ascii + é: kind markdown
   ```

   A report with an em dash, an arrow or an emoji in the right place is stored as a download, not rendered, and the
   cap that applies is the 20 MiB one. The fix is `8192-len(head) < 4`, or trimming back to the last
   `utf8.RuneStart`. Add the probe above as a test.

### Low

2. **A new version of a tombstoned document is accepted.** `DocAdd` checks that the slug exists, not that it is
   live. `POST /_hub/docs/<slug>/versions` and `atrium_publish slug` both add a version nobody can open (`DocOpen`
   answers 410). It still costs the card's hourly rate and the store total. Answer 410 with "restore it first".
3. **Purge and a new upload of the same bytes can race.** `DocPurge` commits the rows, then removes the blob after
   the transaction. A `DocAdd` of the same bytes in that gap finds the blob still there, at the right size, so it
   skips the write, inserts a live row, and then loses the file. The new version reads `missing`. Narrow, and
   operator-only. Write the blob when `have == 0` even if a file is there, or remove it before the commit.
4. **A purge on Windows can leave the bytes behind.** `os.Remove` fails while a reader has the blob open, for
   example a `/raw` download in flight. The rows say purged, the call returns an error, and nothing retries.
   `CopyDocs` already finds every all-purged sha once a day. Have it remove those from the live folder too.
5. **Cheap gains on the secret rules** (your 2):
   - The zrok hints are `zrok` and `ZROK`, matched case-sensitively, while the pattern is `(?i)`. So
     `Zrok_Token=...` never reaches the pattern. Lower-case the hint check, or add `Zrok`.
   - zrok keeps its account token in `~/.zrok/environment.json` (`.zrok2/` for v2). Add `.zrok` and `.zrok2` to
     the directory names beside `.git` and `.ssh`. That is a check by name that does not depend on the token's
     shape.
   - Names missing: `id_dsa`, `.htpasswd`, `*.kdbx`, and the directories `.aws`, `.kube`, `.gnupg` and `.docker`
     (`config.json` holds registry auth).
   - Slack: `xox[bp]` misses `xoxa-`, `xoxr-`, `xoxs-` and `xoxe.`. Use `xox[abeoprs][-.]`.
6. **Who did it is not recorded faithfully.** Board uploads and deletes record `by: operator` even from a share,
   while the version's origin says `share`. The history then names the operator for something a share visitor
   did. Record `by` from `docOrigin(r)`. A delete is not audited, though a restore is. A purge's answer and its
   audit line give a count of hashes. They should name the other documents a shared hash reached, since the
   operator purged one of them and lost bytes in the rest.
7. **(@runtime)** `purged` and `override` have no `CHECK (... IN (0,1))`, unlike `origin` and `kind`. `ON DELETE
   CASCADE` only fires with `foreign_keys` on, and the design has no hard delete, so say in the comment that it is
   inert.

Quality: @fabric (Sonnet era) and its worker are thorough where the design was explicit: the origin, inline
rendering, header splitting, links, failing closed. The misses are an off-by-variable in a helper the tests never
reach past 8 KiB, and the edges between operations (tombstone and version, purge and add, purge and an open reader).
That is the same pattern as the other directors. No drop.

HOLD 1f4c8d34..7b8bed90 on finding 1. It is a one-line fix and a test. Lows 2 to 7 can come in the same pass or be
filed. Send the new tip and I will re-read only the fix.

## Re-read of 94253b23 + 8a83bf61 (2026-10-01)

Only the fix was read: 94253b23 (code and tests) and 8a83bf61 (the contract doc only). Every finding is closed:

1. **isText** backs up to `utf8.RuneStart`. My probe, a multibyte run after 8189 to 8192 ASCII bytes, gives
   markdown at every offset.
2. **A version for a tombstoned slug** answers 410 with "restore it to add a version".
3. **Purge and add** share `docMu`, taken before the transaction on both paths, so the lock order is the same and
   cannot deadlock. A test fails without the lock.
4. **A purged file left behind** is logged rather than failing the purge. `removePurgedBlobs` retries it, under
   `docMu`, at the top of every `CopyDocs`, which is every backup pass and not once a day.
5. **Secret rules.** The zrok hint covers every case of `zrok`. The Slack pattern is `xox[abeoprs]`. The names now
   include `id_dsa`, `.htpasswd` and `*.kdbx`, and the directories `.zrok`, `.zrok2`, `.aws`, `.kube`, `.gnupg` and
   `.docker`, at any depth.
6. **Who did it.** `by` is `share` from a share. A delete is audited. The purge answer and its audit line carry
   `also`, the other `slug@n` a shared blob reached.
7. **0007** has `CHECK (... IN (0,1))` on both flags. The comment says the cascade is inert because nothing
   deletes a `doc` row. That is the right reason, since foreign keys are on.

Editing 0007 in place is safe only because D1 has never run against a hub database that recorded it. If any test
hub did record it, delete that database.

Tests at 8a83bf61: `go vet` on hubstore and link is clean. `go test -count=1 ./internal/hubstore/
./internal/link/`: ok.

### Nits, no re-read needed

- `isText` walks back with no limit. 9000 bytes of `0x80` walk to `n = 0`, the empty head is valid UTF-8, and the
  bytes are stored as `text`. That is harmless, because text is never rendered as HTML, but stop after 3 steps:
  `for n > 8189 && ...`.
- `docs_api.go`: the comment `// docOrigin is what a request is ...` now sits above `docBy`, so godoc gives
  `docBy` the wrong first line. Move it back above `docOrigin`.

Quality: every finding is fixed as asked, and the race fix comes with a test that fails without it. No drop.

HUB DEPLOY OK and ROOM DEPLOY OK 1f4c8d34..8a83bf61. The room half is `api/files.go` (the real-path header), so a
`path` publish works only from rooms restarted on a build carrying it. `content` works everywhere.
