- `pullsAbsent` waits for the first read of `/v1/prs` to end rather than for "not loading", which is also true before
  the read has begun: it starts when the event stream opens, late on a loaded runner. pulls-first-read.
