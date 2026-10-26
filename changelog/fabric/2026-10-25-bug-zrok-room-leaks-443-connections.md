A room on zrok no longer opens a new ziti context, with its own router connections on :443, for every dial. Dials and
the hub's listener share the process's one context for the zrok environment's identity, so a redialling room holds a
fixed number of sockets. bug-zrok-room-leaks-443-connections.
