# Review: f-hub-fetch-pass (forge stage 3, the room's served set) 453e754a

Range `526337e1..453e754a`, one commit on claude/f-hub-fetch-pass. @fabric's worker wrote it, and @runtime read it
and sent it here.

It changes:

- `internal/gitsync/backend.go`: `HideFor` per request; filter, any-sha, tip-sha and reachable-sha wants off; the
  `cgiEnv`;
- `room.go`, which adds `Live`;
- the new `served.go` and `served_test.go`;
- `liveBranches` in `internal/cli/roomrun.go`.

Verdict: **OK** for hub and room. There are three Lows.

## How it was checked

- The gitsync tests pass at the tip (118 s, git 2.50.1), and `go vet` is clean on gitsync and cli.
- Two mutants:
  - Keeping the `Git-Protocol` header, which lets v2 through, fails `TestTheRoomsUploadPackSpeaksV0…`.
  - Dropping the `refs/heads/claude/main` re-hide fails three tests.

## Points

- **v0 is enforced, not assumed.**
  - The `Git-Protocol` header is dropped, so `HTTP_GIT_PROTOCOL` never reaches the CGI.
  - That matters, because a v2 `fetch` takes a want for any object whatever hideRefs say. v0 is what makes "a want
    is an advertised tip" true.
  - Stateless HTTP v0 also takes an ancestor of a served tip by sha, as the author notes. That is content the
    served tip already carries, so it is not a leak.
- **The served set is ordered right.**
  - Everything is hidden, `HEAD` included. Then `claude/` comes back, `claude/main` goes out again, and each live
    branch comes back.
  - The last match wins, and `claude/main` is skipped even when a card is on it.
  - hideRefs matches by prefix, so `!refs/heads/feature` would also show `feature/x`. Git's directory/file rule
    keeps `feature` and `feature/x` from both existing, so the prefix cannot show more than the one branch.
- **The repository's own config cannot loosen it.**
  - The policy goes in by `GIT_CONFIG_COUNT`, which ranks above the clone's `.git/config`. So a local
    `allowAnySHA1InWant=true` loses.
  - A local hideRefs `!…` entry comes before the env's `refs` hide-all, so it loses too.
  - `cgiEnv` keeps the system and global config out.
- **`servableBranch`.** The value never becomes a config line (it goes through the env), and the guard refuses
  `refs/`, a leading `-`, `..`, `@{`, glob and quote characters, and dot-segments anyway.
- **It is read on every fetch,** so a card that ends stops being served at once.

## Lows

- **L1: a card is matched to a repo by its folder's last segment** (@runtime's note). Two repositories with the
  same folder name, for example `a/atrium` and `b/atrium`, can each serve a branch NAME of the other's live card.
  It is only a name, and only when the other repo has a branch of that name. Match on the card's full
  `<host>/<owner>/<repo>` once the card records it (stage 2's scm path gives that), or on the clone's path.
- **L2: a live card on the clone's own `main` serves `main`.**
  - A card that works in the operator's checkout on `main`, rather than in a worktree, puts `refs/heads/main` in the
    served set.
  - That is probably fine, since the card's work is served. Still, the design says "live cards' branches", and
    the operator's `main` may hold unpushed work.
  - Decide whether a card on the clone's default branch is served, and write the decision into `ServedHide`.
- **L3: "live" means running, needs-input and needs-permission.**
  - A parked or idle card that still owes a review stops being served until it wakes.
  - That is intended ("a card that ends stops being served"), but a parked card is not ended. Say which is meant.

Atrium-Verdict: room-ok 526337e1..453e754a
Atrium-Verdict: hub-ok 526337e1..453e754a
Quality: the served set is a pure function with tests through real upload-pack. The v0 choice is the load-bearing
line, and a test guards it. m1mini commits are unsigned.
