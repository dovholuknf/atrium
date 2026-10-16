- `GET /_hub/doc-debt` says, for each counted doc item that has a landed sha and each stale page it names, whether that page
  changed on the hub's mirror of claude/main since the feature landed, with the newest such commit's sha and date, as
  `changed` on the item. The hint is read on demand, remembered in memory, and left out when the mirror or git cannot
  answer. It closes nothing, and an item filed by hand has none. m-doc-debt-from-features.
