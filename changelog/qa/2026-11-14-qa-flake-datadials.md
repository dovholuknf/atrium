# TestTheHubFlagsARoomWhoseDataDialsFail no longer spends its control slot on a failed dial

The test's `onlyFirst` dialer treated the first dial as the control connection whether or not it connected. On Windows
a runner short of ephemeral ports failed that first dial, so the control connection was refused too, the room never
attached and the wait for `Starved` timed out. The dialer now counts only a dial that connected. Test only, no product
change.
