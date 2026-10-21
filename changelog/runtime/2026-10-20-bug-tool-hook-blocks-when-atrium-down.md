Atrium's hooks no longer hold a session on "running PreToolUse hooks" when the room is down, restarting or slow.
Every hook dials for at most a second, the permission gate stops retrying after four seconds, and the default address is
127.0.0.1 so there is no name lookup. Item bug-tool-hook-blocks-when-atrium-down.
