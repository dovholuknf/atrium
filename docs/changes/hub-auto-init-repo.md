Docs: none, a fix that makes the hub do what `atrium rooms git init` did by hand

## Test plan

## @LETTER@. A card's first push makes the repository

### @LETTER@1. Push to a repository the hub lacks

1. On a card, `atrium_git_push` a branch of a GitHub repository the hub's store does not hold.

**Expected:** the push is taken. The hub made the repository and seeded `main` from the forge's default branch.

### @LETTER@2. Ask for its URL

1. On a card, call `atrium_git_url` for another repository the hub lacks, with no branch.

**Expected:** the answer is found and the hub made the repository.

### @LETTER@3. A forge that cannot be reached

1. Push, or ask, for a repository that does not exist on the forge.

**Expected:** the refusal still says to ask the operator to run `atrium rooms git init`, and nothing is made.
