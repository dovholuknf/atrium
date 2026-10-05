# f-control-git-push (IN PROGRESS, handoff)

Launcher (orchestrator-sg4-control@sg4-control) chose option A: write all of it, prove on m1mini what m1mini alone can
(ziti push, git_url's hub-only fallback), commit, report `done <sha>`. Hub deploy is done by sg4 afterwards, then the
atrium pushes get proved.

Done so far (builds, gitsync + link tests pass):
- gitsync/receive*.go: adopted mirror takes card work branches; refuses its integration branch, main, claude/main,
  tags, non-heads for everyone (mirrorRefuses, Store.mirrorOf). Test TestAnAdoptedMirrorTakesWorkBranchesAndNothingThePassOwns.
- gitsync/hubpush.go: PushToHub(..., name) pushes a clone with no hub remote to the forwarder URL for name;
  HubNameOf (origin URL, else <root>/<host>/<owner>/<repo>). daemon GitPush passes d.cloneRoots().
- gitsync/lookup.go: answer() shared by Lookup and HubOnlyAnswer; AskHubLookup (link /_hub/git/url, 404 -> store advert).
- link: Hub.GitLookup + serveLinkLookup on git kind; atrium_run sets h.GitLookup. link/git_door.go GitDoor shared;
  hub's handlers use c.gitDoor().
- daemon GET /v1/hub/git/url (handleHubGitURL), api HubGitURL.
- cli/control_git.go: stdio registers both via link.AddGitTools.

TODO: cli/control_git_test.go (stdio tests through controlServer with mcp.NewInMemoryTransports), tests for
AskHubLookup fallback + serveLinkLookup, full go test ./internal/..., build.claude, changelog
changelog/fabric/2026-10-05-f-control-git-push.md, install + room restart m1mini (park workers first, say them after),
prove ziti push via a card in /Users/claude/git/github/openziti/ziti-worktrees/embedded-controller-spike-2 and
git_url fallback, final REPORT.md, one atrium_report done <sha>.
