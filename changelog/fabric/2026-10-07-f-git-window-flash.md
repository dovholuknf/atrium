# git and the other background commands no longer flash a console window

- On Windows a hub or room started with no console (`atrium room --detach`, a self-restart) opened a console window for every git it ran. Every git child gitsync starts is now started without one. Item f-git-window-flash.
- That includes `git http-backend` for each fetch and push the hub or a room serves. It used to go through Go's CGI handler, which allows no such setting, and now runs through atrium's own.
- The same goes for the other commands nobody watches: git from the card and PR routes, `ziti edge enroll`, `taskkill`, the overlay setup, `opencode` and the agent `--help` probe. Runner terminals, editors and terminals opened from a card still show.
