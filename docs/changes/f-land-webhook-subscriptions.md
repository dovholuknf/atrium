Docs: none, the design doc docs/fabric/land-webhook-design.md is the reference until a docs item takes it

# f-land-webhook-subscriptions

Stages E1 and E2 of `docs/fabric/land-webhook-design.md`: a hub event endpoint, subscriptions, delivery on the CI
hook's loop, the `atrium_subscribe` tool and command, the daily existence check, the two scripts, and the board's
Hears line.

## The land script edit, for the human

`scripts/land-claude-main.ps1` is untracked, so it is not changed here. Remove `-NotifyTo` and the `atrium tell`
block, and after each outcome call the script with the same facts:

```powershell
pwsh -File scripts/atrium-event.ps1 -Kind land -Repo dovholuknf/atrium -Branch $Target -Outcome pushed `
    -From $before -To (git rev-parse $Branch) -Commits $count
pwsh -File scripts/atrium-event.ps1 -Kind land -Repo dovholuknf/atrium -Branch $Target -Outcome not-pushed `
    -From $before -To (git rev-parse $Branch) -Reason $why
pwsh -File scripts/atrium-event.ps1 -Kind land -Repo dovholuknf/atrium -Branch $Target -Outcome stopped `
    -From $before -To (git rev-parse $Branch) -Reason $why
```

Pass `-From` and `-To` on every outcome. An event repeats as a duplicate when kind, repo, branch, outcome, from and to
all match, so two `stopped` events with empty from and to would collapse into one. The script prints one line and
exits 0 whatever happens, so a hub that is down never fails a land.

Then the orchestrator calls `atrium_subscribe` once with `kind: land`, `repo: dovholuknf/atrium`, `branch: main`.

## Things worth knowing

- `POST /v1/tasks/exist` is listed in `cardRoutesNotCards` (`internal/api/cardnames.go`), or the card-name resolver
  answers it as a card called `exist`.
- The hub migration is `0033_hub_events`. Three position-counting migration tests each moved by one.
- The CI hook's deliverer is now `Deliverer`, shared with events. A 404 from a room is `Gone` and deletes the
  subscription.

## Test plan

## @LETTER@. A landing reaches the cards that subscribed

### @LETTER@1. Subscribe from a card and land

1. In a card's session, run `atrium subscribe land --repo dovholuknf/atrium --branch main`, then `atrium subscriptions`.
2. On the hub machine run `pwsh -File scripts/atrium-event.ps1 -Kind land -Repo dovholuknf/atrium -Branch main
   -Outcome pushed -From aaa1111 -To bbb2222 -Commits 3`.

**Expected:** the script prints `event: 1 subscribers`, and the card receives one fyi from `land@<hub>` naming the
repo, branch and commit count. Running the same command again prints `event: duplicate` and delivers nothing.

### @LETTER@2. The board's Hears line

1. Open the card's details on a hub board.
2. Click **+ land**, leave the repo and `main` as filled, and subscribe. Then remove the chip with its cross.

**Expected:** the chip `land <repo> main` appears and then goes. A board with no hub shows no Hears line.

### @LETTER@3. The hub is down

1. Stop the hub, then run the script from step 2 of the first test.

**Expected:** one warning line, exit code 0.

### @LETTER@4. A parked card

1. Park a subscribed card without `wake`, land three times on the same branch, then resume it.

**Expected:** it receives only the newest landing. A pending delivery older than 24 hours is dropped.
