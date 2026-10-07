The hub answers `POST /v1/recognise` with no room named from its own rows, so a room on an older build (with an empty
table) no longer answers "nothing here knows what that is". It runs the row's `forge` or `redirect` fetch itself; a row
with a custom fetch command is still placed on a room. When `/v1/open` or a pulls-tab paste is placed on a room that
answers `no_recogniser` for a link the hub's rows know, the hub marks that room deaf for its build and retries the next
one. Recognisers now cover every link gwt opens: a new `discourse-topic-slug` row follows a slug-only `/t/<slug>` link
to the numbered topic (the new built-in `redirect` fetch), Discourse and Zendesk rows take any `*.discourse.group` and
`*.zendesk.com` host, Zendesk defaults to `openziti/ziti-tunnel-sdk-c` as gwt does, and new rows handle GitHub
advisories (and their fork PRs and branches), Bitbucket repos, and GitLab issues and repos (`scripts/recognisers/gitlab.json`).
`github-repo` drops a trailing `.git`. Placeholder resolution, cwd description and the forge fetch moved into shared
code (`store.Recogniser.Resolve`, `store.DescribeCwd`, `forge.Facts`, `internal/linkfetch`).

Test plan: `docs/test-plan.md` IV1.
