# Deploy readiness runs far fewer git processes

A readiness pass on a small repo started 12 git processes cold and 8 warm, about 60ms each on Windows, so the deploy
button read "timed out after 5s reading git" under load. A cold pass now starts 6 and a pass where neither the branch
tip nor the installed build moved starts 1. The answers are the same.
