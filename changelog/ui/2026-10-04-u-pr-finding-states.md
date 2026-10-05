# A finding can be accepted, and a PR is reviewed when every finding is posted or dismissed

The walk drawer has a new state, accepted, for a finding you agree should be raised but have not posted yet (`y`). The board now says posted for done and dismissed for skipped, and the header and the pulls row read `N of M: a accepted, p posted, d dismissed`, where N counts posted and dismissed. Accepted and deferred findings are not reviewed yet. Keys are `d` posted, `s` dismiss, `f` defer, `u` back to open and `Enter` copies the comment and opens the line. At phone width every state is a button.
