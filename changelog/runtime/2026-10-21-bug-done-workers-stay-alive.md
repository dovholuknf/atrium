A worker's done now says its runner is exiting and why, in the answer and on the card. A launcher whose handle did not
resolve to a card still has its worker exited. A card woken by a say can fetch and push from the first moment
(bug-done-workers-stay-alive).
A card launched with lifetime work, including one the orchestrator launches through the hub, now exits after its done report whoever launched it (bug-done-workers-stay-alive).
