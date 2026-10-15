The remote deploy now waits for what matters. After `atrium stop` it waits for the old room to release its store, not
only its port, so the new room no longer exits "store held by another process" (sgg). It waits up to 120s, says what it
is waiting on, and reports the pid still holding the store if the budget runs out. After the swap it waits up to 600s
(one constant, `$ReattachBudget`) for the room to reattach to the hub, polling the hub and saying "waiting for <room>
to reattach (Ns of Ms)", so a ziti reattach that takes minutes (sg3) is no longer reported as failed. The log says how
long the reattach took. Items bug-deploy-store-held and bug-deploy-reattach-wait.
