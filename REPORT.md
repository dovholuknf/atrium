# f-control-git-push

A card on any room now has `atrium_git_push` and `atrium_git_url` on its own room's stdio control server. A push from
a worktree of the atrium mirror clone, or from a clone made by hand with no remotes, lands on the hub. The launcher
chose option A: write all of it, prove on m1mini what m1mini alone can prove (the ziti push and git_url's hub-only
fallback), and report. The hub is deployed from sg4 afterwards, and the two atrium pushes get proved after that.

## What changed

- **One implementation for both servers.** `link.GitDoor` (internal/link/git_door.go) is both tools: it checks the
  branch, finds the caller's card, asks the room to push and gives the hub's URLs on the room's forwarder. The
  hub's control MCP (`c.gitDoor()` in git_hub.go) and the room's `atrium control` (internal/cli/control_git.go)
  each fill in only how they find the card and how they reach the board. `link.AddGitTools` registers both tools
  with the same descriptions.
- **atrium_git_url from a room.** The room asks its hub over the git link kind, the only route a room has to the
  hub, at `GET /_hub/git/url` (link `serveLinkLookup`, which answers `gitsync.Hub.Lookup`). The board route is
  `GET /v1/hub/git/url` (daemon `handleHubGitURL`, gitsync `AskHubLookup`). A hub older than the route answers
  404, and the room then reads the hub store's advert itself (`HubOnlyAnswer`): hub branches only, main included,
  with a note that no room was asked. The answer is then rewritten onto the room's forwarder (`ForCard`), the same
  way the hub's own tool does it.
- **A clone with no hub remote.** `gitsync.PushToHub` takes the hub name of the clone (`HubNameOf`: origin's forge
  URL first, otherwise where the clone sits, `<scm root or git root>/<host>/<owner>/<repo>`, read through a
  worktree's common git dir). When neither `hub` nor `atrium-hub` exists, it pushes to the forwarder URL for that
  name, given to git as a URL. **Nothing is written to the clone's config**, which keeps the rule that atrium does
  not touch an operator's clone without a yes. A clone whose `hub` points somewhere else is still refused.
- **The adopted mirror takes work branches.** The hub's store copy of atrium is an adopted mirror, and it refused
  every push. It now takes any `refs/heads/*` except its integration branch (claude/main), `main` and `claude/main`,
  from cards and the operator alike. Tags and anything outside refs/heads are still refused (`mirrorRefuses`). The
  advert is offered, so `git push` gets as far as the rules.

## Why the mirror takes work branches, and not a separate namespace or the forwarder

The mirror pass force-fetches only `+refs/heads/<integration branch>`, and a room's sync fetches only claude/main. A
work branch in the mirror's store is therefore never overwritten by the pass and never pulled into anyone's
checkout. It is just a branch the hub holds, which is exactly what `atrium_git_url` lists and a card fetches. The
refs the pass owns are still refused, so a push can never fight the pass.

- A separate namespace (say refs/work/*) would need its own lookup, advert and fetch rules, and every reader would
  have to know about it. `git fetch <url> <branch>` would stop working for exactly these branches.
- The room's forwarder accepting the push would hold the branch on the room, not the hub. It would be gone when the
  room is offline, and it is the work-in-progress path atrium_git_url already says a card cannot fetch.

## Tests

- internal/cli/control_git_test.go runs the server `controlServer()` builds over mcp in-memory transports against
  a fake board:
  - both tools are listed;
  - git_push pushes the caller's own card and takes only a plain branch;
  - a refusal comes back in the room's words;
  - there is no card without ATRIUM_AGENT_NAME;
  - git_url asks the room, puts the hub's URL on the forwarder and gives a room source no URL;
  - an older room is called older by both tools.
- gitsync:
  - TestAskHubLookupTakesTheHubsAnswer and TestAskHubLookupReadsTheStoreOfAnOlderHub (the 404 fallback, every
    branch including main, a missing repo, a bare name);
  - TestAHandMadeCloneWithNoHubRemoteIsPushedByTheURLItsPlaceNames;
  - TestHubNameOfReadsOriginFirstAndSaysNothingOutsideARoot;
  - TestAnAdoptedMirrorTakesWorkBranchesAndNothingThePassOwns.
- `go build -o build.claude/ ./...` is clean.
- `go test ./internal/...` with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG unset: gitsync, link and cli pass.
  - With the default macOS TMPDIR, ptyhost and some daemon tests fail on `bind: invalid argument`: the unix socket
    path is too long.
  - With a short TMPDIR these still fail, and **all of them also fail on claude/main e2ed7864** in a throwaway
    worktree: api TestTheWalkerLaunchSetAndClear (/private/tmp vs /tmp), daemon TestNoTestHereCanReachALiveRoom (a
    Windows claude path in this machine's settings), TestGlobalAutoSurvivesAReopen (SQLITE_BUSY), a few ptyhost and
    hostterm socket tests, ptyhost TestIdleExit and TestProbeNeverEvicts.
  - The daemon tests that failed only in the full run here (TestIdleParkWorkersFirst and others) pass when run
    alone.

## Proof on m1mini

The build of 4ab3c171 was stamped and installed at ~/.local/bin/atrium. The old binary is kept at
~/.local/bin/atrium.pre-f-control-git-push.

### The room restart

- Only the m1mini room was restarted, not the hub.
- I asked u-term-debug-and-lag, which was working, to reach a stopping point first.
- I restarted the room the same way the hub-asked restarter does: a detached
  `atrium room --restart-after 4s --dir ~/.atrium/room --http 127.0.0.1:7781 --agent 127.0.0.1:7777 --db ~/.atrium/atrium.db`,
  then an interrupt to the old room.
- The room came back as pid 59360, running the new binary (lsof shows the same inode as the installed file).
- Atrium resumed the supervised runners that were mid-turn.
- Afterwards I messaged each supervised worker again: u-term-debug-and-lag, f-new-openwiki-zrok and u-pin-tests.

### Ziti: a clone made by hand, with no remotes at all

- A proof card, p-ziti-push, was launched with cwd
  /Users/claude/git/github/openziti/ziti-worktrees/embedded-controller-spike-2, a prompt and no brief.
- The worktree's own untracked BRIEF.md is unchanged (same sha1 before and after).
- The card's atrium_git_push came from the room's stdio control:

```
{"branch":"claude/embedded-controller-spike-2","card":"01a10ceb-7e40-793b-abe9-7e69cbf73111","report":"To http://127.0.0.1:7777/git/hub/github/openziti/ziti.git\n*\trefs/heads/claude/embedded-controller-spike-2:refs/heads/claude/embedded-controller-spike-2\t[new branch]\nDone"}
```

- The name github/openziti/ziti came from where the clone sits under ~/git, because it has no origin.
- Afterwards the ziti clone still has no remotes and no hub or extraHeader config.
- atrium_git_url, from this card on the room's stdio control, against the old hub, which answers 404 to
  /_hub/git/url, so this is the store-advert fallback:

```
{"branch":"claude/embedded-controller-spike-2","branches":[{"name":"claude/embedded-controller-spike-2","sources":[{"sha":"bd983a42c67f28d0d5b2d15de4e6d7665bffebe2","source":"hub","url":"http://127.0.0.1:7777/git/hub/github/openziti/ziti.git"}]}],"note":"only the hub's store was read (the hub is older than the lookup a room asks it for), so no room's work in progress is listed","repo":"github/openziti/ziti","state":"found","text":"fetch it with: git fetch http://127.0.0.1:7777/git/hub/github/openziti/ziti.git claude/embedded-controller-spike-2 (finished, on the hub)"}
```

This is bd983a42c, on the room's forwarder. Before the push, the same lookup answered `not found`.

### Atrium: waits for the hub deploy

atrium_git_push claude/r-context-cycle from this worktree reaches the hub, which is still the old build, and gets its
old refusal:

```
the push did not land:
fatal: remote error: atrium: github/dovholuknf/atrium is a mirror the hub keeps in step with a checkout, so a push cannot land there. fetch it, or push under another repository name
```

atrium_git_url for it answers `not found` from the fallback.

Once the hub runs this branch:
- push claude/r-context-cycle (fb62c770) and claude/r-context-nudge-handoff (7b3ba864) with atrium_git_push from a
  card in this clone;
- look both up with atrium_git_url, which will then come from the hub's own /_hub/git/url, with room sources too.

Nothing on m1mini needs to change for that.
