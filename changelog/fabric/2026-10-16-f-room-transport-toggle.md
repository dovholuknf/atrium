A room can turn its ziti or zrok transport off and on without a restart, from the board's rooms page or the new
`atrium_transport` tool. Off closes the overlay's context and stops redialling, and the choice is kept in
`transports.json`. A room refuses to turn off its last working path to the hub. A failing overlay controller is now dialled
on a backoff (1 s doubling to 1 min) and its context closed on each failure, where before a refusing controller was hit
about 10,000 times a minute by each room. A transport whose dials keep failing is flagged on the rooms page. Room and hub
restart.
