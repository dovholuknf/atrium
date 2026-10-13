# lifecycle-audit: paste-a-link phases on claude/main

Tip audited: 4676606c. The atrium_backlog tool was not available to this worker, so the acceptance points come from
the plan table (section 14), section 13 and Interview Q7 of docs/rnd/card-lifecycle-design.md, plus the item names
cited in code comments. Each piece was checked by file and function with git grep. Commits are the ones that added the
files. Not run: tests, and whether the pulls tab and launch dialog call the verb (not checked in the web js).

| item | verdict | commits | missing |
| --- | --- | --- | --- |
| r-open-verb-pr | done | e416fd8f (open.go, openroute.go) | |
| r-open-cli | done | 95081f76 (cli/control_open.go: atrium_open; cli/open.go: atrium open) | |
| r-card-inventory | done | 294b5891 (store/resources.go, api/resources.go, DiskBytes on card and pulls row) | |
| r-inventory-sweep | done | 7407b79d (api/sweep.go: Sweep, getSweep, postSweepClose) | |
| r-open-support-kinds | partial | 16cdd0a6 (api/openkinds.go: classify, openOther, scratchDir) | The hub claim for these kinds is only a room-local `link:<key>` tag scan; the text rules of section 11 were not checked. |
| r-card-procs-ports | partial | e588c3cf (api/cardports.go, cardproc, cli/control_own.go: atrium_port, atrium_own) | There is no atrium_proc_start tool; the process registry is a separate item. |
| f-card-overlay | done | 8748e20a (api/cardoverlay.go: postOverlay, overlayUp, overlayIdentity, overlayTunnel, overlayDown) | |
| r-card-idle-cost | partial | 294b5891 (disk on the card) | There is no idle age or reminder (Q7 ruled out a per-card reminder) and no tokens or dollars on the card. |
| f-card-move-inventory | not started | none | Nothing in daemon/move.go, store/move.go or moveroutes.go touches card_resources; MoveResources only renames pending to card. |
| r-gwt-calls-atrium-open | not started | none | gwt is not in this repo and no change is seen here; gwt still makes its own worktree and card. |
| r-open-nonpr-key-claims | not started | none | The hub still has only pr_claim (hubstore/prclaim.go, PRClaim, keyed on PRs); there is no link_claim with a card column. |

Counts: done 5, partial 3, not started 3.
