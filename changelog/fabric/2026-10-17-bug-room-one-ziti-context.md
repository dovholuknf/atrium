A process now holds exactly one OpenZiti context per identity and never closes it while it runs. A failed dial or a
reset logs the context in again instead of building another, commands such as `atrium room ssh` and `rooms spec pull`
dial the hub through the running room instead of loading the identity themselves, and a second build for one identity
is refused and logged as a bug. `bug-room-one-ziti-context`.
