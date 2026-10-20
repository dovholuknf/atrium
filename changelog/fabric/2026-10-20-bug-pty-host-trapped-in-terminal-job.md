- A room or pty host started from a terminal tab no longer stays in the terminal's job object: the live start scripts and
  the room's own detached start go through WMI when they are inside a job, so closing the tab cannot end the atrium.
- With `pty_host` on, startup waits (bounded, logged) for the host before it reopens runners, and a runner that still
  lands in the room's own process is logged loudly and noted on its card.
- A refused stop says the host sits in a job that refused breakaway and what to do, and `/v1/preflight` now reports the
  host's build. Item bug-pty-host-trapped-in-terminal-job.
