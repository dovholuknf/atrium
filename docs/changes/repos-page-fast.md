# repos-page-fast

- internal/gitsync/viewcache.go: `Store.Snapshot` / `RefreshView`, the snapshot the repos list reads. No git on a GET.
- internal/link/git_hub.go: git/repos answers from it, with `computed`, `age_seconds`, `building`.
- internal/gitsync/receive.go, store.go: a push and an init rebuild the snapshot.
- internal/gitsync/run.go: `Runner.Spawned`, a counter of git processes, for the test.
- internal/api/web/js/hubrepos.js: polls again while the hub says `building`.
- Test: viewcache_test.go (integration tag).
