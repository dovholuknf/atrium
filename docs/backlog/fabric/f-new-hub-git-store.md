# f-new-hub-git-store: the hub's own git store, with `main` seeded once

Status: HELD (the pause). Filed by @rnd 2026-10-02 from `docs/rnd/hub-forge-design.md` revision 2, stage 1 (section
3.1). Asked by clint (interview, 2026-10-02): the hub holds its own `main`, and a feature is finished only when it is
pushed there. Owner @fabric. Size about 1 day. Goes with `f-new-hub-receive`.

- An install setting on the hub, `git.store`, defaulting to `<hub atrium-dir>/git`. Repos live at
  `<git.store>/<host>/<owner>/<repo>.git`, bare.
- `atrium hub git init <url>` makes one. For a public repo it fetches the forge's default branch into `main`, once.
  For a private repo with no credential, it makes an empty repo and tells the operator to push `main`.
- Creation on a room's first push, only when the hub setting `git.create_on_push` is on (default off, design
  question 3).
- atrium's existing `git_repos` entry and its `claude/main` mirror are untouched.

Acceptance: design section 7, row `f-new-hub-git-store`.
