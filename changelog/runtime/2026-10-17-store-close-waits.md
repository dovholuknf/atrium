- The store's Close now lets calls that were mid-operation finish before it closes the database, so the file is
  released when Close returns. A room shutting down, or a test's cleanup, no longer finds the database held by a
  background measure that had just taken the connection. store-close-waits.
