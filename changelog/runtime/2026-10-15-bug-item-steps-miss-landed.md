Items that landed and pushed now read landed and pushed: the hub reads them by the builder's commit, and by the item's changelog entry when the commit is not the one on main (bug-item-steps-miss-landed).
A bug built with no design no longer sits at "designed": designed and approved are skipped for it.
Round 2: an item whose build never read as done, or whose row has no branch, is now read by its changelog entry too, so it shows landed and pushed.
