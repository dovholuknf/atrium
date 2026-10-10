# A pty host binds under a long home or TMPDIR

A pty host socket path over the 104-byte unix limit (macOS TMPDIR, a long home) failed with "invalid argument". The socket
now moves to a private `/tmp/atrium-<uid>/` when the state dir path is too long, and a path that still cannot fit
says so and names the limit. Two tests that failed only on macOS are fixed too. Item t-macos-suite-red.
