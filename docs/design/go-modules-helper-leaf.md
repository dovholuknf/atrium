# Go modules split: the helper leaf

One bounded step of `go-modules-movability.md`, item 3. Six small helpers left `internal/daemon` for a new leaf
package, `internal/small`. Nothing else moved and no name changed beyond the export.

## The package

`internal/small`. It holds small, plain helpers that many daemon sets call and that need nothing from the daemon. It
imports only `store` (for `IsParked`) and the standard library. A name like `helper` was tried first and dropped,
because daemon tests already use a local variable called `helper`.

## What moved

| Old name and file | New name | What it does | Call sites changed |
| --- | --- | --- | --- |
| `writeJSONErr`, messages.go | `small.WriteJSONErr` | writes a JSON error body with a status | 174 |
| `hasTag`, a2a.go | `small.HasTag` | case-blind tag membership | 75 |
| `isParked`, park.go | `small.IsParked` | is a card parked | 43 |
| `errString`, finish.go | `small.ErrString` | an error from a string literal | 36 |
| `firstLine`, launch.go | `small.FirstLine` | cuts a summary to one short line | 25 |
| `launchKeys`, `keyedMutex` with `newKeyedMutex`, `lock`, `heldKey`, `launchTaskKey`, `launchResumeKey`, keyedmutex.go and stopkeep.go | `small.LaunchKeys`, `small.KeyedMutex`, `small.NewKeyedMutex`, `Lock`, `HeldKey`, `LaunchTaskKey`, `LaunchResumeKey` | the per-card and per-resume launch lock | 5 `LaunchKeys` and 8 `Lock` or `HeldKey` |

`keyedmutex.go` and its integration test moved whole with `git mv`. `heldKey` had to come along because a method must
live with its type.

## Left out

- `hideWindow`: it is not a function in the daemon. `nowindow.go` is a one-line alias, `var hideWindow = nowindow.Hide`,
  and `internal/base/nowindow` is already a leaf. Callers can use `nowindow.Hide` directly, which is a different cut
  (about 11 files) and not a move.
- `isClaude`: it calls `runsAs`, a 12 line function that `isCodex` shares. Moving it would pull `runsAs` out too and
  force `isCodex` to call an exported name, which is more than a few lines of other code.
- `truncatePeer`: it needs `maxPeerMessage`, a constant in the const block of `peers.go` that five other places use.
  Moving it means moving or duplicating the constant.

## Size

- 84 files in the commit, 83 code files and this note. Code diff: 500 insertions and 403 deletions, renames counted
  once. Of the 83, 80 are daemon files and 3 are in `internal/small`.
- Most of the call-site edits are one word each, plus one import line per file.

## Tests

`bash scripts/test.sh all ./internal/daemon` on claude/main before the change: ok.
`bash scripts/test-touched.sh` after (`./internal/daemon`, `./internal/small`, integration tag): ok, gofmt clean.
Same pass and fail set, no failures either side. `go vet` is clean for the default and integration tags and for
GOOS=windows.
