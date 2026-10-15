Items stage 3: pools. Resident cards tagged `atrium:pool:<name>` are fed items one at a time each, queued in order, with
`atrium_item submit`. A card with a blocked item counts as busy. A pool card's `atrium_done` may carry `learned`, which
atrium appends to LEARNINGS.md in the card's directory, capped at 50 entries or 20 KB with an alert. Part of the items work.
