- **The open integration test waits for the disk measure.** The measure after an open runs once the answer has gone, so
  the test read the inventory before it was in and saw 0 on a slow macOS run. It now polls until the disk is counted or
  the measure's own budget ends. (mac-ci-fixes)
