## Test plan

Test plan @LETTER@: an unscoped launch finds the room that has the directory. Run on sg4 with the hub and every room on this build (rooms: claude-sg4, sg4-control, sg3, m1mini). gwt is unchanged. claude-sg4 and sg4-control share a machine, so both have every sg4 directory.

1. In `D:\git\github\dovholuknf\dotagents` run `gwt claude .`. gwt shows its picker listing exactly claude-sg4 and sg4-control, not sg3 and not m1mini. Pick one and the session opens there with no "is not a directory" failure.
2. Stop the sg4-control room, then run step 1 again. It launches on claude-sg4 at once with no picker, because a room that is not attached is not asked and only one room is left on this machine with the directory. Start sg4-control again.
3. In a directory that exists on no room's machine, the hub answers 422 `no room has the directory ...` and gwt shows no picker. Check by hand from the hub machine: `curl -s -i -X POST localhost:<hubport>/v1/launch -d '{"harness":"claude","cwd":"C:\\nowhere"}'` answers 422 with an `error` and no `rooms` key.
4. A directory that exists only on sg3 (not on sg4), launched from `gwt` on sg4, answers 422 and not a picker with sg3 in it. The same launch with `curl -H "X-Atrium-Room: sg3" ...` still lands on sg3 exactly as before.
5. A launch that names a room, a `room~card` task_id or a card is unchanged: no pause, and it lands where it did before.
6. `curl -s "localhost:<roomport>/v1/launch/cwd?path=D:/git"` on a room answers `{"exists":true,"dir":true}`, a file answers `{"exists":true,"dir":false}`, and a relative path or a missing one answers `{"exists":false,"dir":false}`.
7. A launch posted to the hub from another machine (not the hub's own loopback) is checked against every room rather than only sg4's, so it routes to whichever single room has the directory, or lists those that do.
8. There is no default room: with two matches the picker always appears, and naming a room skips it.
