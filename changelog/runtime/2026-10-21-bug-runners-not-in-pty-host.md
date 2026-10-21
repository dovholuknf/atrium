A launch now always tries the pty host, even after an earlier host failure was remembered, so a runner no longer lands in
the room's own process because of an old failure. When the live start cannot read whether it runs inside a job, it starts
through WMI instead of assuming it does not (bug-runners-not-in-pty-host).
