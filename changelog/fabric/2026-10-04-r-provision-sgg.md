Adding a Windows room with no git, no winget and nobody logged in now takes one `provision-room.ps1` command, where sgg
needed four hand steps. `room-git.ps1 init` installs MinGit with no admin (latest release or `-GitVersion`, SHA256 checked
against the release's published hash, into `~\.local\git` and the user Path). When the Interactive logon task cannot start
the room (last result 267011) provision falls back to `room --detach`, says `start warn` with the reason, and goes on to
the git step, the room's tools, auth and smoke instead of stopping. The smoke on sgg failed only because that early stop
skipped the `mcp` step, so claude had no atrium_say or atrium_report. A smoke that does not report now prints the card's
own last screen lines and names that fix. The clone `room-git` makes carries `atrium.clone=made`, and the room's git sync
no longer asks about guarding `origin` on a clone that has none.
