# One board view per worker worktree, at <name>.localhost

- The hub serves a separate copy of the board for each live card whose worktree holds `internal/api/web/index.html`, at `http://<worktree-folder>.localhost:7778/`. The card's alias answers too. The API, rooms and data are shared; only the static files differ. Design: docs/rnd/board-views-design.md.
- Off unless the hub is started with `--board-views <root>`. Only worktrees under a root, on the hub's own machine, are served. Files open through `os.OpenInRoot`, so a link out of the tree is refused. An unknown `*.localhost` name is a 404 and never the root board.
- Every `build` answer (`/v1/health`, the merged health, `/_hub/health`) is the view's own hash, rehashed at most every two seconds. A view tab reloads when its worktree changes, and never on another view's or the root's.
- A view comes from the cards the rooms announce and is never stored. It goes when its card ends or its worktree is removed.
- `GET /_hub/views` lists the views. A card's menu has "open its board view" when its worktree has one.
- `edge.LoopbackHost` counts `*.localhost` as loopback (RFC 6761).
- The live hub's args in `scripts/live/live-common.ps1` add `--board-views D:\worktrees`. That takes effect on the next hub deploy.
