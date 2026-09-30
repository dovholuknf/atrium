- **A room deploy no longer interrupts anybody mid-call.** `atrium_deploy request` records why a director needs
  its room redeployed and tells the deploy owner once. The owner calls `start`, and every other card's next tool
  call is refused with "end your turn and wait" while messages between agents are held (the operator's are not).
  `wait` blocks until the room is quiet. The room lifts its own hold when it comes back and types one wake line into
  each held card: deploy done, or the deploy did not take when the same build came back. `cancel`, the 60 minute
  expiry, and a lift on the board wake them by message instead. Launches onto a held room are refused, and
  keep-alive, idle park and the merged cull skip held cards. The owner is the hub setting `deploy_owner`, set with
  `PUT /_hub/deploy-owner` from the hub's machine. Unset, a request is refused. Room side live at the next room
  restart, the tool and the owner endpoint at the next hub deploy. (r-deploy-hold)
