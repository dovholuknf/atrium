# u-rooms-pill-setup-drift

- Hub: internal/link/setupwatch.go reads each attached room through its existing GET /v1/hooks, /v1/health (now also reports the go version) and /v1/harnesses, on attach and every 5 min (15s tick, off the request path). The facts ride Attached.Setup, so /_hub/rooms and the `rooms` event carry them. POST /_hub/setup-check?room= rereads one room, used after a fix.
- The build compared is the board hash /v1/health already reports against the hub's boardID. Skipped when either is empty.
- Board: rooms.js gives the pill a warn class and tooltip lines. Rooms rows get issue lines, a fix button (POST /v1/hooks/install with X-Atrium-Room) for hooks only, and a "not answering" chip.
- Tests: internal/link/setupwatch_test.go. Headless roomsSetup section plus bootClean pass. go test for link, api and cli pass.
- PNGs: docs/screens/u-rooms-pill-setup-drift/, before and after for the pill and the row.
- Runners and go version are collected into Setup but not drawn yet.
