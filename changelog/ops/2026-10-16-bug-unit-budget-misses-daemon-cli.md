The unit budget no longer fails daemon and cli changes: the git and filesystem tests that tripped it moved to
integration files, and the held-mark timer test uses an injected timer. Item bug-unit-budget-misses-daemon-cli.
