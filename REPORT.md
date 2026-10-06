# u-card-colors-on-hub

The hub now owns `card_colors`: stored in the hub setting table (`hubstore.HubCardColors`/`SetHubCardColors`), seeded once, served in `hubSettingsBody` (ALL view), saved through `saveHubCardColors` in fanout.go with validation and a `settings` broadcast. A room-scoped view keeps the room's own. Seed and validation moved to the new `internal/cardcolors` package, shared with `internal/api`.

themes.js untouched: `repoColorsHave` runs after the settings read, so `repoColorsMoveBrowser` runs against the hub once it answers with `card_colors`.

Tests: new link, hubstore and cardcolors tests pass. Headless repoColors and bootClean pass. Failures `TestReposIsOpenLikeGrowlsAndExactlyShaped` (link) and `TestTheWalkerLaunchSetAndClear` (api) also fail on base.
