## Test plan

## @LETTER@. A room finds a hub that moved

### @LETTER@1. Every address dead

1. Stop the hub, or block its port, on a room whose room.json and transports.json name only its address.
2. Open the room's board. The rooms pane says the room cannot reach the hub at any known address and lists each
   address it tried.

### @LETTER@2. A new address with no restart

1. On that room, run `atrium room hub <new host:port>` for an address where the hub answers.
2. Within one backoff the room log says its hub addresses changed on disk, and the room attaches without a restart.
