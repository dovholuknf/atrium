The automatic new-context cycle no longer starts while you have a part-written line or pressed a key in the last couple of
seconds. It waits on the same gate a peer's message does and starts on the next check. On the board, a cycle that
arrives on the task stream holds typing at once, the line you had typed is kept in the compose box and typed back
after the cycle, and a list read that began before the cycle can no longer lift the hold. (bug-new-context-cycle-over-human-typing)
