A runner launched by a second room on one machine reports its hooks to that room. The room sets `ATRIUM_HUB_URL` to its
own agent address in every runner it launches, which a hook reads ahead of the machine's address file. A harness that
sets `ATRIUM_HUB_URL` itself keeps its value. (bug-second-room-runner-hooks-hit-first-room)
