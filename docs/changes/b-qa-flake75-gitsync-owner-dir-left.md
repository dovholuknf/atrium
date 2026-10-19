Docs: none, a fix that makes the code match what the docs already say

## Test plan

## @LETTER@. A push-made repository that took nothing leaves no directory

### @LETTER@1. Refused push

1. With create on push, push a branch whose object the hub lacks to a repository that does not exist.

**Expected:** the repository and its owner directory are both gone.
