# bug-git-push-skips-hub-for-shared-repo

## Cause

`GitPush` in `internal/daemon/hubremote.go` returned success without pushing when the hub remote was another room's
forwarder on this machine and `visibleLauncher` found a launcher sharing the object store. It answered "nothing to
push: <launcher> sees this commit already". That assumes the launcher is the only reader. A branch meant for a worker
on another room has to be held by the hub, and `atrium_git_url` answered "not found" for it.

## Fix

Push anyway, to THIS room's forwarder. Removing the skip alone would turn success into a refusal, because
`hubPushTarget` refuses a hub remote that is not this room's forwarder, and it stays strict for everyone. So when
`PointsAtOtherForwarder` is true, `GitPush` calls the new `gitsync.PushToHubByURL`, which pushes by URL to this room's
forwarder for `HubNameOf(dir)`, the path a clone with no hub remote already takes. The shared `.git/config` is never
written. If `HubNameOf` answers "" the push fails with that reason, it is not skipped. When the launcher shares the
repository the report gains the note "<launcher> sees this commit already on disk".

## Test plan

Integration test `TestGitPushReachesThisRoomsForwarderWhenTheHubRemoteIsAnotherRoomsAndTheLauncherSharesTheRepo` runs
git's http-backend as this room's forwarder over a bare repository. It asserts the branch lands there, the hub branch
is recorded, the report carries the note and not "nothing to push", and `remote.hub.url` in the shared config is
unchanged.
