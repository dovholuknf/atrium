# Worktree audit: one path, one place

SamePath resolves symlinks and Windows short names before comparing, so /var/x and /private/var/x on macOS, or
RUNNER~1 and runneradmin on a Windows runner, match. The audit tests failed on both runners because the audit printed
one form and the test looked for the other. A directory that is gone keeps its name and has its parent resolved.

