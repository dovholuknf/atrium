`atrium ssh config` now writes `ssh-proxy %n` (the name as typed) instead of `%h`, so a LAN `HostName` for the same
Host no longer sends the proxy a name no room has (sgg). Blocks already written with `%h` are rewritten by the next
`atrium ssh config add`. Item bug-ssh-proxy-percent-h.
