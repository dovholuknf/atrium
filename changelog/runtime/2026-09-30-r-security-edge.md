- **A web page cannot reach atrium through the browser.** Every listener a browser can reach, on the hub and the room,
  refuses a cross-site write (`http.CrossOriginProtection`) and a websocket upgrade from another origin, and a loopback
  listener answers only a loopback `Host`, which stops DNS rebinding. Hooks, the CLI and curl send neither header and
  pass as before. A request the hub forwards to a room is checked at the hub, so terminals still attach through it. A
  test pins that a refused hook behaves like an unreachable one. Growlers also skip Open Questions asked before
  growlers began on the hub (`growl.since`), so old questions no longer buzz the phone. Hub and room side: the room
  half is live at the next room restart. (security-design stages 0 and 1)
