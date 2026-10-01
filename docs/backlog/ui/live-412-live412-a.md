# live-412 live412-a: /m redirects and home, on the running hub

Hub http://127.0.0.1:7778, Playwright Chromium, 412x891, deviceScaleFactor 2.625, isMobile, hasTouch, Android Chrome UA.
Taps are `touchscreen.tap`, the pull is CDP `Input.synthesizeScrollGesture` with touch. Scripts and raw output are in
`D:/tmp/live412/live412-a/` (item1.js, item1b.js, item1c.js, item2.js, item2b.js, item2c.js, `*.out.txt`). Nothing was
typed into or approved on any card. Deploy was never touched. Data moves while the tests run (the live412 testers are
working), so counts differ by one or two between runs, and every check compares against `/v1/tasks` fetched in the
same page.

Screenshots are under `docs/backlog/ui/img/live-412/`.

## Item 1, redirects

PASS 1a root `/`: a fresh phone tab on `/` ends on `/m/` (replace, one history entry).
`img/live-412/01-root-redirect-m.jpg`

PASS 1b `/?why=1`: the banner reads `redirect: going to /m/ (coarse=true any-coarse=true touch-points=1
shortest-side=412)`, stays about 4 s, then the tab lands on `/m/`. `img/live-412/02-why-1.jpg`

PASS 1c `/alias/fabric` goes to `/m/alias/fabric` and opens the fabric card (thread, composer, open terminal).
`img/live-412/03-alias-fabric.jpg`

PASS 1c `/room/claude-sg4/fabric` goes to `/m/room/claude-sg4/fabric` and opens the same card.
`img/live-412/04-room-card.jpg`

PASS 1c `/room/claude-sg4` goes to `/m/`, stores `atrium.room=claude-sg4`, and the home shows the chip `room: claude-sg4`
with 35 needs and 151 all. `img/live-412/05-room-only.jpg`

PASS 1c `/alias/rnd`: three cards hold the alias (two on claude-sg4, one done on sg4-control), only one is live. It
opens the live claude-sg4 card, which is the "1 live, any done" row of the design table, not the chooser.
`img/live-412/06-alias-rnd-clash.jpg`. The chooser itself (two live cards on two rooms) was not reachable, no such pair
exists, so that branch is SKIPPED.

PASS 1c `/alias/test-nope-xyz` shows "no card called test-nope-xyz", an `all cards` link, and the would-work list as links
(`pr-ziti-4397@claude-sg4 (@pr-ziti-4397)` and so on). `img/live-412/07-alias-missing.jpg`

PASS 1c `/?land=claude-sg4~01a0eadc-c358-725f-bbb0-f06231c08fe1` goes to `/m/` and opens the fabric card, the hash is
consumed. `img/live-412/08-land.jpg`

PASS 1d desktop opt-out: the `desktop board` link on `/m/` is visible, a tap goes to `/`, sets
`sessionStorage atrium.m.desktop=1` and `data-opted-out=1`. A reload stays on the desktop board. `/alias/fabric` in that
tab stays on the desktop page. `/?why=1` says `not redirected, this tab chose the desktop board`. A new tab in the same
context sends `/` to `/m/` again. `img/live-412/09-m-home-desktop-link.jpg`, `10-desktop-board.jpg`, `11-optout-why.jpg`

PASS 1d way back: in the opted-out tab the bell drawer shows `phone view` (71x46 px), a tap goes to `/m/` and clears the
flag. `img/live-412/12-optout-drawer.jpg`

FAIL 1e (low) "tap to enable sound" pill covers the page. In every fresh-context screenshot a floating pill sits over
the top of the content, on the home it covers part of the "Needs you" heading (05-room-only.jpg), on a missing-card page
it sits beside `all cards`, on the desktop board it covers a card at the bottom (10-desktop-board.jpg). Repro: open any
`/m/` page in a new context with no prior tap. A fresh context is also what a first visit on a phone looks like. Guess:
the unlock hint in `internal/api/web/m/js/bell.js` or `growl.js`. It disappears after a tap, so cosmetic.

## Item 2, /m home

PASS 2a counts: tabs read `needs you 49` and `all 195`, the page drew 49 and 195 rows, the model count was 49 and
`/v1/tasks` held 195 cards with none archived. Needs-me-only on the all list gave 49, all of them in the needs set.
`img/live-412/13-home-needs-default.jpg`

PASS 2b needs order: default is oldest wait first (first ages 6d 2d 2d, last 6m 4m 3m) and the order is monotone by
`since`. Newest is the exact reverse (first ages 3m 4m 6m). `img/live-412/14-view-panel-needs.jpg`,
`15-needs-newest-first.jpg`

PASS 2b all order: sections Working 5, Waiting 27, Idle 163. Newest and oldest each hold within every section against
`last_activity_at` from `/v1/tasks`. One apparent violation in Working in both runs was two running live412 cards whose
activity moved between the page draw and my fetch, so it is data motion, not a sort fault. The two lists keep separate
orders: setting the needs list to newest left the all list at newest default, and setting all to oldest left the needs
list at newest. `img/live-412/16-all-newest.jpg`, `17-all-oldest.jpg`

PASS 2c group on needs: room gives claude-sg4 34, m1mini 3, sg3 6, sg4-control 6 (sum 49, no card under the wrong
room), project gives 25 headings summing to 49, none gives no headings. `img/live-412/18-needs-group-room.jpg`,
`18-needs-group-project.jpg`, `18-needs-group-none.jpg`

PASS 2c group on all: room gives 151, 10, 22, 12 (sum 195), project sums to 195 with a `no project` bucket of 1, none
gives Working, Waiting, Idle. `img/live-412/19-all-group-room.jpg`, `19-all-group-project.jpg`, `19-all-group-none.jpg`

PASS 2d hide finished: 195 drops to 33, the expected `195 minus done and dead` is 33, the chip reads `162 hidden by
filters. show`. Tapping the chip shows all 195 with `showing 162 hidden by filters. hide them again`, and tapping again
hides them. The tab count stays `all 195`. `img/live-412/20-all-hide-finished.jpg`, `21-all-hide-finished-revealed.jpg`

PASS 2d needs me only (all list): 49 rows, chip `146 hidden by filters`, 195 minus 49 is 146.
`img/live-412/22-all-needs-me-only.jpg`

FAIL 2d hide subagents hides the directors. Repro: `/m/`, view, all tab, tap `hide subagents`. 82 of 195 rows go.
Among them are the live directors `fabric`, `ui`, `rnd`, `runtime`, `review` and `u-paste-spinner`, `u-ready-spam`
(status needs-input). Measured with `/v1/tasks`: `fabric` has tags
`atrium:context-ceiling, atrium:subagent, origin:agent`, `ui` the same, the orchestrator has
`atrium:hold-notices, atrium:subagent, orchestrators, origin:agent`. The orchestrator stays visible (`orchestrators` is
in the exempt list). The live directors do not, because the exempt list matches the tag `atrium:director`, and only the
old done cards (`merge`, `ui`, `rnd`, `review`, `runtime`, tagged `atrium:director`, `directors`) carry it. On the needs
tab, hide subagents leaves 22 of 49 with chip `27 hidden by filters`, and the 6 needs-you cards tagged atrium:director
stay, but the live needs-input directors do not. Running aliased cards (live412-a to d, scratch) stay visible as
designed. Severity medium: wrong but usable, the cards are one tap from the chip. Guess: `NOT_SUB` and `isSub` in
`internal/api/web/m/js/home.js` lines 148 to 153, the exemption needs a tag that live directors really carry (for
example `atrium:context-ceiling`) or the alias set. `img/live-412/23-all-hide-subagents.jpg`,
`24-needs-hide-subagents.jpg`

PASS 2d all filters: needs with hide subagents and hide finished leaves 6 rows, chip `43 hidden by filters`, the view
badge reads 3. Turning each off restores the list. `img/live-412/25-needs-all-filters.jpg`

PASS 2e room chip and hidden count: scoped to claude-sg4 the tabs read `needs you 35` and `all 151`, and `/v1/tasks` has
151 cards on that room. The row room label is dropped under scope (0 of 35) and shown on all 195 rows when unscoped.
Hide subagents under the scope gives `18 hidden by filters` (35 minus 17). `all rooms` clears the scope, drops the
stored room, restores 50 and 195. m1mini scope gives 3 and 10 (10 on `/v1/tasks`).
`img/live-412/26-room-chip.jpg`, `27-room-chip-hide-subs.jpg`, `28-all-rooms-room-labels.jpg`

SKIPPED 2f pull to refresh on the home. Two real touch pulls from the top of the list (CDP synthesizeScrollGesture,
400 px down, touch source) at scrollY 0 changed nothing: no reload (a window marker survived), no `/v1/tasks` request,
no indicator. The home has no pull handler of its own. Only the card view does (`pullInit` in `m/js/card.js`, reloads
the page), and the `#m-pull` element lives inside the card. The home relies on the browser's own pull to refresh
(`body.m { overscroll-behavior-y: auto }` in `m.css`), which emulated Chromium does not implement, so it cannot be
tested here. Needs a check on the real phone. `img/live-412/29-home-after-pull.jpg`
