A runner that was already running keeps its git token across a room restart. The room stores each token it hands a card
in its database and puts it back when it starts, so `git fetch hub`, `git push hub` and `atrium_git_push` from a runner
the pty host kept alive work at once, where they were refused until the card was relaunched. A card that ends or whose
token is revoked loses its stored token too. Room restart.
