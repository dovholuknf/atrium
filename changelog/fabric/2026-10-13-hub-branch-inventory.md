- **Closing a card deletes the claude/* branches it pushed to the hub.** Each branch a card pushes is an inventory row,
  freed by close with a delete push the hub takes only for a branch that card pushed. A merged branch goes without
  asking, an unmerged one is a keep or delete ask, and a failed delete is reported without stopping the close. The hub
  now takes a delete push from the owner (the operator may delete any branch but main). Tags, main and claude/main
  stay undeletable. Hub and room code. (hub-branch-inventory)
