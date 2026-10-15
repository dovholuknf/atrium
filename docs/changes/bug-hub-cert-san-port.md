## Test plan

## @LETTER@. The hub certificate covers the advertised address

### @LETTER@1. Direct dial by IP

1. Start a throwaway hub on spare ports with a temp data dir and `--link-advertise 192.168.1.68:7779`.
2. Read `hub.crt` in its link dir and check the SANs list the IP as an IP address and no name with a port.
3. Join a room that dials that IP and confirm it attaches with no certificate error.
