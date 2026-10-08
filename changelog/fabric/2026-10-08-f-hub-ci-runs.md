# The hub reads CI for rooms

- New control tool `atrium_ci`, on the hub control MCP and on `atrium control`, read only. A session asks for `runs`, a `run`'s jobs and steps, a failed job's `log`, a run's `artifacts`, or one `artifact` downloaded on the hub. The hub runs `gh`, so no room does and nobody has to paste `gh run view --log-failed`.
- A log is bounded while it is read (the last 400 lines by default, within 64 KiB). Artifacts download to the hub with a 200 MiB cap.
- A missing or expired `gh` login answers the command to run on the hub. Bitbucket answers `not supported on bitbucket`. Item f-hub-ci-runs.
