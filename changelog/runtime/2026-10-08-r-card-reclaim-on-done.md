A worker's done now closes its card on every door. `atrium_report` and `atrium finish` already did. An `atrium_say`
to its launcher that starts `done <sha>`, which is what the launch text tells a worker to send, now does too, with no
second notice to the launcher. A card tagged `atrium:keep-open` or `atrium:investigation` is not exited by its own
done report. The launcher's `atrium_cull` is now the word that the work is merged and deployed: it reclaims the
session, the worktree and branch, BRIEF.md, a scratch directory that is empty without it, and the card's inventory,
and the card and its history stay. It refuses a card an agent did not launch, a card whose work is not done, and a
kept-open or investigation card. A merge alone no longer culls: `merged_cull_grace` is off until the operator sets
it. Item r-card-reclaim-on-done.