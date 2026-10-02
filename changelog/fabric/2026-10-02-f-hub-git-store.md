The hub has its own git store (hub forge stage 1, part 1). `atrium rooms git init <url>` makes a bare repository
under the hub's `git.store`, seeds `main` once from a public repo and makes an empty one for a private repo.
`GET /_hub/git/repos` lists the store for the board, and `git.store` and `git.create_on_push` are hub settings.
Nothing is pushed yet. Item f-new-hub-git-store.
