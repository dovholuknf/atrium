# r-new-review-7f4e76c0. Review findings on r-new-item-dependencies (bug)

Status: 1 to 4 fixed on claude/r-deps-review, each with a test. The note for @ui is @ui's. Owned by @runtime. Filed by @review 2026-09-30 after a correctness read of r-new-item-dependencies,
landed on claude/main as 7f4e76c0 (bb24b47d and e9c24dd2). The MED was proven with one scratch test in
`internal/hubstore`, shown below and not committed. @runtime's gate (build, vet, tests of itemgate, hubstore, gitsync,
link and cli) had passed.

Nothing here is HIGH. The change is hub side only, so nothing here touches a room restart.

Checked and fine: every git argument that comes from a caller is validated before it reaches git (`ValidSHA` for
`sha:`, `ValidItem` for items and `live:`, `deptName` for `ready`). `GateMet` records a gate met once, even when two
checks race. The launch refusal runs before `reserveSlot`, so a refused launch holds no slot. A failed git read leaves
a gate open with the reason on the row. A room that is not attached answers the tell with 503, which `tell` retries
for a day as the comment says. `batchHeads` handles a `missing` line from `cat-file --batch`. `atrium_deps` is not in
the worker tool list. The partial unique index lets a met gate be added again.

## 1. MED: a rename can close a loop that add refuses

`internal/hubstore/deps.go`, `RenameItem`. `AddGates` refuses any edge that closes a loop, through
`itemgate.FindPath`. `RenameItem` moves every open gate onto the new name and refuses only the one-edge case
(`item == target`). A longer loop is written without a word.

Proof (scratch test, run at 7f4e76c0):

```go
addGate(t, s, "r-900", item("r-901"))
addGate(t, s, "r-902", item("r-900"))
s.RenameItem(depRepo, "r-901", "r-902", "x") // accepted
```

```
open: r-900 waits on r-902
open: r-902 waits on r-900
```

Now neither item can be launched: `launchGate` refuses both, and neither can land through atrium, so neither gate can
clear. Only a human clear breaks the loop, and the refusal text does not say that it is a loop. `rename` is an agent
verb in `atrium_deps`, so a director can do this by mistake. The case to expect is a slug renamed onto a number that
already has gates.

Fix: in `RenameItem`, after the open gates move, run `openEdges` on the result, and refuse the whole transaction with
`itemgate.LoopError` if any moved item edge closes a loop. The test above, turned into a refusal assertion, covers it.

Fixed as suggested. Every loop the rename can close passes through the new name, so the check follows that name's
edges. A refusal rolls the rename back, the `item_rename` row included.

## 2. LOW: a loop through `live:` is not seen

`openEdges` reads only `kind = 'item'` rows. `live:<item>` is also a wait on an item, and it clears only after that
item lands. `r-900 waits on live:r-901` plus `r-901 waits on r-900` is accepted, and it deadlocks the same way as
finding 1. Fix: take the argument of a `live:` condition as an edge in `openEdges`, and in the loop check in
`AddGates` and `RenameItem`.

Fixed. `itemgate.WaitsOnItem` is the one rule for which gates are edges, and `openEdges` reads the `live:` argument
under its current name. `live:` on the item itself is refused like an item target is.

## 3. LOW: the ready say repeats reasons from earlier rounds

`internal/link/deps.go`, `tellWaiters`, builds the text from `Gates(repo, item, false)`: every gate the item has ever
had, including gates met and told in an earlier round. An item that is gated, cleared, and gated again gets a message
that lists the old reasons next to the new ones. Fix: build the text only from `byItem[k]`, the gates in this pending
tell.

Fixed as suggested.

## 4. LOW: a title without a colon escapes the launch refusal

`itemgate.ItemOfTitle` finds an item only in `<id>: ...` or in a title that is only an id. `r-037 fix the reaper`
and `r-037 - fix` give no item, and the launch goes ahead past an open gate. The tool description says that
`atrium_launch` "REFUSES a worker whose title starts with an item". Fix: take the first field when the title splits at
a colon or at whitespace, and keep `ValidItem` as the test. Or, if the colon rule is intended, say `<id>:` in the tool
description.

Fixed, narrower than the first suggestion. A first field followed by a space counts only when it is a numbered id
(`r-037`, `91`). Any word is a valid item id, so taking every first field would read `fix the build` as item `fix`.
The tool description now says both forms.

## Note for @ui (not a bug in this change)

`/_hub/deps/clear` accepts only a loopback caller, the same as the other hub write routes. So when the Blocked pane
from section 5 of the design is built, a board reached over an overlay (the phone, another machine) cannot clear a
gate. Until then, `curl` on the hub machine is the only way to clear one. The refusal text sends agents to "a human
clears it on the board", and no board button exists yet.
