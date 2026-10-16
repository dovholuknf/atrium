Fixed a flaky link test, TestDataFollowsTheControlWhenTheHubAdvertisesAnotherAddress. It counted every dial to the stale advertise, including the room's own path probe that marks that rung down on the board, so it failed about one run in eight. It now counts only data dials. No product change.
Docs: none, test fix
