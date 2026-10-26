- A close waits for the card's processes to exit before it removes the worktree, and retries a held folder with
  backoff. A worktree still held is retried later in the background, and the board says "not cleaned up yet" while it
  is on disk. (f-close-locked-worktree)
