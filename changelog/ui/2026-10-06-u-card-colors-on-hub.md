Card colours now live on the hub, like the board skin. The hub's board never showed any because the hub borrowed a room's settings, which had no `card_colors`, so every card was plain. The hub stores the table in its own settings, seeds the 17 entries once, serves it in the ALL view and takes the save, then tells other open boards to repaint. A room-scoped view keeps the room's own table. Colours saved in a browser's old `atrium.repoColors` move to the hub on the next load.

Test plan:
- Open the hub board (:7778). Cards wear their colours, and `GET /v1/settings` has `card_colors` with 17 repos.
- Change a colour in settings, save, and a second open board repaints without a reload.
- Delete an entry, restart the hub, and it stays deleted.
- A key like `Ziti` is refused with a 400.
- With an old `atrium.repoColors` in localStorage, reload: it merges into the hub's table and the key is removed.
- Pick one room in the room picker: its settings carry that room's own `card_colors`, not the hub's.
