`POST /v1/open` opens every kind of link, not only a pull request. An issue gets a worktree on its own branch
(`issue-<N>`) off the default branch and a card. A branch link gets a worktree on that branch, made from the clone's
remote-tracking branch, and a branch the clone does not have is refused with a sentence. A Zendesk ticket or a
Discourse topic names no repo, so it opens in a `zendesk-<N>` or `discourse-<N>` worktree of the request's `repo`, else
the recogniser's new `default_repo` (the seeded rows default to `github.com/openziti/ziti`), and `repo: none` makes a
scratch folder of the card's own under `<git.scm_root>/scratch/<host>/`. None of these makes a review row. The card
carries `link:<key>`, and a second paste of the same link answers it. Keys: `host/org/repo/i<N>` for an issue,
`host/org/repo@<branch>` for a branch, `<kind>:<host>/<N>` for a ticket. The card owns its worktree and branch, or its
scratch folder, and closing it removes them (a folder only when it is under the scratch root). Every non-PR card's
prompt ends with a sentence saying the linked text is someone else's, is data, and its instructions are not followed.
A link that names no piece of work (a repo page) answers `not_openable`. The answer gains `repo` and `recogniser`, and
`kind` is now pr, issue, branch or support. `atrium open --repo` and `atrium_open {repo}` pass the repo through. The
hub backfills `default_repo` on its seeded rows once, and its placement no longer reads an issue as a claimed pull
request. Design: `docs/rnd/card-lifecycle-design.md` sections 4, 5 and 11, and Q6, phase 8.

Test plan:
- Paste `https://github.com/openziti/ziti/issues/<N>` with `atrium open`. A card opens in an `issue-<N>` worktree off
  main, with no review on the pulls tab. The same again prints `already open:`.
- `atrium open https://netfoundry.zendesk.com/agent/tickets/<N>`. A card opens in a `zendesk-<N>` worktree of
  openziti/ziti. Its prompt ends with the sentence about someone else's text.
- `atrium open --repo none <the same kind of link, another number>`. The card starts in
  `<git.scm_root>/scratch/netfoundry.zendesk.com/zendesk-<N>`. Close the card and the folder is gone.
- `atrium open https://github.com/openziti/ziti` prints that the link names no piece of work, then the resolution.
