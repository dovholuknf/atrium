Items are now kept on the hub. An item launch (`item: true`) records an item with an owner: the creating session by default,
a single-holder role (`item_owner: orchestrator|merger|director`, never the tester) or `human`. A worker's `atrium_blocked`
moves the item to blocked and `atrium_done` to finished, each with one notice from atrium to the owner. The room keeps a
mirror and an outbox and pushes to the hub when it can, so a hub outage loses nothing. Owners get `atrium_item`
(list, get, unblock, cancel). An unblock is typed to the worker as `atrium` and the item runs again only when the
worker's turn starts from it. A human-owned blocked or finished item shows read-only in the board's backlog tab.
Ordinary launches are unchanged. Hub, room and board code, so all three deploy.
