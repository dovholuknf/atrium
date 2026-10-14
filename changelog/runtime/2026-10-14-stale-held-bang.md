The held `!` chip is recomputed from the messages actually still pending every time the board reads it, so it no
longer outlives a delivered message. r-bug-stale-held-bang.
