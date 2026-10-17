A room no longer leaks one OpenZiti context per ssh sync call, which pinned thousands of contexts and pegged the CPU
within half a day. The ssh sync and the wizard route now dial over the room's own link, and a count past 16 live
contexts is refused with a log line. `bug-room-leaks-ziti-contexts`.
