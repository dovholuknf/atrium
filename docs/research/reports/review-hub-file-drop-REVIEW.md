VERDICT: LAND AFTER FIXES

Reviewed 4fb66ecc (cf052ed7..4fb66ecc) by hand: the planned security and functional subagents were blocked by a hook, so every point below is from reading the code myself. `go test -tags integration ./internal/hubstore/ ./internal/link/ -run Files` passes. No HIGH found. The one MEDIUM is a cap that can be set to a value the room then refuses.

## Order of checks
1. **Migration** (internal/hubstore/schema.go, 0017_hub_files): appended at the end of the slice, every statement is `IF NOT EXISTS`. OK.
2. **Path safety** (internal/api/filedrop.go): `destEscapes` refuses absolute, rooted and `..` before anything resolves, then `safepath.Contained` runs before and after `MkdirAll` and on the final target. Non-replace writes use `O_EXCL`, so a symlink at the target is not followed. The room, not the hub, decides the root from the card. Hub-side names go through `cleanName` (strips directories). OK.
3. **Bounds and halt rule**: put-from-path reads with `LimitReader(limit+1)` (docs_mcp.go:281), upload with `MaxBytesReader(cap+overhead)` (files_api.go:188), export with `LimitReader` (files_mcp.go:500), the room drop with `MaxBytesReader` (filedrop.go:121). Refusals are `docRefuse`/`DocError` and `ErrNoRows`, which `guard` treats as answers, so none halts the store. OK.
4. **Sweep** (files.go FileSweep): expired rows deleted, then every blob no row names is removed, all under `filesMu`, which `FileAdd` and `FileDelete` also hold, so no blob is swept mid-add. A failed blob removal on delete is logged and the sweep retries. Stale `.tmp-` files go after an hour. No orphan blob and no row without a blob except after a hub restore, which the design documents.
5. **Design gaps**: none found beyond the items below.

## MEDIUM
- internal/link/files_mcp.go:336-343 and internal/api/filedrop.go:121: the room's drop limit is the constant `maxUploadFile` (32 MiB, internal/api/files.go:36), but the operator can raise the hub's file cap (files.go SetFileSettings). Above 32 MiB a file puts fine and then every `atrium_get` fails with 413. Fix: clamp `SetFileSettings` `File` to the room's limit, or have the room take the limit from the hub.

## LOW
- internal/api/filedrop.go:138-143: the file is written, then `AppendEvent` failing returns an error although the bytes landed. Fix: log the event failure and still answer 200 with the path.
- internal/hubstore/files.go:208-216: the per-card hourly rate counts live rows, so deleting a file resets the count. Fix: count puts in a log, or accept and document it.
- internal/hubstore/files.go:226-232 (quota): a blob on disk with no row yet (crash leftover awaiting the sweep) is not counted against the total. Fix: add the orphan bytes, or sweep at startup.
- internal/link/files_mcp.go:222: put `content` is fully in memory before `FileAdd` checks the cap, bounded only by the MCP transport. Fix: check `len(in.Content)` against the cap before `FileAdd`.
- internal/api/filedrop.go:46-56: on Windows a drive-relative `C:foo` is not refused by `destEscapes`. It lands inside the root, so it is harmless, but it makes an odd name. Fix: refuse a `:` in `dest`.
- internal/api/filedrop.go:99-110: a small TOCTOU window remains between `Contained` and the open. Rule 8 accepts this, and it needs a hostile local process.
- `dest` may point at `.git/hooks` or `.atrium` inside the caller's own card. That is no escalation, since the caller already owns the directory.

## Not checked
files.js and the headless board script were skimmed only (no raw `innerHTML` of file data found). The "departures" section of the design doc was not compared line by line.

Wall time and tokens: not measured; a restart cut the session in the middle (several hours of wall clock, well under 1M tokens).
