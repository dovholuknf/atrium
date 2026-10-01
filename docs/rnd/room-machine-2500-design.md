# The best atrium room machine for $2,500

Prices seen 2026-10-01 unless noted. "Est" means my estimate, not a measurement or a quoted price. Nothing was bought,
ordered or signed up for.

## Recommendation

- Buy one Linux desktop: Ryzen 9 9950X, 64 GB DDR5, 2 TB NVMe, about $2,150 (est, parts below), plus a $150 UPS.
  Total about $2,300, which leaves about $200 of headroom.
- Make it a new atrium room on Linux. It is the only OS where "room starts at boot, no desktop login" already works
  (systemd user unit plus `enable-linger`). Provisioning is proven on WSL Ubuntu over ssh, and the deb's postinstall
  turns on linger. What is still unproven: a box that is not WSL, provision WITH `-Linger`, a reboot with nobody
  logged in, the Chromium libraries for the suite, and the Go tests on Linux. Proving those is the first week, and
  the real cost of this pick.
- Keep m1mini as the macOS room. Move heavy work (the board suite, `go test ./internal/daemon`, most claude sessions)
  to the new box. sg4 can stop hosting work and go back to being a laptop. sg3 can retire or stay as a spare.
- If clint will not take the Linux provisioning risk, the fallback is a Mac mini M5 Pro 64 GB at $2,599. It is $99 over
  budget and has the same power-loss problem m1mini has today.

## What the workload tells us

- The suite is bound by total CPU work. sg4 ran at 79% average CPU with p99 timer drift of 104 ms, so it is saturated
  and the drift is why it lags. m1mini has only 8 cores and runs a 4m08 suite with 5 ms drift, so it was not starved.
- sg3 (6 cores, 12 threads, i7-10750H) finished in 3m10, faster than sg4 (14 cores, 20 threads) at 3m28. More cores did
  not help sg4. A laptop that throttles, runs Defender and sits at 57/64 GB explains that better than core count does.
  This is an inference from three numbers, not a profile.
- The sessions are cheap. 15 claude processes at 250-350 MB is about 4 to 5 GB. They never justified 64 GB. The suite
  (one Chromium per shard), Go builds, git worktrees and the 12 GB leak did.

## Single thread vs cores vs RAM vs NVMe

1. Sustained all-core speed in a machine that does not throttle. This moves suite and build time most. A desktop
   9950X (16 cores, 32 threads, 170 W) holds its clocks where a 13900H laptop cannot. I estimate the suite at 1m30 to
   2m and the daemon tests at 100 to 150 s. These are guesses scaled from core count and clocks, so measure on arrival.
2. RAM capacity, to a point. 32 GB runs the sessions and a suite. 64 GB gives room for a leak and several worktree
   builds at once. Past 64 GB buys nothing here.
3. Single-thread speed. It sets per-shard latency and Go link time. Every current chip here is good enough.
4. NVMe speed. It matters for worktree creation and Go cache, but any PCIe 4.0 drive is enough. Spend on capacity
   (2 TB), because a few GB per worktree adds up. sg4's C drive is 774 of 926 GB used.

## Local models

No. A local model earns nothing from the budget today. Embedding or a small reviewer would want a GPU or a 64 GB
unified-memory Mac, and all the review work already runs on API models that are better. Skip it. If it is wanted
later, a Mac mini M5 Pro is the cheap way to add it, and it fits as a second room.

## The builds

| Option | Parts | Price | Sessions | Suite (est) | Go tests (est) | Notes |
|---|---|---|---|---|---|---|
| A. Linux desktop (recommended) | 9950X, 64 GB, 2 TB, UPS | about $2,300 total (est) | 40+ | 1m30-2m | 100-150 s | Loud under load, idle about 60 W (est), needs Linux room work |
| B. One Mac mini M5 Pro | 15-core, 64 GB, 1 TB | $2,599 | 40+ | 2-3m (est) | 150-200 s (est) | Silent, idle under 15 W (est), $99 over budget, power-loss issue |
| C. Two M6 minis | 24 GB, 512 GB each, $1,299 x 2 | $2,598 | 2 rooms x 20 | 3-4m each | 250+ s | Failure isolation, two rooms to run, weak at 24 GB |
| D. Three small Ryzen mini PCs | Beelink SER9 Pro 32 GB, $699 x 3 | $2,097 plus UPS | 3 rooms x 15 | about 3m each (est) | about 250 s | Cheapest to lose one, three rooms of Linux or Windows |

Links and sources:

- A. Ryzen 9 9950X on Newegg, listings from $399 to $588 depending on seller and condition, the cheap ones may be open
  box: <https://www.newegg.com/p/pl?d=ryzen+9+9950x>. DDR5 is the surprise. A Corsair Vengeance 64 GB DDR5-6000 kit is
  listed at $869.99 on Newegg, and kits run $900 to over $2,000: <https://www.newegg.com/p/pl?d=ddr5+64gb+kit>, with
  background at <https://www.newegg.com/insider/best-ddr5-ram-kits-in-2026-and-why-prices-are-up/>. My parts estimate:
  CPU $500, RAM $870, board $200, 2 TB NVMe $250 (est, not looked up), case, PSU and cooler $300 (est). Sum about
  $2,150. Memory is now the largest line, so buying 32 GB first (about half the RAM price, est) and adding the
  second kit later is a real option, and it brings the build under $1,800.
- B. Mac mini M5 Pro 15-core, 64 GB, 1 TB, $2,599 at B&H:
  <https://www.bhphotovideo.com/c/product/1997679-REG/apple_mhqn4ll_a_mac_mini_m5_pro.html>. Lineup and starting
  prices (M6 $899, M5 Pro $1,699) from <https://www.macrumors.com/guide/2024-vs-2026-mac-mini/>.
- C. Mac mini M6 24 GB, 512 GB, $1,299 at B&H:
  <https://www.bhphotovideo.com/c/product/1997678-REG/apple_mhqm4ll_a_mac_mini_m6.html>.
- D. Beelink SER9 Pro, 32 GB, 1 TB, $699 on Amazon:
  <https://www.amazon.com/Beelink-SER9-10-Core-LPDDR5X-Ultra-Quiet/dp/B0DZNPTBV3>. I found no 64 GB model. The
  Minisforum MS-A2 (Ryzen 9, 64 GB option, 10G networking) lists at $839.90 but every configuration was sold out:
  <https://www.minisforum.com/products/minisforum-ms-a2>. I did not price a used Mac mini or a Mac Studio in detail.
  The M5 Max Mac Studio starts at $2,499 with 36 GB and is $2,899 at 64 GB, so it is out of budget
  (<https://www.macworld.com/article/2973459/2026-mac-studio-m5-release-date-specs-price-rumors.html>).

Throughput figures in the table are estimates scaled from the three measured machines and core counts. None were run.

## One box vs several

- For several: failure isolation, a power cut or a hung room takes out a fraction, each room has its own session cap,
  and the hub already links rooms.
- Against several: provision refuses a second room on one machine (`docs/release/packaging.md`, exit 6), so sg4's
  two rooms (claude-sg4 and sg4-control) are hand work, and more rooms mostly means more boxes. Each extra box is
  another provision, another claude login
  (`claude auth login` over ssh, once each), another set of worktrees to sync by git, more plugs and more fans.
- The suite is one job that wants one big pool of cores. Splitting it across small rooms makes each shard slower and
  gains nothing, because the board suite is not spread across rooms. Three small boxes would each take about 3 minutes
  where one fast box should take under 2 (est).
- A single box has a single point of failure and a memory ceiling. With m1mini staying up as a second room, the
  failure case is covered at no extra cost. That is why I do not buy a second new box now.

## OS

- Linux: best fit for an always-on room. `scripts/atrium-service.sh` installs a systemd user unit and
  `provision-room.ps1` handles Linux over ssh, with `-Linger` so the room starts at boot with nobody logged in.
  `docs/release/packaging.md` records provision (install, rerun, `-Autostart`, `-Remove`) proven on WSL Ubuntu over
  ssh localhost, and the deb postinstall turning on linger with the unit active. To finish on a real box: run
  `provision-room.ps1 user@host -Linger` once, reboot with nobody logged in, install claude and codex, run
  `claude auth login` over `ssh -t`, check the Chromium dependencies, and run the Go tests there.
- The suite already dispatches to sg3 by default (`scripts/test-board-sharded.js`) and needs pwsh on the target.
  Moving it to the new box means setting `ATRIUM_SUITE_ROOM` and installing pwsh there.
- Windows: sg4 and sg3 are rooms today, but the logon task fires only at an interactive logon, and
  `docs/rnd/room-autostart-design.md` says the boot task is designed, not shipped. Defender also costs build time
  (the repo ships `room-defender.ps1` for exclusions).
- macOS: m1mini is the room today. A LaunchAgent needs a GUI login. The design doc says FileVault makes an unplanned
  reboot wait at the unlock screen, and the LaunchDaemon plan works only with FileVault off.

## Always on

- Power loss on m1mini: `autorestart 1` brings the hardware back, but the room then needs more. Check these:
  (1) FileVault is on, which stops boot at the unlock screen. (2) No desktop login, so the LaunchAgent never loads.
  (3) The network may not be up yet when the room starts. (4) Anything started from a terminal is gone.
  I did not check FileVault or auto-login on m1mini, since that needs `fdesetup` and admin. It is the first thing to
  look at. Currently `sleep 0` is held by `caffeinate`, and `womp 1` is set.
- On the Linux box: set "restore on AC power loss" in the BIOS to Power On, enable linger (the unit already ships
  `Restart=on-failure`), keep sshd enabled, and add a second remote path (the hub link already gives one).
  A board with BMC or Intel AMT costs more, so a smart plug is the cheap way to power cycle.
- UPS: a 1000-1500 VA line-interactive unit is about $150 (est). Add `apcupsd` or NUT so the box shuts down cleanly
  before the battery ends. This protects sqlite and worktrees more than it protects uptime.
- Noise: a 9950X with a decent tower cooler is quiet at idle and audible under the suite. Put it where it can be heard
  without bothering anyone. Idle power for the desktop is about 60 W (est), against about 5 to 7 W for a mini.

## What it frees

- sg4 stops hosting. The Stealth 16 is a throttling laptop with a battery under constant load, and it does not suit
  server duty. Move claude-sg4's work to the new box and keep sg4 as a client. Whether sg4-control (the orchestrator's
  room) moves too is a separate choice (@rnd's note).
- sg3: its RAM is unknown. I could not read it from here and the brief does not say.
- m1mini stays as the macOS room, and tests the macOS side of things.

## Open questions for clint

- Is a Linux room acceptable, given the steps still unproven off WSL? If not, take option B and accept $99 over.
- Is a 32 GB first, 64 GB later purchase fine, given DDR5 prices?
- Is FileVault on m1mini? That decides whether it survives the next power cut.
