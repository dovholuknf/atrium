# Card colours live on the daemon, and the hidden repo table is gone

The default colour and the colours by repo are now stored on the daemon like the skin, so a phone or a second browser
wears the same ones, and a change in one open board shows in every other at once.

- A fresh daemon starts with 17 visible `provider/org/repo` entries, written once. Delete one and it stays deleted.
- The table that coloured any repo by its last path segment is removed. A fork now has no colour until you add its
  full `provider/org/repo`, or the default applies.
- Colours this browser kept before are merged into the daemon's on the next load, this browser's winning for the same
  repo, and then removed from the browser.
