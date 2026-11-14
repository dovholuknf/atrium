# TestALaunchWithNoBriefCarriesTheLineOnItsPromptOnce flake

Windows CI run 43463076404 failed at launch_brief_integration_test.go:179 with `doubled: ""`.

Cause: `promptSeenBy` treated an existing file as the prompt. The shell's `> out` creates the file empty before printf
writes it, so a loaded runner let the test read it empty.

Fix: wait until the file is non-empty. No timeout widened and product code is unchanged.

PROVEN: 20 of 20 passes after the fix (`-count=20 -race`, integration tag).
INFERRED: the failure before the fix, from the code order. It did not reproduce locally, so it was not run red.
