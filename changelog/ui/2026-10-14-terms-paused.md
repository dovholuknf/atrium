- `shelved` is now `paused`: a card with no agent running that CAN be resumed. A dead card cannot. The stored status is
  migrated, and `shelved` is still accepted as an alias on the API and CLI.
- A parked card with no process reads paused, never ready, and the paused chip lists it and hides it. Hidden paused
  and dead children no longer show nested under a visible parent, at any level.
- A paused card's popup has a resume row. A dead card with its folder has a new agent here row and no resume.
- The Terminals header says `N shown`.
