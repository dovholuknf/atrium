# Starting at boot with nobody logged in: why not a Windows service, and a $2,500 Linux room

Series: The daemon and the halt. Status: idea. Audience: people running agent machines at home.

**Hook.** A service runs in session 0 and supervises nothing you can attach to; a Mac LaunchAgent waits for a desktop login; Linux just works.

**Angle.** Each OS has a different answer to 'come back after a reboot', and it decided which machine to buy.

**Rests on:** logon task autostart, room autostart, room machine spec. See `docs/blog/inventory.md`.

## Story beats

1. Why a Windows service would report Running and supervise nothing useful.
2. The logon task instead, and what it cannot do.
3. 10-01: m1mini stayed down after a reboot because the LaunchAgent only loads at login.
4. The $2,500 room spec: Linux, because unattended boot already works there.
5. What is still unproven.

## Screenshots and demos

- a table of the three OSes
- the provisioning script's autostart step

## Sources

- website/docs/story.md
- docs/rnd/room-autostart-design.md
- docs/rnd/room-machine-2500-design.md
- changelog/fabric/2026-10-01-provision-mac-start.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
