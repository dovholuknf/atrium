# Rooms read the forge only through the hub

A room attached to a hub no longer runs gh or bb and no longer fetches from GitHub or Bitbucket. It asks the hub for a
pull request, its diff, its head or an issue, and the hub runs the forge under its own login. The hub fetches a pull
request's head into its store under `refs/atrium/pr/<N>`, and the room reads it from there. A room with no hub still
runs the forge itself.

The hub picks the forge per host from its own `forge.entries`, set at `PUT /_hub/forge`. A missing or logged out CLI on
the hub, or a head fetch the forge refused a credential for, raises one growler on the board that names the hub and
the login to run there, and the next forge answer that works ends it. `POST /_hub/forge/check` checks the hub's logins.

The room's forge settings, `POST /v1/forge/check` and the board's "forge logins" block are gone, and migration 0083
deletes the old `forge.<tool>.host` and `forge.<tool>.cmd` rows. A room preflight with forges on a room with a hub runs
nothing and says the logins are the hub's.

Recognisers get a built-in fetch, `forge pr` or `forge issue`, that reads through the hub with gh's fact names. A room
with a hub refuses a row whose fetch runs gh, bb or glab. A PR worktree the placed room refuses now releases the hub's
claim, so a retry places it again.
