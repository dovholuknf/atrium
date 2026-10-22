A room on zrok no longer leaks connections. Every dial built a ziti context that was never closed, and one room held
about 16,000 sockets to `:443` and starved its machine of ports. zrok and ziti now dial on shared contexts, and a room
on direct rides two mutual TLS connections with every link connection a stream inside one. The control connection has
a session of its own on every transport, so a heartbeat never queues behind a git fetch or a download and a busy
uplink no longer gets a healthy room dropped. Rooms and hubs upgrade in any order. bug-zrok-room-leaks-443-connections.
