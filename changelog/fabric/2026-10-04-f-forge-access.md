A room can now say which forge CLI it needs (gh, bb or glab), the host it must be logged in to and the command name,
in the room settings under forge logins and in atrium.requirements.yaml under forges. POST /v1/preflight takes a forges
list and runs the CLI's own status command, never reading a token, and answers ok, not installed, logged out or missing
scope for each, with the scope named. A failure raises a forge-access alert on the board whose message says what to run
on which room, for example "gh is not logged in on sg3: run `gh auth login --hostname github.com` on sg3". Nothing is
checked until something asks, and nothing polls. Daemon.RaiseForgeAccess(tool, host, detail) raises the same alert for
a runtime failure. Atrium stores only the host and a bare command name. Item f-forge-access.
