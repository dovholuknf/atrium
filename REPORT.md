# r-forge-bb report

## Built
- `internal/forge/bitbucket.go`: the Bitbucket Cloud forge. `forge.New("bitbucket", cmd, run)` returns it, `cmd` defaults to `bb`.
- `forge.go`: New builds it, and `AccessError` gained an optional `Login` field so the sentence can name `bb auth login`
  (no `--hostname`). The GitHub sentences are unchanged.
- `bitbucket_test.go`: fake runner with recorded JSON for same-repo and fork PRs, access errors, bad output.
  `TestUnbuiltForgeIsNoForge` now uses gitlab, since bitbucket is built. No network.

## Which bb
No official Bitbucket CLI exists and several unrelated tools are called `bb`. I picked datlechin/bitbucket-cli (Rust,
`cargo install bitbucket-cli`). Its docs show `bb pr view N -o json`, `bb pr diff N`, a raw `bb api <path>` with
`-o json`, and credentials in the OS keychain (`bb auth login`, `bb auth status`). I use `bb api` for all three reads
rather than `bb pr view`, because the REST 2.0 shapes are documented and stable and the CLI's own pr JSON is not:
- `GET /repositories/{o}/{r}/pullrequests/N` for title, author, head sha, branches, repositories
- `.../diffstat?pagelen=100` for the files
- `.../diff` for the unified diff

The `git credential fill` fallback was not needed and is not built. It could not be anyway, since `Runner` has no stdin.

## Decisions
- FromFork: source repository full name differs from the destination's, or the source repository is gone.
- FetchSpec: Bitbucket has no PR head ref, so the refspec is the source branch name. Remote is the destination's https
  URL for a same-repo PR and the fork's https URL for a fork PR. `FetchSpec(ref)` runs nothing and takes only the ref,
  so the branch and fork are remembered from the last `View` of that ref. Without a prior View it returns the
  destination remote and an empty refspec.
- Author is the nickname, else the display name. A deleted file takes its old path.

## Not done
- `bb` was not installed here, so nothing ran against a real `bb` or Bitbucket. The `bb api` argv and the `-o json`
  flag come from its README, unverified. The raw diff assumes `bb api` prints a non-JSON body as is.
- Diffstat is one page of 100 files, `next` is not followed.
- Callers must call `View` before `FetchSpec`. `prworktree.go` was not touched, so check its order.
- The preflight key table and recogniser rows were not touched.
