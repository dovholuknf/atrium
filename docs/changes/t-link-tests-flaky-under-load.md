# The link tests run beside each other and finish under load

`go test -tags integration ./internal/link` took 407s on a quiet machine and did not finish in 550s with six busy cores,
the way sg4 timed out at 600s on 2026-10-08. Item t-link-tests-flaky-under-load.

## What was found

The 2026-10-08 hang in the scoped-stream test was already fixed in f00779c3, so the helper there no longer waits
without a bound. Reading the rest of the package for the same shape turned up no other unbuffered send with no
receiver. The time is a different thing. The 713 tests add up to 395s and 98 of them take over a second each. Most of
that is `git` itself. A git process costs 60ms to 170ms on this machine, and one rig builds 30 to 40 of them before
its test says anything. Rigs: `newPassRig`, `newURLRig`, `newGitFixture`, `newPushFix` and with it `newCRRig`,
`newRoomPush`, `newStoreProxy` and `newForgeHub`. They share nothing, each has its own directories and listeners, yet
they ran one after another.

## What changed

- `gitSlot` in `link_integration_test.go`. A rig constructor calls it first. It marks the test parallel and takes one
  of four slots, held until the test ends, so a loaded machine is never given more than four rigs at once. A test that
  builds two rigs takes the slot once.
- `caching`, the hub and room the announce tests stand up, is parallel too. Those tests are mostly waits.
- `TestAConnectionThatWaitedInThePoolStillWorks` waits 12s on purpose and is parallel, so the wait overlaps.

No test was deleted and no assertion was changed. The sleeps that remain are the ones that assert nothing happens in
that time, and a shorter sleep would weaken them. Parallel tests start after the serial ones finish, so the serial
tests that time a bound still run on a quiet package.

## Wall time

| Run | Before | After |
| --- | --- | --- |
| Plain, `-count=1`, 12 cores | 407s | 238s |
| Six CPU burners running | not finished at 550s (685 of 713 passed) | 344s, ok |

## The ten slowest, before

| Test | Elapsed | Why |
| --- | --- | --- |
| TestAChunkedPostGoesThroughForwarderLinkAndCgi | 20.1s | 1500 branches made by fast-import, then a chunked fetch |
| TestAConnectionThatWaitedInThePoolStillWorks | 12.0s | a 12s sleep, on purpose, longer than any header timeout |
| TestABranchTheRoomWouldNotServeIsNeverListed | 8.5s | url rig, about 40 git processes |
| TestABranchOnBothAnswersBothAndSaysWhetherTheRoomIsAhead | 8.2s | url rig, and three more commits |
| TestTheToolReturnsTheEndpointsJSONAndALine | 8.1s | url rig |
| TestStashNotesAndAnUnservedBranchAreRefusedThroughTheHub | 8.0s | url rig |
| TestACardWhoseRoomDoesNotSayWhereItsForwarderIsIsGivenNoURL | 7.6s | url rig |
| TestAHostThatIsNotANameAndAPortIsNotMadeIntoAURL | 7.2s | url rig |
| TestACardOnAnotherRoomIsGivenItsRoomsForwarderAndFetchesThroughIt | 7.1s | url rig |
| TestACardIsGivenNoURLForARoomsWorkInProgress | 7.1s | url rig |

Nine of the ten are git process cost, so the fix is to run them side by side. The chunked post is the only one with real
work in it. The 12s sleep is the test.

## Still serial, and left alone

The tests that time a bound on a hung room (`TestAHungRoomDoesNotStallThePrune`, the pin order, placement) have two
seconds of slack and are left serial, so load cannot trip them. They add up to about 110s.

## Test plan

## @LETTER@. The link tests finish under load

### @LETTER@1. Plain

1. Run `go test -tags integration -count=1 ./internal/link` on a quiet machine.

**Expected:** ok in under 300s, apart from the two known failing tests.

### @LETTER@2. Loaded

1. Run the same command with several cores busy, for example six spinning shells.

**Expected:** ok, and no test hangs on a channel send.
