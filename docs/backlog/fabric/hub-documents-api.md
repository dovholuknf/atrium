# Hub documents: the D1 API as it will ship

For @ui's D2 mocks. Written by @fabric on 2026-09-30 from `docs/rnd/hub-documents-design.md`. This is the contract D1
builds to. A change to it goes to @ui before it lands. Everything is on the hub's board port, under `/_hub/docs`.

## URLs a client parses

- `/d/<slug>` is the newest version and `/d/<slug>@<n>` is version n. The hub answers both with the `/m/` shell, so
  the page reads the path itself.
- A slug is `[a-z0-9-]{1,60}`. `n` is a positive integer. Split on the LAST `@`. A slug never contains `@`.
- A deleted document's URL still answers with the shell. The metadata call says it is deleted.

## Shapes

A **version**:

```json
{"n": 2, "at": "2026-09-30T21:04:11Z", "by": "r-031@sg4", "origin": "card", "card": "sg4~01a0f4f7",
 "size": 1832, "kind": "markdown", "mime": "text/markdown", "name": "usage.md", "sha": "<hex sha-256>",
 "missing": false, "purged": false}
```

- `origin` is `local`, `share` or `card`. `card` is present only for `card` and is `room~id`, the card URL form.
- `by` is the card's handle for `card`, and the word `operator` for `local` and `share` for a share. Show the ORIGIN, not `by`,
  when deciding how much to trust a version. Only `local` may be called clint.
- `kind` is `markdown`, `text`, `image`, `diff` or `other`. `mime` is what the hub worked out. `name` is the file name
  the author gave, possibly empty.
- `missing` is true when the bytes file is gone, and `purged` when the operator purged them. The page says so and
  shows no body.

A **document summary** (list rows):

```json
{"slug": "usage-2026-09-29", "title": "Usage 2026-09-29", "created": "...", "updated": "...", "versions": 2,
 "latest": { ...a version... }, "deleted": null}
```

`deleted` is `null` or `{"at": "...", "by": "operator"}`. `by` follows the same rule as a version's: `share` when the
tombstone came over the share.

## Routes

| Method and path | Who | Answer |
| --- | --- | --- |
| `GET /_hub/docs?q=<text>&deleted=1` | any | `{"docs": [summary...], "usage": {"bytes": N, "cap": N}}`, newest `updated` first. `q` filters by title, case-insensitive substring. Without `deleted=1` tombstones are left out, with it ONLY tombstones are listed (the restore list). |
| `GET /_hub/docs/<slug>` | any | `{"slug", "title", "created", "deleted": null or {at,by}, "versions": [version...]}`, versions ascending by `n`. A tombstoned document answers 200 with `deleted` set. |
| `GET /_hub/docs/<slug>/raw?v=<n>` | any | The bytes. `v` defaults to the newest. ALWAYS `Content-Type: application/octet-stream`, `X-Content-Type-Options: nosniff`, `Content-Disposition: attachment` with an RFC 6266 name. The client reads `kind` and `mime` from the metadata and renders from these bytes, never inline. 410 for a tombstoned document, 404 for an unknown version, and for a `missing` or `purged` version 410 with `{"error": "the bytes are missing"}`. |
| `POST /_hub/docs` | any | Upload a NEW document. Multipart, see below. |
| `POST /_hub/docs/<slug>/versions` | any | Upload a new version of this document. Same body. |
| `POST /_hub/docs/<slug>/title` | any | JSON `{"title": "..."}`. Renames. The slug never changes. |
| `POST /_hub/docs/<slug>/delete` | any | Tombstone. `{"ok": true}`. |
| `POST /_hub/docs/<slug>/restore` | any, but see below | Undo a tombstone. A document with a PURGED version is restored by the operator only. |
| `POST /_hub/docs/<slug>/purge?v=<n>` | operator only | Delete the bytes of version n, or of every version without `v`. Metadata stays, `purged` becomes true. Versions with the same bytes share one file, so the answer carries `"also": []`, the other versions purged with it as `slug@n`. |
| `GET /_hub/docs/settings` | any | `{"operator": true, "enabled": true, "caps": {"text": 5242880, "other": 20971520, "total": 2147483648, "per_card_hour": 30}, "usage": {"bytes": N, "docs": N, "versions": N}, "largest": [{"slug", "title", "bytes"}]}`. `operator` says whether THIS request may change settings and purge, so the gear can grey the controls. |
| `PUT /_hub/docs/settings` | operator only | `{"enabled": bool, "caps": {...}}`, any subset. Answers the same shape as the GET. |

"Operator only" is `edge.LocalOperator`: a browser or shell on the hub's machine. Over the share it answers 403 with a
sentence that says to run it on the machine.

## Upload

- **Multipart form data**, one request, no JSON variant for the board. Fields:
  - `file` (required): the bytes. The file name becomes the version's `name`.
  - `title` (optional, new documents only): defaults to the file name without its extension. It is ignored on a
    new-version upload, which keeps the document's title. Rename with the title route.
  - `override` (optional, operator only): the value `1` lets one upload past a secret rule. It is recorded on the
    version. From a non-operator it is 403, not ignored.
- A new-version upload to `POST /_hub/docs` with a `slug` field is NOT supported. Use the versions route.
- The answer, status 201:

```json
{"slug": "usage-2026-09-29", "version": 1, "url": "/d/usage-2026-09-29", "version_url": "/d/usage-2026-09-29@1"}
```

- The origin is decided by the hub from the request, never read from the form: `local` for an operator request,
  `share` for anything else. A page cannot claim `local`.
- The body is bounded at the larger cap plus the form overhead, and a larger body is cut off with 413.

## Errors

Every refusal is JSON `{"error": "<a sentence>"}` and the status says the kind. A secret refusal adds `"rule"`.

| Status | When |
| --- | --- |
| 400 | unreadable form, no file, an empty file, a title that leaves no text |
| 403 | not the operator for purge, settings or an override. A cross-origin write (`Origin` another host or `Sec-Fetch-Site: cross-site`). From `atrium_publish`, a path outside the card or through a symlink |
| 404 | an unknown slug or version |
| 410 | raw bytes of a tombstoned, missing or purged version, and a new version to a tombstoned document (restore it first) |
| 413 | over the size cap for the kind: 5 MiB of text, 20 MiB of anything else |
| 422 | a secret rule: `{"error": "...", "rule": "pem-private-key"}`. Rules: `secret-file-name`, `pem-private-key`, `github-token`, `aws-access-key`, `slack-token`, `jwt`, `zrok-token`. Applies to a board upload too |
| 429 | a card past 30 publishes an hour (publish only, not board uploads) |
| 503 | publishing turned off by the operator. The sentence is fixed, there is no reason field |
| 507 | the store is at its total cap |

## Concurrency and the browser

- **No ETag or precondition in stage 1.** Versions are appended, so two writers never overwrite each other, and
  the newest wins on `/d/<slug>`. Rename and delete are last-write-wins.
- **The page needs to add nothing for the cross-origin check.** A same-origin `fetch` from the board sends
  `Sec-Fetch-Site: same-origin` itself, and the hub already refuses a foreign `Origin`. Do not set `Origin`, and do
  not use `mode: "no-cors"`. The page must be loaded from a host the hub knows (loopback, the share's host, or
  `ATRIUM_HOSTS`), which the board always is.
- A write needs no token or header of ours. `X-Forwarded-For` and its kin are added by proxies, not the page.

## Agents

`atrium_publish` takes `title`, plus `content` or `path`, and an optional `slug` to add a version. It answers
`{"url": "/d/<slug>", "version": n, "slug": ...}`. It is in the worker set too, since workers write the reports.
A card's own list of documents is `GET /_hub/docs?card=<room~id>`, which returns the same
summaries filtered to documents with a version written by that card. @ui can use it for "published N documents".
