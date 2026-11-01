A flaky pty host test, TestARunnerLaunchedAfterAFailedDialStillGoesToTheHostOnceItIsUp, now waits for the host to
answer a probe before it launches the second runner. No product change. Item q-hostterm-after-dial.
