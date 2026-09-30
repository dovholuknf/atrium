- **A card has an address made of names.** The hub and every room serve the board page on `/alias/<alias>`,
  `/room/<room>` and `/room/<room>/<name>`, and the phone page on `/m/alias/<alias>` and `/m/room/<room>/<name>`. The
  server never resolves the name, the page does, through the handle-addressed routes. A wrong shape is a 404 page
  naming the shapes that work. Nothing on the board reads these yet (u-new-card-urls U1). Hub side now, room side at
  the next room restart. (u-new-card-urls R3)
