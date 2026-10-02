# measure-term-switch --hover: "no row" for one card

Low, found 2026-10-02 on the sg4 rerun. With `--hover`, the script's card for the orchestrator's own session on the hub
(the sg4-control card-audit card) printed "no row" in every round: `#term-list .card.tab[data-id="<id>"]` found
nothing for it. Cause not found. Candidates: the card is hidden or filtered out of the terminals list, the row uses a
different id form than the one `/v1/tasks` returns (a hub's `room~uuid`), or the card is a joined session that has no
row. Either the script picks cards the list does not show (it should skip them up front and say why) or the board is
missing a row it should have.

Not blocking: the plain run covers the card, and clint says switching is instant for him. The 180-300 ms "two
frames" the same run showed after a kept switch was a headless software-GL artifact (clint's own switch is instant),
so it is not pursued.
