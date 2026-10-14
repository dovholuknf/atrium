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
