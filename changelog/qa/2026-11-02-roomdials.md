# TestTheRoomDialsTheAddrItsSwitchNames flake

- Test only: when the first dial gets a 101 because a parallel test bound 127.0.0.1 at the probed port, the test picks a
  new port and retries, up to 10 times, then skips.
- PROVEN: the test passes locally with `-tags integration` (it sits behind that tag).
- INFERRED: the CI failure came from that race. It was not reproduced.
