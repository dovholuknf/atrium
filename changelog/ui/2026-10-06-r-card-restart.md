# Restart an agent from its card

A card's right-click menu and its details popover have `restart`. It types the runner's exit keys into the terminal
atrium owns (claude: ctrl-d twice), waits for the process to leave, then starts it again on the same card, resuming the
same conversation with the same model, effort, arguments, environment and lean setting. The card keeps its id, alias,
tags and history.

- A runner that ignores its exit keys is left running and the error says so. Restart no longer closes its terminal or
  kills it behind your back. Terminate is still there for that.
- A session atrium does not supervise shows the menu entry dimmed with the reason, and has no button in the details.
