- A board stream opened on the hub right as the last one closed streams its room's events: the reconcile run on its
  way out no longer cancels the pump the next run started. The hub logs a room's event stream starting, stopping and
  failing. The link tests close their clients before the front, so a failed stream test fails in 30 s instead of
  holding the package to its 20 minute timeout. link-stream-reopen.
