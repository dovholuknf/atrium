Rooms can log in to each other over ssh with no hand key copying. `atrium ssh setup` now makes the room's key, publishes
it with the login user (`--user`, default claude, kept by the room) to the hub, and writes the keys the hub relays into
the login user's authorized_keys inside an `# BEGIN atrium` block, and each room refreshes that block every minute, so a
removed room's key goes. `atrium ssh config add` writes the room's login user into its Host block. f-ssh-room-login.
