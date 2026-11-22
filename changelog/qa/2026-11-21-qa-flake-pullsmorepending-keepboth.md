# pullsMorePending: keep both not found

macOS CI board-1 failed pullsMorePending at the click on the "keep both" button, which never resolved for 5s.

The unit now waits on the real state: the ask is open with a keep both button, and when it is not it reads the walk
again (a dismissed ask is asked again on a read). A failure prints whether the dialog is open, its title and its buttons.
