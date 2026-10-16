The `item` argument of `atrium_launch` and `atrium_launch_worker` is pinned as a string, the backlog id, by a test, so a schema that calls it a boolean cannot return. (bug-launch-item-schema-boolean)
