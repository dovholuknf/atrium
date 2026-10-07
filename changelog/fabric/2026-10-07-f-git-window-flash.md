# git and the other background commands no longer flash a console window

- On Windows the hub and the rooms start every git child without a console window, so gitsync's constant `git` runs no longer flash a window on the operator's desktop. Item f-git-window-flash.
- The same goes for the other commands nobody watches: git from the card and PR routes, `ziti edge enroll`, `taskkill`, the overlay setup, `opencode` and the agent `--help` probe. Runner terminals, editors and terminals opened from a card still show.
- Still open: `git http-backend`, which the hub and the rooms start through Go's CGI handler for a fetch they serve, has no hook for this and can still flash.
