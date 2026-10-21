The hub's three ziti binds (the board, the room link and the CI hook) are switches in the gear, under "hub ziti
transports", where each was a start-up flag. Each shows bound, binding, failing with the last error, or off, with its
service and identity, and turns off and on without a restart. Off closes the listener and, when no other transport that
is on shares the identity, closes its ziti context, which stops the traffic to a controller that is struggling. A bind
that fails retries on a backoff from 30 seconds to 10 minutes. The room link's ziti refuses to turn off while a room
reaches the hub only over it, and names the room. A flag still given at start only seeds the value, and the first change
in the gear replaces it. Set from the hub's machine only. Hub restart.
