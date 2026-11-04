The cursor refit test no longer reads the large viewer's frames before the replay has arrived.

The attach path writes `caps`, then the `size` frame, then the replay body (`internal/daemon/attach.go`). The test waited
on the size frame and then checked for the cursor move at once, so the check could run between the two writes. The
test now waits for the cursor move inside the same `hostWait`. The product order is intended, so no product change.
