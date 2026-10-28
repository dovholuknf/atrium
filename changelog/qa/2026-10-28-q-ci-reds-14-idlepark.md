# TestIdleParkKeepsStatus no longer races its own dead filing

CI run 40735611428 (macOS) failed "not parked": the test filed the card dead from a goroutine after a 20ms sleep, and
on a loaded runner that landed before parkIdle read the status, so the card was never eligible. The test now files dead
from inside the wind-down (its own idleLeave), so the order is fixed and no sleep is needed. Test only.
