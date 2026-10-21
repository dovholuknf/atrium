A runner that was already running keeps its git token across a room restart. The room stores a SHA-256 of each token it hands a card
in its database and puts that back when it starts, so `git fetch hub`, `git push hub` from a runner
the pty host kept alive work at once, where they were refused until the card was relaunched. A card that ends or whose
token is revoked loses its stored token too. Room restart.
The store holds only the hash, never a usable token, and `atrium_git_push` pushes for such a card with a token only the room holds, so the runner's own is not replaced.
