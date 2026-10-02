- **`restart_atrium` no longer opens the board to the LAN.** The restart relaunched the daemon with only `--db`, so a
  loopback daemon came back on the flag defaults `:7778` and `:7777`, every interface, with no login. The daemon now
  records the addresses it bound (`board_listen`, `agent_listen` in the location file) and the restart passes them
  back: loopback stays loopback, and a daemon the operator started wide keeps that. A location file written before
  this falls to loopback on its port, never wider. The `atrium daemon` defaults for `--http` and `--addr` are
  now `127.0.0.1:7778` and `127.0.0.1:7777`, so a bare start is loopback only. Item r-restart-loopback.
