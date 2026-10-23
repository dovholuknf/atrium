The watch-paths test changed the address before the watcher had read its first list, so no change was seen and nothing was pushed: it now waits for the watcher to read and tick first (q-watchpaths).
