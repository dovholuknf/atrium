- **A new context finishes, or ends with a reason, and never sticks on a step.** A card at its prompt whose Stop hook
  never landed is treated as between turns (looks-idle flag, or an idle frame on a silent pty), so the capture starts
  at once. The 409 names the step: "stuck on step N of 3 (step) for D".
- **A room restart ends a cut-off new context with the reason on the card.** Runs in flight are journalled in the
  `new_context_journal` setting. At startup each one becomes a failed chip reading "the room restarted during step N
  of 3", plus a card event. The hub proxy holds none, so a hub restart strands nothing.
- **Deploys wait for a new context under way.** The hub answers `GET /_hub/new-contexts`. The one-click deploy
  refuses with a 409 naming the card, and `deploy-hub-only.ps1` and `deploy-batch.ps1` wait through
  `Wait-NewContextsDone`, then exit 0 with "nothing changed" on timeout.
- **Board and phone typing is refused during a new context.** The attach drops the bytes, still acks a paste with
  `in-done`, and sends an `in-refused` frame. ctrl-c and the shell terminal are unaffected. Room side, needs a room
  deploy. Item r-clear-vs-restart.
