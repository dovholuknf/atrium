# Nest a child under a launcher on another room

See `changelog/terminal/2026-10-14-nest-cross-room.md`.

## Test plan

- Headless `nestCrossRoom`: a child with `spawned_by` `boss@sg4` nests under `sg4~b1`, not a `kc/boss` decoy.
- By hand: launch a worker on another room from a session here. It sits under its launcher on every tab.
