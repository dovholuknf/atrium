# Review: r-hub-remote 3ad9d658 (hub-forge stage 1)

Range `ef406235..3ad9d658`, one commit, test-plan II. It adds:

- the room's hub forwarder at `/git/hub/` on the agent listener;
- per-card tokens behind `gitsync.CardAuth`;
- the launch env that carries a token to a card's git;
- the `hub` remote on room clones;
- `atrium_git_push`.

Verdict: **hold** on M1. The token itself is built well. The hole is in `atrium_git_push`, which checks the push URL in
a way the clone's own config can get around. M2 is a test that cannot fail. The Lows can follow.

## How it was checked

- I read the diff in full. That is the token table, the forwarder, the launch env, revoke on exit, `GitPush`,
  `InheritedTaint` and the clone remotes.
- Gates, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
  - gofmt flags only the known `fyi_test.go`;
  - vet is clean;
  - gitsync, link, cli, store and runnersetup pass;
  - the daemon Hub, GitPush, Token, Outside, GitClone and Launch tests pass, and so do api GitPush and Settings. The
    known reds were the only failures.
- Probes, in a scratch worktree with `git http-backend` behind a stub and the real forwarder in front:
  - two pushurls on `hub`;
  - `remote.hub.proxy`;
  - a client's own card headers, scoped and unscoped;
  - a fake token.
- Mutants, below.
- It merges onto landing 29efa251 cleanly, and II is the only section with that letter.

## The token, one condition at a time

1. **256 bits from crypto/rand.** OK. `Mint` reads 32 bytes with `crypto/rand`. The token is `<card>.<64 hex>`.
2. **Memory only, with a hash held.** OK for `CardTokens`: it keeps a sha256, never the token, and nothing is written
   to the DB.
   - However, `hubGit.issued` keeps the plaintext token per card, so the room can push with it. See L1.
3. **Constant-time compare.** OK.
   - `Check` splits on the last `.`, checks the hex length and `ValidCardID`, and then compares the hashes with
     `subtle.ConstantTimeCompare`.
   - It checks `Live` last, so a token that matches still fails for a card that has ended.
4. **It opens only the forwarder route.** OK.
   - The token is checked in `HubForwarder.ServeHTTP` and nowhere else.
   - The extraHeader is scoped to `http://127.0.0.1:<port>/git/`, so git sends the token there and nowhere else. A
     probe fetch from another server got no Atrium header.
5. **It is revoked on runner exit and whenever the card is not running.** OK.
   - `awaitExit` revokes once no runner is left for the card.
   - `cardLive` lets through only running, needs-input and needs-permission, so a shelved or finished card fails
     `Check` even before the revoke.
   - There is one race on resume (L4).
6. **No token for an outside-code card.** OK. `hubGitEnv` adds nothing when the card is tagged
   `atrium:outside-code` or the launch says `OutsideCode`, and `GitPush` refuses that card.
7. **Behind `gitsync.CardAuth`.** OK. The forwarder sees only `Mint`, `Check` and `Revoke`.

## No leak (B)

- **argv:** none. The token travels only in `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_n` and `GIT_CONFIG_VALUE_n`, at launch
  and in `PushToHub`.
- **`.git/config`:** none. `ensureHubRemote` writes the URL only.
- **Logs:**
  - No added log line prints the env or the token.
  - The forwarder's refusals never repeat the header.
  - `PushToHub` runs under `CleanEnv`, which strips `GIT_TRACE*`.
- **Card details, API JSON and the DB:** none.
  - `issued` and the table are in memory only, and the launch env map is not stored.
  - The env reaches the pty host over its local socket, the same way every launch env does, and the host does not
    keep it.
- **Transcripts:** the token is in the env of every tool subprocess a card runs, so a card that runs `env` sees its own
  token. That is the design, and outside-code cards are left out.
- **Setup scripts:** `InheritedTaint` strips `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_*` and `GIT_CONFIG_VALUE_*`. No test
  covers this (L3).

## The forwarder (C)

- **Del, then Set, of `X-Atrium-Card` and `X-Atrium-Card-Chain`.** The code is right. Its test is vacuous (M2).
  - On its own, Set already replaces, so dropping only the Del is harmless.
  - Dropping the Del and turning Set into Add would let a client's own values through. The hub's `identity.go` would
    then refuse the request as a second value, so this fails closed today. The forwarder's promise is still not
    tested.
  - The `X-Atrium-Card-Token` Del is tested: removing it fails `TestInACardEnvGitPushHubLandsWithTheCardInThePushLog`.
- **Refused before any link traffic.** OK.
  - The order is path, method, service, Content-Type, token, `git.push`, and only then the transport.
  - A probe with no token and one with a fake token both got a refusal, and the hub saw nothing.
- **Path confinement.** OK.
  - `%2e`, `%5c` and `%2f` are refused.
  - Only `info/refs` GET with upload-pack or receive-pack is served, plus the two POSTs with git's Content-Type.
- **The extraHeader's scope.** OK. It is scoped to the forwarder base. A fetch from another server carried no Atrium
  header.
- **Bytes untouched.** OK.
  - The proxy streams with `FlushInterval: -1`.
  - `MaxBytesReader` caps the body and is reported as a git error.
  - The hub's refusal reaches git as the hub sent it.
- **The `atrium_git_push` push-URL check.** It can be bypassed. See M1.

## Stage 2 (D)

`StableHubURL` keeps the 0.0.0.0, empty and `::` → 127.0.0.1 mapping that stage 2's L4 asked for. `setAgentAddr` now
forces 127.0.0.1 too, which is right for a card on the same machine. One edge remains (L5).

## Mutants

| Mutant | Result |
|---|---|
| Drop the `Live` check in `Check` | caught (`TestAWrongRevokedOrOtherCardsTokenIsRefused`, `TestACardLosesItsTokenWhenItIsNoLongerRunning`) |
| Drop the outside-code check in `hubGitEnv` | caught (`TestACardThatRunsOutsideCodeGetsNoGitTokenInItsEnv`) |
| Drop the `X-Atrium-Card-Token` Del | caught (`TestInACardEnvGitPushHubLandsWithTheCardInThePushLog`) |
| Drop the card and chain Dels, and turn Set into Add | **survived** (M2) |
| Weaken the `InheritedTaint` `GIT_CONFIG_VALUE_` filter | **survived** (L3) |
| Drop the revoke in `awaitExit` | a launch test timed out after 10 s, which is not a clean catch |

## Mediums

### M1: `atrium_git_push` trusts the clone's config, so a push can go elsewhere and the token can go to a proxy

`HubPushRemote` accepts a remote when `git remote get-url --push <name>` starts with the forwarder's `hub/` URL. That
command prints only the first pushurl. Both probes below changed only the card's own `.git/config`.

- **Two pushurls.** `hub` had the forwarder as its first pushurl and `http://127.0.0.1:<other>/evil.git` as its second.
  - `get-url --push` shows only the forwarder, so the check passed.
  - `git push` pushes to every pushurl. The forwarder landed the branch, and then the other server received
    `GET /evil.git/info/refs?service=git-receive-pack` and the push.
  - No token went to the other server, because the extraHeader is scoped.
- **`remote.hub.proxy`.** This overrides `-c http.proxy=`, which is all the runner's hardening clears.
  - With `remote.hub.proxy=http://127.0.0.1:<proxy>` set, the proxy received
    `GET http://127.0.0.1:<port>/git/hub/.../info/refs?service=git-receive-pack` with `X-Atrium-Card-Token` in plain
    text.
  - `http.<url>.proxy` in the clone's config does the same.

A card with a shell has its own token and its own network, so for that card this adds nothing. `atrium_git_push`,
though, is the push "with no shell". A card that can edit files but cannot run commands can:

- turn the room into its way out to any server;
- send its own token to a listener of its choosing.

Test-plan II step 8 also promises that a redirected push URL is refused.

Fix:
- **Check every pushurl.** Use `git remote get-url --push --all <name>`, and require every line to match. Refuse more
  than one.
- **Take the proxy out of the clone's hands.** Run `PushToHub` with `-c remote.<name>.proxy=` and
  `-c http.<forwarder base>.proxy=`, next to the `http.proxy=` already there.
- **Or push to the URL directly.** Push to the explicit forwarder URL, not the remote name, with
  `-c url.<base>.insteadOf=` and `pushInsteadOf` cleared. That makes the clone's remote config irrelevant.
- Add a test for each of the two probes above.

### M2: the test that a client's own card headers never reach the hub cannot fail

`TestAClientsOwnCardHeadersNeverReachTheHub` adds the client's `X-Atrium-Card` and `X-Atrium-Card-Chain` under the bare
`http.extraHeader`. The card env already sets the scoped `http.<forwarder base>.extraHeader`.

- Git's URL matching uses the most specific match for a key and drops the less specific ones. So git never sends the
  test's headers. I checked this against a raw listener: only the token arrived.
- The forwarder's Del and Set are never exercised. Dropping the Dels and turning Set into Add still passes.

Fix: set the two client headers with `ExtraHeaderKey(f.base)`.
- I tried this. The test then passes against the real code and fails on the Del-and-Add mutant with
  `X-Atrium-Card reached the hub as ["evil-card" "C1"]`.
- A unit test on `Rewrite` with a hand-built request that already carries both headers would also do.

## Lows

- **L1: "hash held" is half true.** `hubGit.issued` keeps every live card's plaintext token in the room's memory so
  `atrium_git_push` can reuse it.
  - Say so in the security design, where it says "the room keeps only a hash".
  - Or let the room's own push carry a room-internal credential that the forwarder accepts only from inside the
    process.
- **L2: no test for the hub's double-value refusal through the forwarder.** Once M2 is fixed, add one: Del dropped and
  Set turned into Add should be refused by the hub's `identity.go`.
- **L3: the `InheritedTaint` `GIT_CONFIG_*` filter is untested.** Weakening it survives. Add a setup-script env test
  that asserts no `GIT_CONFIG_VALUE_*` gets through.
- **L4: a revoke race on resume.** A resumed card gets a new token at launch, before the new runner is registered.
  - If the old runner's `awaitExit` runs in that gap, `get(taskID)` is nil and it revokes the token that was just
    issued. The card then comes up with a token the forwarder refuses.
  - Revoke only the token that run was launched with: keep it on the runner, or compare before deleting.
  - The revoke mutant's 10 s timeout may be this timing. Make the test deterministic.
- **L5: `setAgentAddr` forces 127.0.0.1.**
  - Before this change, a room whose agent listener was bound to a LAN address only had its git clone URL point at
    that address.
  - Now a forced 127.0.0.1 is not listened on, and card git and clone both fail.
  - Map only the wildcard addresses, as `StableHubURL` does, and keep a specific bind as it is.

Atrium-Verdict: hold ef406235..3ad9d658
Quality: a careful token design that is scoped, checked in constant time and revoked by card state, and the forwarder
refuses before any link traffic. The room's own push and one test still trust what the card controls. m1mini commits
are unsigned.
