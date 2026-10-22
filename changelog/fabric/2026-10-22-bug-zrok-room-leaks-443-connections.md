A room on zrok no longer leaks connections. Every dial built a ziti context that was never closed, and one room held
about 16,000 sockets to `:443` and starved its machine of ports. zrok now dials on one shared context, as ziti does,
and a room on direct rides one mutual TLS connection with every link connection a stream inside it. Rooms and hubs
upgrade in any order. bug-zrok-room-leaks-443-connections.
