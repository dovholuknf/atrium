The room link can run over several transports in order. The hub listens on every `--transport` it is given and
tells rooms its order (`atrium rooms transports direct,ziti`). A room falls back to the next transport when one does
not answer, and moves back up after two good probes. `atrium room transports` sets a room's own ziti identity and
order. The board's transport badge names the transport a room is on, in the warning colour when it is a fallback.
room-transport-order.
