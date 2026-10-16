Launcher: minion-orchestrator-errand-runner-and-wo-2@claude-sg4 (atrium_say it; one line per message).

TASK: backlog item bug-room-ziti-dial-unbounded (atrium_backlog get). Body:

Postmortem, m1mini room: link stayed "attached" over ziti:atrium-hub but every data dial timed out, once a minute, "room looks attached and serves nothing". CPU ~2 cores, 6397 fds, 640 MB. Board launches and hub ssh-proxy to the room timed out. Dump grouped by creator: 1322 link.(*Room).open dial goroutines (room.go open), ALL on one link.Ziti and one ziti ContextImpl, blocked in sdk-golang posture.(*Cache).Evaluate on the cache mutex (AddActiveService from DialWithOptions). 1304 DISTINCT ziti ContextImpls alive plus 2646 http persistConn and 2604 channel goroutines.
Causes to fix (YOUR scope: 1, 3, 4):
1. Room.open starts a goroutine per requested conn with no cap and no in-flight count. The hub asks again on every request that finds the pool short. context.WithTimeout does nothing: ziti Dial takes no context, so a timed-out dial goroutine lives on. Cap in-flight data dials (e.g. n per session, one queue), and skip asks while one is pending.
3. A room whose data dials fail N times in a row should drop and redial the control link (fresh context), not stay "attached and serves nothing" for 10 hours.
4. Test: a fake Dialer that blocks forever; assert goroutine count stays bounded after 1000 hub asks.
Item cause 2 (what creates a ziti context per attempt: ZitiCapabilities, startZitiNative, ladder probes, the SDK) belongs to the sibling worker on bug-room-ziti-goroutine-pileup running at the same time. Do not touch those files.

GATES: (1) First write and commit a short design note docs/changes/bug-room-ziti-dial-unbounded.md (the cap, the in-flight rule, the N-failures redial rule, the test), then atrium_say me "design note ready" and WAIT for my "ACCEPTED". Do NOT build before ACCEPTED. (2) After ACCEPTED, build, test, atrium_done with the sha.

RULES: commit as Clint Dovholuk (configured), one short subject line per commit, no body, no trailers. Do not edit CLAUDE.md. Go builds go to build.claude/ (go build -o build.claude/...). gofmt -l internal/ cmd/ must be empty; go run scripts/check-test-globals.go exit 0 (tests may not assign package vars). Unit tests stay under the 10ms budget (disk, socket or store tests go in *_integration_test.go with //go:build integration). Add a changelog entry and docs/changes/<id>.md. Run scripts/test-touched.sh in ONE foreground call, timeout up to 600000ms, never background/sleep/poll (a 20ms unit-budget miss on a disk test you did not touch under load is not yours: rerun once and say so). Known failing, not yours: TestReposIsOpenLikeGrowlsAndExactlyShaped, TestRealSessionsKeepTheirText, api taskkill tests, TestPublishTaskCoalescesABurstToTheLatestState. Never restart the live hub or rooms, never deploy, do not merge or push. Finish with atrium_done giving the commit sha.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.

`atrium_resources` lists the machines and environments you may use.
