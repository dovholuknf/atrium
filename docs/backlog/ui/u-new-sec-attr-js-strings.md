# u-new-sec-attr-js-strings: `onclick="fn('${esc(x)}')"` runs whatever an agent puts in a tag

Status: held (pause, 2026-10-01). @ui. Medium (agent to operator). Source: docs/backlog/review/review-new-security-audit-kimi.md (the Kimi audit, checked by @review on 2026-10-01).

## Why

Around 40 templates put data in a JS string inside an HTML attribute. The browser decodes entities before the
handler runs, so neither `esc()` nor `.replace(/'/g,"&#39;")` keeps a quote out. Real case:
settings-spine.js:1020 builds `addTag('…')` from `knownTags()`, every card's tags. An agent sets tags through MCP
(internal/link/control_mcp.go:1303), and `NormalizeTags` only lowercases and strips commas
(internal/store/tasks.go:1467). The tag `x');alert(1);//` runs when the operator clicks the chip. Same pattern:
rooms.js:218,224,300 (room names from rooms that dialled in), expose2.js:436, expose3.js:336 and overlays.js:1140
(ziti service names), alias.js:24 and fixtures.js:34-42,406. The audit's M12 (`esc` misses `'`) is a symptom of
this. Also to check: runners.js:210,249 put `r.board`, a room's advertised URL, in an href with only `esc()`. Make
sure the scheme is http(s).

## Wanted

- `data-*` attributes and `addEventListener` in place of inline handlers that carry data, across the board.
- A headless case with a tag and a room name holding `');alert(1);//`, clicked, and nothing runs.
