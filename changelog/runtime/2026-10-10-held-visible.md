An fyi say to a card that holds its notices, such as the orchestrator, is no longer parked on the card where nothing read
it. It is typed when that card's turn ends, or at once if the card is idle, and the sender is answered `queued` with
`when: "done"`. `held` now means only that the hub or a room is not answering. A worker's done, blocked and progress
reports are always queued for the launcher's turn end, and `launcher_told: true` is true only when that is queued. Fyis
already parked on a holding card are delivered once at the next start. Automatic notices that are not says, such as a
silent stop, stay held.