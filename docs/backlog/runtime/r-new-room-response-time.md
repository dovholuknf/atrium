# r-new-room-response-time: room stats measure how fast a room answers

Asked by clint 2026-10-01, after the m1mini move could not be judged on responsiveness.

## What is wrong

Room stats report CPU, memory and tokens. Nothing says how long a room takes to answer, which is what a person
feels as lag. The benchmark that compared sg4, sg3 and m1mini (drift p99 104ms, 26ms, 5ms) was a one-off run, not
something atrium keeps.

## What is wanted

A latency series in `GET /v1/room/stats`, next to the CPU and memory series, that atrium measures itself. Candidates:

- Hub to room round trip on a cheap endpoint, sampled on the stats tick.
- Scheduling drift inside the room process: how late a timer fires against when it was due. This is what the
  benchmark measured and what tracks keystroke lag on Windows.
- Permission hook round trip, from the agent listener's side.

Pick the one that tracks what clint feels. Drift is the default, since the benchmark already showed it separates
the three machines.

## Done means

The board shows the series per room, and a loaded room reads worse than an idle one.
