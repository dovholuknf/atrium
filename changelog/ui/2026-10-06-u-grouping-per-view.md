# Every tab keeps its own grouping and sort

Choosing `by pile` on the stack no longer changes the terminals, and the other way round. The board, the stack and the
terminals each keep their own grouping on or off, group mode, group order and typed group code, and each keeps its own
sort. All of it survives a reload.

- A tab that has not been changed yet starts from the grouping you had saved before, so nothing is lost.
- The pills on each tab and the folded terminal tray show that tab's own setting.
- The grouping section of settings has a tab selector at the top. It opens on the tab you came from, and the pickers
  below it set the tab chosen there.
- The stack's sort is now remembered across a reload. The board's and the terminals' already were.
- Your named groups and their colours are still shared by every tab, because they describe the cards and not a view.
