# Review: f-hub-git-store 06ea9026 (hub forge stage 1 part 1, design rev 2 section 3.1)

Range cd07b86e..06ea9026, read by @review on m1mini. The commit is unsigned, like every m1mini commit. go vet is
clean, gofmt is clean, and gitsync, hubstore, link (Git) and cli (Git, Rooms) pass.

## (1) URL handling: OK

- **Userinfo:** refused both as `u.User` and as an `@` in the host, and no refusal repeats the input. Control
  characters, spaces, backslashes and non-ASCII are refused before parsing. The only scp user allowed is `git`.
- **Hostile URLs refused:** non-https schemes (so no `ext::` and no `file://`), ports, query, fragment, `%` escapes
  and a path that `path.Clean` changes.
- **Segments:** `[A-Za-z0-9._-]{1,100}`, with no `.` or `..`, no `.git` suffix, no trailing dot, and no Windows
  reserved names.
- **Hosts:** `^[a-z0-9]...$`, so a host can't start with `-`.
- **Argv:** the forge URL is rebuilt as `https://<host>/<o>/<r>.git` from the three parts only. It sits after `--`
  in ls-remote and fetch. The store path is absolute, and the refspec target is fixed. Nothing reaches argv in an
  option position.
- **A host named `github`** canonicalises to `github` and is seeded from github.com, never from a LAN host called
  `github`. Fine.
- **Case:** collision() walks the host, owner and repo with EqualFold, and the lock is keyed on the lowercased name.

## (2) The seed: OK

- **Env:** GIT_CONFIG_NOSYSTEM, GIT_CONFIG_GLOBAL=devnull (which covers XDG too), an empty GIT_ASKPASS,
  GIT_ALLOW_PROTOCOL=https, and GIT_TERMINAL_PROMPT=0 from CleanEnv. Timeout two minutes.
- **What's left on disk:** no remote is added and nothing of the URL is written. That holds through
  `--template=` and `--initial-branch=main`, and is tested (TheNewRepoHoldsNoURLOrCredential,
  IgnoresTheOperatorsGlobalGitConfig).

## (3) Adopting a mirror: HOLD on M1, proven

- **The first init of a git_repos mirror** only writes the marker, and the refs are unchanged. That is tested.
- **M1: the second init seeds main into the mirror.** initRef's `existed && isMarked(dir)` case falls through to
  the seed whenever main is empty. A mirror holds only `claude/main`, so its main is always empty, and once marked
  it is seeded.
  - I proved it by adding a second `Init` to TestTheStoreAndTheGitReposMirrorsCoexist. It returned "already there,
    and main was empty, so it was seeded from the forge's default branch". The mirror's refs went from
    `claude/main` alone to `claude/main` plus `refs/heads/main` fetched from the forge.
  - The mirror is what the link serves rooms, so they now see a forge main the operator never pushed. This breaks
    the file's own rule: "a mirror is never changed by being inited".
  - Fix: seed only a repository this store created. Write the marker as `made` or `adopted` and never seed an
    adopted one, so its main is the operator's push. Add the second init to the coexist test.
- **M2, minor: two locks on one directory.** An adopted mirror is locked as `mirror:<name>` by mirrorOne and as
  `store:<name>` by the store, so a seed and a mirror pass can run in the same repository at once. mirrorOne
  starts with cleanLocks on `refs` and `packed-refs`. Fixing M1 removes the seed side. Beyond that, take the
  mirror's lock whenever the directory is a configured mirror.

## (4) /_hub/git/repos: OK

- **Shape:** host, owner, repo, url, path, main {sha, at|null} and branches [{name, sha, room, card, at, released}],
  matching @ui's fixtures. `claude/main` is left out of branches. No disk path: `path` is the URL path.
- **Errors:** the route's own 503 is a fixed sentence. Init's errors and notes go through scrub (Root and Dir, in
  both slash forms).
- **L1:** the scrub is literal. A Windows path git prints in another case or as an 8.3 short name, or a resolved
  symlink like macOS `/private/var`, gets through. Scrub case-insensitively on Windows, and also scrub
  `filepath.EvalSymlinks(root)`.

## (5) Gating, limits and methods: OK

- repos is GET only and open, like growls.
- init (POST) and settings (GET and PUT) are behind `edge.LocalOperator`. settings' GET is gated too, since `store`
  is a hub path.
- Both bodies are capped at 4 KiB through a LimitReader. Decode errors name a type, never a value.
- SetGitStore wants an absolute path, not a disk root and not a file.

## (6) Concurrency: OK, with M2 above

- Every store git command runs under the per-repo lock, and InitInParallelMakesItOnce covers the race.
- **L2:** `Foo/x` and `foo/y` are different lock keys. Racing on NTFS, both pass collision() and both MkdirAll,
  and the second lands under the first's casing. The result is listed under the other owner's spelling. A store-wide
  lock around collision plus MkdirAll would close it, since an init is rare.

## Other lows

- **L3:** the seed fetch should set `-c transfer.fsckObjects=true`. The forge's objects are untrusted, and the hub
  serves them on to rooms.
- **L4:** segments may start with `-`. Nothing here puts one in an option position, but the next items, serving
  and receive, will. Refuse a leading `-` now.

Closed: none (first read) / Open: M1, M2, L1, L2, L3, L4

Verdict: HOLD cd07b86e..06ea9026, on M1. The fix is a few lines: never seed an adopted repository. Everything else
in your list holds.

Quality: careful and well tested. The URL parser refuses without echoing, the seed's environment is clean, there
is no migration, and the shape matches the board. The adopt path is right on the first call and wrong on the
second, and the coexist test stops one call short of showing it.
