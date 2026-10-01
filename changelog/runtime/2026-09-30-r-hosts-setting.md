- **The names the hub answers are a setting.** `GET/PUT /_hub/hosts` keeps a list beside `$ATRIUM_HOSTS` in the
  hub's store, and every board listener answers it from the next request, with no restart. Set from the hub's machine
  only, since the list is the DNS rebinding guard. A wildcard over a public suffix (`*.duckdns.org`, `*.co.uk`,
  `*.github.io`) is ignored and logged once, because anybody can own a name there. The answer lists the ignored
  entries with why. A refused Host is told to add its name in the gear. Hub side, the gear row is @ui's.
  (r-new-hosts-setting)
