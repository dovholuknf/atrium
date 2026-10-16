`atrium room remote-access` no longer writes its switch into a directory no room lives in. A `--dir` that holds no room
certificate is refused with an error naming it (and the running room's directory, when one is known), where it used to
answer "on" for a door nothing would open (sg3). Every answer, for `on`, `off` and the status form, now says which
directory it read or wrote, and notes when the running room on this machine uses a different one. With no `--dir`, the
running room's recorded directory is taken first, then the default, and the error says where it looked. Item
f-remote-access-wrong-dir.
