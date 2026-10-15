# process-names spike on sg3 (Windows)

Deployed binary: C:\Users\claude\.atrium\bin\atrium.exe (room running, also atrium.ptyhost.exe). Not touched.
Copy made in %TEMP%\pn-spike, hard link atrium.hub.exe of the copy (New-Item -ItemType HardLink).
SHA256 of deployed, copy and link are all 27FAE62FDE953FA0A2BFD0FCA3A6C7AA4295CAC69C7ADACD4F9E2525500D933F.

Run: `atrium.hub.exe run --no-room --addr 127.0.0.1:17778 --link 0.0.0.0:17779 ...` and, for comparison,
`atrium.exe run ...` on 17788 / 0.0.0.0:17789. Scratch dirs and db. Ports 17xxx-18xxx were free (netstat).

| Run | Get-Process Name/ProcessName | Path | tasklist |
| --- | --- | --- | --- |
| link | atrium.hub | ...\pn-spike\atrium.hub.exe | access denied in this sandbox |
| plain | atrium | ...\pn-spike\atrium.exe | access denied in this sandbox |

So Name follows the link's file name, with .exe stripped (same as the deployed room showing as "atrium", ptyhost as
"atrium.ptyhost"). Task Manager Details shows the image name, the same value plus .exe, so atrium.hub.exe is expected.
I could not look at Task Manager itself.

Listening: 0.0.0.0:17779 (pid of link) and 0.0.0.0:17789 (pid of plain).
Connect from 192.168.1.147 and 127.0.0.1 to both 17779 and 17789: TcpTestSucceeded True for all four.
Note this is a connection from this machine to itself, which Windows often does not filter, so it does NOT prove an
outside host could connect.

Firewall: NOT determined. Get-CimInstance, Get-NetFirewall*, Get-NetTCPConnection fail with CIM access denied, and
`netsh advfirewall firewall show rule` was denied too (needs elevation). I cannot say whether an atrium* rule existed
before or appeared after, and cannot see GUI dialogs. Nothing on stdout/stderr suggested a prompt, and the listeners
bound fine. A real test needs a second machine connecting to 192.168.1.147:17779 versus :17789, or an elevated shell.

Also not determined: Win32_Process CommandLine/ExecutablePath (CIM denied).

Second run, cross-machine (open item, INCONCLUSIVE): atrium.hub.exe on 0.0.0.0:17779 and plain atrium.exe on
0.0.0.0:17789 were held open for about 10 minutes. From sg4 (192.168.102.41, Wi-Fi) a TCP connect to 192.168.1.147 on
both ports failed, but ping to 192.168.1.147 also timed out, so sg4 cannot reach that subnet at all. That is not a
firewall result. No connection from another host was seen on sg3 either. It still needs a test from a host on
192.168.1.x, or a read of sg3's firewall profile and rules from an elevated session.

Incidental: the plain atrium.exe hub on 17789 logged "THE STORE HAS HALTED: table schema_migration already exists"
while the link hub, started at the same moment, did not. Likely a startup race between two hubs started together
(separate db files), not related to the link. Listener stayed up. Not chased.

Cleanup: both processes stopped, no process left with a pn-spike path, scratch folder deleted, repo has no changes
(only BRIEF.md untracked, not committed).

Third run, same LAN (result relayed from sg4's session): atrium.hub.exe (hard link) on 0.0.0.0:17779 and plain
atrium.exe on 0.0.0.0:17789, started 8 seconds apart and held open about 10 minutes. m1mini (192.168.1.119, same /24,
ping to sg3 OK at 3.6ms, port 135 open, 445 filtered) probed 192.168.1.147 with `nc -vz -G 5 -w 5`. Both ports TIMED
OUT, and sg3 saw no connection arrive on either. So inbound is filtered for both names on these spare ports. A new name
is no worse than plain atrium.exe, and neither is allowed on an arbitrary port.

Not determined: whether the live hub's port 7779 has a port-based allow rule (rules could not be read without
elevation), so this does not settle whether the hub needs a per-path rule.

## macOS (m1mini)

Deployed binary: /Users/claude/.local/bin/atrium (a regular file, 69849106 bytes), run by the live room. Not touched.
Copy in /tmp/pn-spike-mac, hard link `atrium-hub` (same inode as the copy), symlink `atrium-room -> atrium`. SHA256 of
deployed, copy, hard link and symlink target are all
e69303309cc190a3f83c07a5b2c69ca161441afd235e813a8bf547be8ab8ef74.

Run with HOME pointed into the scratch folder so nothing under ~/.atrium was read or written. Ports 17777-17791 were
free (lsof). Hubs: `run --no-room --addr 127.0.0.1:177x8 --link 127.0.0.1:177x9` with scratch `--atrium-dir` and
`--atrium-db`. Rooms: `rooms add` against the spike hub (with `--atrium-board-addr` on the spike port, never the
default 7778), then `room join <string> --mtls 127.0.0.1:17779 --isolated` with scratch `--dir`, `--db`, `--http` and
`--agent`. Both rooms joined and listened.

| Run | `ps -o comm` | `ps -o ucomm` (kernel name) | args argv[0] | name seen |
| --- | --- | --- | --- | --- |
| hub via hard link | ./atrium-hub | atrium-hub | ./atrium-hub | atrium-hub (`sample` header: `Process: atrium-hub`) |
| hub, plain | ./atrium | atrium | ./atrium | atrium |
| room via symlink | ./atrium-room | atrium | ./atrium-room | atrium (`sample`: `Process: atrium`, path .../atrium) |
| room, plain | ./atrium | atrium | ./atrium | atrium |
| live room (deployed) | /Users/claude/.local/bin/atrium (cut to 16 chars in the default column) | atrium | full path | atrium |
| live pty host | /Users/claude/.local/bin/atrium.ptyhost | atrium.ptyhost | full path | atrium.ptyhost (`sample` agrees) |

Findings:

- A hard link gets its own name on macOS: the kernel name follows the file name it was exec'd by. A symlink does NOT.
  The kernel resolves it, so the process is named after the target ("atrium") and only `comm` and argv[0] keep the
  link's name, as invoked. Activity Monitor takes the process name from the kernel, so a symlink role would show as
  plain "atrium". Option A needs hard links, or copies, on macOS, never symlinks.
- `ps -o comm` on macOS is the path as exec'd, not a basename, so it shows the link name for both. `ucomm` is the one to
  trust, and the one to use in any macOS check.
- I could NOT observe the Activity Monitor GUI. `lsappinfo` lists no atrium (they are not apps). `sample` reads the
  same kernel name that Activity Monitor uses, which is why it is cited above.

Open question 5: the live room's process list has `atrium.ptyhost` as its own process name (pid 76813, a separate
file, parent pid 1), next to the `atrium room` process. So on this machine `pty_host` is on, and the ptyhost already
has its own name. This does not say what the three identical Windows rows were.

Application firewall: `socketfilterfw --getglobalstate` says "Firewall is disabled. (State = 0)". Not changed. Listeners
here were loopback, so no inbound prompt could have appeared in any case, and macOS keys its allow list by path or
signature, which was not exercised.

Cleanup: every process I started was stopped (the two rooms needed a second kill), no listener left on 177xx, sample
files and /tmp/pn-spike-mac deleted. The live room and ptyhost were still running afterward.
