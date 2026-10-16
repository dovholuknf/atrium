# bug-git-push-skips-hub-for-shared-repo

## Cause

`GitPush` in `internal/daemon/hubremote.go` returned success without pushing when the hub remote was another room's
forwarder on this machine and `visibleLauncher` found a launcher sharing the object store. It answered "nothing to
push: <launcher> sees this commit already". That assumes the launcher is the only reader. A branch meant for a worker
on another room has to be held by the hub, and `atrium_git_url` answered "not found" for it.

## Fix

Push anyway. The shortcut stops being a skip: the push runs as for any other hub remote, and when the launcher shares
the repository the report gains a note, "<launcher> sees this commit already on disk", so the caller still learns it.
No new parameter: a push that lands is harmless for a launcher-only branch, while a skipped one breaks every other
reader, and a flag the caller must remember is how this bug happened.

## Test plan

Integration test `TestGitPushIsASuccessWhenTheHubRemoteIsAnotherRoomsAndTheLauncherSharesTheRepo` now expects the push
to be attempted rather than skipped: the report no longer says "nothing to push" and the branch is recorded for the
hub (`atrium_git_url` finds it).
