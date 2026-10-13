`atrium_launch`, `atrium_launch_worker` and `atrium launch --item` start an item: a task whose only routing is to tell
whoever wanted it when it is done. An item worker is never told it has a launcher, gets its opening prompt from atrium,
loses `atrium_say`, `atrium_peers`, `atrium_task` and `atrium_report`, and the room refuses any say from it. Its result
is one notice per generation. A launch without the flag is unchanged. Room-side, no migration (items stage 1).
