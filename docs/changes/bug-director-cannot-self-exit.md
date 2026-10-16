## Test plan

## @LETTER@. A director may exit itself

### @LETTER@1. Self exit

1. Launch a session tagged `atrium:director`.
2. From it, call `atrium_exit` with no card and confirm the runner exits.

### @LETTER@2. Another director

1. From a session, call `atrium_exit` with the card of a different `atrium:director` session, with and without force.
2. Confirm both are refused and the target keeps running. The operator's exit from the board still works.
