A card owns the processes and ports its agent starts. `atrium_port {count}` (`POST /v1/tasks/{id}/ports`) hands out
free ports from the room's card range and records each on the card's inventory. A port is skipped when Windows has
reserved it (`netsh int ipv4 show excludedportrange protocol=tcp`, read once a minute), when another card here holds
it, or when a loopback bind on it fails. The range is the room setting `ports.card_range` ("lo-hi"), else a block of
200 from 20000 picked from the room's name, so it stays below the 49152 dynamic range and clear of the previews'
50000-50999. `atrium_own {kind, ref}` (`POST /v1/tasks/{id}/own`) records a `proc` (a pid, with its start time read at
once), a `dir` inside the card's worktree or the scratch folder, or a `port`. Closing the card stops its processes
first, with every process each started (taskkill /T on Windows, the process group elsewhere), and only while the pid
is still the process recorded. Then it releases its ports. The sweep marks a proc freed once its process exited or
its pid is another process. The full process registry (processes atrium runs in a pty, with attach) is its own item.
Design: `docs/rnd/card-lifecycle-design.md` section 9, phase 9.

Test plan:
- From a card, call `atrium_port {count: 2}`. Two ports come back, in the room's range and outside 50000-50999. A
  second card asking gets different ones.
- Start a long-running process from the card (`ping -n 600 127.0.0.1` in the background) and `atrium_own {kind: proc,
  ref: <its pid>}`. The card's close preview lists it as "stopped, with every process it started".
- Close the card. The process is gone, and the ports are handed out again to the next card that asks.
- Record a proc, end it by hand, then press leftovers on the pulls tab. The proc row is marked freed.
