The pty host's `in_job` (and `/v1/preflight`) now says true only for a job that kills or limits it, so an ambient job
that only allows breakaway no longer blocks `atrium stop` or shows a warning. Item: bug-wmi-start-lands-in-a-job.
