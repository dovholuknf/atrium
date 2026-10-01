- **A proxy on the machine is no longer the operator.** Every hub route that answered only loopback (notify, hosts,
  launch caps, deps writes, deploy owner, git, `/_hub/mcp`, nudge, the restart ask) and the room's `/v1/shutdown` now
  ask `edge.LocalOperator`: a loopback source, a loopback `Host`, and no forwarding header (`Forwarded`,
  `X-Forwarded-*`, `X-Real-Ip`, `X-Proxy`). A zrok share terminates on 127.0.0.1, so before this anyone the share
  admitted passed those gates. From the share, the gear's notify row and the hosts setting now answer 403 with a line
  saying why. Scripts, the CLI and MCP on the machine are unchanged. Hub and room side: the room half is live at the
  next room restart. (docs/rnd/local-proxy-trust-design.md, LP1)
