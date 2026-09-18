# Z. Providers: telling atrium where your repositories live

This is the human walkthrough for `backlog-2026-09-13-001`. It is written to be done by a person at the board,
in order, in about fifteen minutes. Every step says what to do, what you should see, and **what it proves**,
because a step whose failure you would not notice is a step not worth doing.

Append this to `docs/test-plan.md` as section Z once you have run it.

The one thing to understand before starting: **a provider is a declaration, and a repository row is durable.**
Almost every surprising behaviour below follows from those two sentences. Discovery only ever ADDS rows. It
never deletes one, so nothing you do to a disk can erase what you typed.

------------

## Z0. Set up, once

You need two directories. Use real ones if you like, but a throwaway pair makes Z6 and Z7 safe to run.

```powershell
mkdir D:\zztest\git\github\dovholuknf\atrium
mkdir D:\zztest\git\github\openziti\ziti
mkdir D:\zztest\git\github\notarepo
mkdir D:\zztest\worktrees\github
cd D:\zztest\git\github\dovholuknf\atrium
git init
cd D:\zztest\git\github\openziti\ziti
git init
```

So: two real checkouts under one org each, and one directory that is not a checkout at all.

------------

## Z1. Define a provider, and watch it adopt what is already there

1. Board, **runners** tab, **providers** pane. It says nothing describes where your repositories live yet.
2. Press **add a provider**.
3. Name it `github`. Root `D:/zztest/git/github`. Leave everything else alone. **Save**.

**Expect:** the dialog closes and a toast says **adopted 2, ignored 1 that are not checkouts**.

**This proves the whole point of the feature.** You did not add a repository. You said where they live and
atrium went and looked. It also proves the two counts are separate: `notarepo` was not adopted AND was not
silently dropped, which is the difference between a list you can trust and one you spend an afternoon arguing
with.

4. Press **repositories** on the row.

**Expect:** `dovholuknf/atrium` and `openziti/ziti`, each with its full path, neither greyed.

------------

## Z2. Press look again, twice

Press **look again**. Then press it again.

**Expect:** both times, `adopted 0, already knew 2, ignored 1 that are not checkouts`.

**This proves discovery is idempotent**, which is what makes the button safe to press when you are not sure.
If the count ever climbs, rows are being duplicated.

------------

## Z3. Hide one, and confirm the hide survives a look

1. Press **hide** on `openziti/ziti`. It stays in the list, marked `hidden`.
2. Press **look again**.

**Expect:** `adopted 0, already knew 1, left 1 hidden`. `openziti/ziti` is still hidden.

**This proves hidden is a durable no rather than a suggestion.** Without it there would be no way to dismiss a
repository permanently, because the next look would re-adopt it and you would be dismissing it forever.

3. Press **unhide** to put it back.

------------

## Z4. THE ONE THAT MATTERS MOST: unplug the drive

You do not need a real drive. Rename the root.

```powershell
Rename-Item D:\zztest\git\github D:\zztest\git\github-away
```

1. Press **look again**.

**Expect:** a dialog saying **nothing adopted, could not read that root**. Both repository rows are STILL
THERE, greyed, each marked `not on disk`, each keeping its use, hide and forget buttons.

**This is the failure the design exists to prevent, and it is the step to run if you only run one.** If
presence were stored on the row, this run would have marked everything absent and any tidy-up would have
deleted it. If the rows were derived from the disk instead of stored, the list would now be empty and
everything you typed would be gone. Neither happens, because the ROW is durable and PRESENCE is worked out
fresh on every read.

2. Put it back and press **look again**.

```powershell
Rename-Item D:\zztest\git\github-away D:\zztest\git\github
```

**Expect:** `adopted 0, already knew 2`, and the rows are no longer greyed. **Nothing had to be repaired.**

------------

## Z5. Turn worktree support on, and make one

1. **edit** the provider. Tick **worktree support**. A folder field appears. Set it to
   `D:/zztest/worktrees/github`. **Save**.

**Expect:** no filesystem check of any kind, and no complaint that the folder is empty or missing.

**This proves that turning the toggle ON never reads the disk**, because there is nothing to orphan. Only OFF
is dangerous.

2. Open the launch form (**new agent**). Press **repo** beside the working directory.
3. Pick `github`, then **choose** on `dovholuknf/atrium`.
4. In **make a worktree**, type `zz-test` and press **make**.

**Expect:** a toast saying **worktree and branch made**, the directory field on the launch form is now
`D:/zztest/worktrees/github/dovholuknf/atrium/zz-test`, and **one** card would start if you pressed launch.

**Do not press launch.** Look at the board.

**This proves the single most concrete improvement over what this replaced.** The old mechanism ran a command
template whose default also STARTED A SESSION, so pressing make and then pressing start gave you two agents.
`git worktree add` makes a directory and does nothing else.

5. Press **repo**, choose the same repository again.

**Expect:** `zz-test` is now listed beside "the checkout itself".

6. Type `zz-test` into the make box again and press **make**.

**Expect:** a toast saying **already there**, and the same path. Not an error.

**This proves already-there is an answer rather than a failure**, which is what makes the button pressable
when you cannot remember whether you already made it.

------------

## Z6. The refusal

1. **edit** the provider. Untick **worktree support**. **Save**.

**Expect:** the save is refused, **the tick comes back on**, and under the field you get something like:

```
worktree support for github cannot be turned off while D:/zztest/worktrees/github holds 1 thing.

  dovholuknf: a folder holding 1 checkouts

turning it off would leave those on disk with nothing describing them. remove them, or move them
somewhere atrium has not been told about, and try again. the check button beside the toggle asks
this question without saving anything.
```

**Three things to check, and each is its own decision:**

- **It names what is in the way.** "Not empty" would make you hunt. This tells you which directory.
- **The tick came back.** The save was refused WHOLE, so the stored row still has worktrees on, and a box left
  unticked would be showing you a state that is not the one stored.
- **The last line names the way out.**

2. Now the important one. In the same dialog, ALSO change the root to `D:/zztest/elsewhere` before saving with
   the toggle off. Save. Read the refusal. Close the dialog. Open it again.

**Expect:** the root is still `D:/zztest/git/github`.

**This proves the refusal runs before any write.** Applying half a request and then reporting a failure is the
worst of both: you read an error and the machine changed anyway.

3. Press **check** beside the worktree folder.

**Expect:** the same list, and nothing saved.

4. Delete the worktree directory, then press **check** again.

```powershell
Remove-Item -Recurse -Force D:\zztest\worktrees\github\dovholuknf
```

**Expect:** *that folder is empty, so worktree support can be turned off.*

5. Untick the toggle and save.

**Expect:** it saves.

------------

## Z7. Delete the provider, and confirm no card moved

Do this one with a real card if you can, because it is the rule that matters most to somebody mid-work.

1. Launch a card in `D:/zztest/git/github/dovholuknf/atrium`. Let it come up.
2. Runners, providers, **edit**, **delete**. Confirm.

**Expect:** the provider and its repository rows are gone. The card is exactly where it was, still running,
still showing the same directory.

**This proves a card outlives the configuration that describes it.** A card's directory is a string a human
typed. Rewriting it because a configuration row went away would be atrium overwriting a human, to a card with
a live process attached.

3. Tidy up: `Remove-Item -Recurse -Force D:\zztest`.

------------

## The same walkthrough, recorded

`scripts/walkthrough/providers.spec.js` drives these steps in a real browser at a pace a person can follow,
and records a video of it. It needs a daemon on `localhost:7778` serving a board that HAS this feature, so a
restart after the build comes first.

```
npm i -D @playwright/test
npx playwright install chromium
npx playwright test --config scripts/walkthrough/playwright.config.js --headed
```

It makes and removes its own tree under `D:/zztest`. The config is required rather than a nicety: without it
Playwright reports "No tests found" even when given the spec by name.

------------

## What this section deliberately does not cover

- **A root on a real network share.** The 30 second deadline is bounded in code and tested with a fake, and a
  genuinely dead UNC path is the one thing a temp directory cannot imitate.
- **Two providers whose roots nest.** Refused at save and covered by
  `TestOverlappingRootsAreRefused`. Worth doing by hand once if you keep worktrees under your git root.
- **A junction pointing back up the tree.** Deduped on the resolved path. `mklink /J` it yourself if you want
  to see it.
