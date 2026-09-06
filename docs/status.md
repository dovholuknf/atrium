# Where everything stands, 2026-09-06

`docs/backlog.md` is what is LEFT. This is where each thing stands, including the things that are finished and
therefore no longer in the backlog at all.

When the two disagree, the backlog is right about what is open and this is right about state.

## The statuses

| | Meaning |
| --- | --- |
| **done** | built, and the operator has looked at it and been happy |
| **needs you** | built and tested, never seen by the operator. This is the pile that matters |
| **designed** | written up and reviewed, no code |
| **decided** | a rule was settled and enforced, with nothing left to build |
| **open** | in `docs/backlog.md` |

---

## Needs you: built overnight on 2026-09-06, none of it seen

Everything here has tests and passes `scripts/ci.sh`. What none of it has is a human looking at it.

| Item | What to do | Proved how far |
| --- | --- | --- |
| **A popped window closes on exit** | pop out, type `exit`, watch it go | cause found. NOT watched in a browser |
| **A paste scrolls all the way down** | scroll up, paste twenty lines | reasoned and guarded, not watched |
| **CI runs on Windows too** | it is green here. Push and see | four platform bugs fixed, matrix untested on a runner |
| **zrok panel locked while sharing** | start a share, try to change a setting | tested |
| **Public and private are two checkboxes** | tick both | five tests, including the upgrade from the old `mode` |
| **The account token is a password field** | look at the setup box | visual, unverified |
| **Configuration export** | `GET /v1/config/export` | run against a COPY of your live database. Clean |
| **Configuration import** | `POST /v1/config/import`, dry by default | eight tests. It cannot erase your zrok token |
| **`atrium room`** | two rooms are reporting in now. See `DEMO.md` | proved on cdzrok and cdaws, over ziti |
| **The rooms pane** | runners tab, bottom | drawn from real remote data |
| **An OIDC login on the published board** | gear, `who may open it` | eleven tests. NO PROVIDER HAS BEEN CONFIGURED |
| **Tab completes a path** | type `/d/git/` and press Tab | two invariants. NOT watched in a browser |
| **`back it up` in the gear** | save the config, then read it back | the read-back is a dry run first |
| **`atrium ask`** | run it in any session | six tests. Stopped and working are told apart |
| **A popped window stops announcing itself** | put one on a second monitor | it was testing focus, not visibility |
| **Both CSS nits** | hover a group heading on `paper` | the hover direction was wrong on every light skin |
| **OpenZiti service for the board** | `C:\Users\claude\atrium-ziti\README.md` | atrium bound it, two live terminators |

## Needs you: older, still unseen

| Item | What to do |
| --- | --- |
| **A share cannot reach a shell** | the only security claim here. Share a session, then try `?kind=shell` |
| **Reserved per-card shares** | share a session, restart, see the same address come back. BLOCKED by the zrok 500 |
| **Directory picker bounded to roots** | the one change that took a capability away. Press browse |
| **Grouping expression refused daemon-side** | `curl` a `group_by` at `/v1/settings`, expect 400 with a reason |
| **`make release`** | five platforms build. The Linux binary now runs: it is what the rooms are running |
| **A share that dies alerts** | hard to stage: take the network away while one is up |

## Done, and you have signed off

Terminal search, after `allowProposedApi` was found to be the cause. The shell beside a wedged agent. Twenty
board skins and the skin panel. Bracketed paste. The wind-down announcement. The peer MCP tools, proved by
starting an agent, briefing it and having it answer back. The sharing flyout and the share progress dialog.

## Designed, not built

| Item | Where |
| --- | --- |
| Starting a card from a ticket, an issue or a PR | `docs/scm-design.md`, reviewed twice |
| Permission requests from a room | `docs/federation-design-v2.md`. Stage one of the rest is built |
| Multi-tenant atrium | `docs/backlog.md` item 3, and two of its objections moved overnight |

## Decided, nothing left to build

| Item | The rule |
| --- | --- |
| The grouping expression | an expression may be stored where it was typed. Refused at the endpoint, three tests |
| CodeMirror in the board | refused: ES modules across seven packages, no bundle, and the board is one file |
| `atrium install` | removed before shipping. Copying a file is the shallow half of installing |
| A shell is not a runner | second map on the supervisor, so nothing that means the runner sees it |
| A pty never federates | not policy, fact. ConPTY has no reattach |
| Auth never applies to loopback | every hook, the CLI and the MCP server talk to it |
| Atrium owns no credentials | identity is delegated. The password half of the login was refused in review |

## Open

In `docs/backlog.md`, twelve entries, ranked by how good the thing is.
