A worker launched in a git worktree whose checkout has a `hub` remote gets a worktree-scoped `remote.hub.url` pointing
at the launching room's own forwarder, so it can push when rooms share one main checkout. The shared url is untouched.
Item: worktree-hub-url.
