// A recording of the providers walkthrough, at a pace a person can follow.
//
// NOT A TEST, and the distinction decides everything about how this is written.
// The Go suites already assert the behaviour, in tempdirs, in milliseconds, and
// they run in CI. This drives the real board in a real browser and RECORDS it,
// so what it produces is a video somebody can watch to see the feature work,
// and a second opinion on whether the board actually does what the tests say
// the daemon does.
//
// It therefore does the opposite of what a test should: it goes slowly on
// purpose, it pauses to let you read, and it prefers a screenshot over a clever
// assertion. Where it does assert, it asserts the things a person watching
// would notice, because an unwatched recording of a broken board is worth
// nothing.
//
// ── running it ──────────────────────────────────────────
//
// Nothing here is installed. Playwright is not a dependency of atrium and this
// file costs nothing until somebody runs it:
//
//   npm init -y
//   npm i -D @playwright/test
//   npx playwright install chromium
//   npx playwright test --config scripts/walkthrough/playwright.config.js --headed
//
// THE CONFIG IS NOT OPTIONAL. Without it Playwright answers "No tests found"
// even when handed this file by name, because a positional argument is a
// regular expression matched against paths and the default test directory is
// not this one. The config also raises the per-test timeout, which a
// deliberately slow recording walks straight through.
//
// The video lands in `test-results/`. `ATRIUM_SLOW` changes the pace.
//
// ── what it needs ───────────────────────────────────────
//
//   - a daemon running on http://localhost:7778, which is the default
//   - `git` on PATH
//   - permission to write D:/zztest, which it makes and removes itself
//
// It uses a THROWAWAY tree rather than your real repositories, so the refusal
// step can leave debris and the delete step can delete something.

const { test, expect } = require("@playwright/test")
const { execSync } = require("child_process")
const fs = require("fs")
const path = require("path")

// SLOW is the whole point of this file. `slowMo` puts a gap between every
// browser action, and the explicit `beat` calls below are the longer pauses
// where something has just appeared and a person would stop to read it. The
// browser side of it lives in the config, next door. This copy is only for
// sizing the beats.
const SLOW = Number(process.env.ATRIUM_SLOW || 600)
const BOARD = process.env.ATRIUM_BOARD || "http://localhost:7778"
const TREE = process.env.ATRIUM_TREE || "D:/zztest"
const ROOT = `${TREE}/git/github`
const WT = `${TREE}/worktrees/github`

// A beat is a pause for the VIEWER, not for the page. Named so that reading
// this file tells you where the interesting moments are.
const beat = (page, n = 1) => page.waitForTimeout(SLOW * 2 * n)

// THE NEWEST TOAST, and the direction matters.
//
// `toast()` appends, so `.first()` is the OLDEST one still on screen and a
// run that has already said something would assert against the wrong message.
// They live nine seconds, which is longer than the pauses between these steps.
const newestToast = page => page.locator(".toast").last()

function makeTree() {
  for (const d of [
    `${ROOT}/dovholuknf/atrium`,
    `${ROOT}/openziti/ziti`,
    `${ROOT}/notarepo`,
    WT,
  ]) {
    fs.mkdirSync(path.normalize(d), { recursive: true })
  }
  // Two real checkouts and one directory that is not one, which is what makes
  // the "ignored 1" count in Z1 mean something.
  for (const d of [`${ROOT}/dovholuknf/atrium`, `${ROOT}/openziti/ziti`]) {
    execSync("git init -q", { cwd: path.normalize(d) })
    execSync("git commit -q --allow-empty -m first", {
      cwd: path.normalize(d),
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: "z", GIT_AUTHOR_EMAIL: "z@z",
        GIT_COMMITTER_NAME: "z", GIT_COMMITTER_EMAIL: "z@z",
      },
    })
  }
}

test.beforeAll(() => {
  fs.rmSync(path.normalize(TREE), { recursive: true, force: true })
  makeTree()
})

test.afterAll(async () => {
  // The provider is deleted by the walkthrough itself, in its last step, which
  // is the step that proves deleting one touches no card. Anything left is
  // cleaned here so a failed run does not poison the next one.
  await fetch(`${BOARD}/v1/providers/github`, { method: "DELETE" }).catch(() => {})
  fs.rmSync(path.normalize(TREE), { recursive: true, force: true })
})

test("a provider is told where repositories live, and adopts what is there", async ({ page }) => {
  await page.goto(BOARD)
  await beat(page)

  // ── Z1: define it, and watch it adopt ──
  await page.getByRole("button", { name: "runners", exact: true }).first().click()
  await page.getByRole("button", { name: "providers", exact: true }).click()
  await beat(page)

  await page.getByRole("button", { name: "add a provider" }).click()
  await page.locator("#pv-name").fill("github")
  await page.locator("#pv-root").fill(ROOT)
  await beat(page)
  await page.getByRole("button", { name: "save", exact: true }).click()

  // THE CLAIM THIS WHOLE FEATURE MAKES: you said where they live, and atrium
  // went and looked. Two adopted, one ignored, and the ignored one COUNTED
  // rather than dropped.
  const toast = newestToast(page)
  await expect(toast).toContainText("adopted 2")
  await expect(toast).toContainText("not checkouts")
  await beat(page, 2)

  await page.getByRole("button", { name: "repositories" }).click()
  await expect(page.locator(".pvrepo")).toHaveCount(2)
  await beat(page, 2)

  // ── Z2: idempotent ──
  await page.getByRole("button", { name: "look again" }).click()
  await expect(newestToast(page)).toContainText("already knew 2")
  await beat(page, 2)

  // ── Z4: the one that matters. Take the root away. ──
  //
  // Renamed rather than deleted, because putting it back has to restore the
  // whole picture with no action, and that is the half people do not believe.
  fs.renameSync(path.normalize(ROOT), path.normalize(ROOT + "-away"))
  await page.getByRole("button", { name: "look again" }).click()
  await beat(page)

  // Every row is still there. This is the assertion the design was written
  // around: the ROW is durable and PRESENCE is derived, so an unplugged drive
  // greys the list rather than erasing it.
  await expect(page.locator(".pvrepo")).toHaveCount(2)
  await expect(page.locator(".pvrepo.gone")).toHaveCount(2)
  await expect(page.locator(".pvrepo .chip.warn").first()).toContainText("not on disk")
  await page.screenshot({ path: "test-results/z4-drive-unplugged.png" })
  await beat(page, 3)

  fs.renameSync(path.normalize(ROOT + "-away"), path.normalize(ROOT))
  await page.getByRole("button", { name: "look again" }).click()
  await beat(page)
  await expect(page.locator(".pvrepo.gone")).toHaveCount(0)
  await beat(page, 2)

  // ── Z5: worktrees on, and make one ──
  await page.getByRole("button", { name: "edit" }).first().click()
  await page.locator("#pv-worktrees").check()
  await page.locator("#pv-worktree-root").fill(WT)
  await beat(page)
  await page.getByRole("button", { name: "save", exact: true }).click()
  await beat(page)

  // ── Z6: the refusal, which needs something in the folder ──
  //
  // Made directly rather than through the picker, because the recording is
  // about the REFUSAL and a full make-a-worktree detour buries it.
  fs.mkdirSync(path.normalize(`${WT}/dovholuknf/atrium/zz-test`), { recursive: true })

  await page.getByRole("button", { name: "edit" }).first().click()
  await page.locator("#pv-worktrees").uncheck()
  // ALSO move the root, which is the half of this step that proves the
  // request is refused WHOLE rather than half applied.
  await page.locator("#pv-root").fill(`${TREE}/elsewhere`)
  await beat(page)
  await page.getByRole("button", { name: "save", exact: true }).click()
  await beat(page)

  const blockers = page.locator("#pv-blockers")
  await expect(blockers).toBeVisible()
  await expect(blockers).toContainText("cannot be turned off")
  await expect(blockers).toContainText("dovholuknf")
  // The tick came back, because the stored row still has it on and a box left
  // unticked would be showing a state nothing holds.
  await expect(page.locator("#pv-worktrees")).toBeChecked()
  await page.screenshot({ path: "test-results/z6-refusal.png" })
  await beat(page, 3)

  // And nothing was written. Asked of the daemon rather than the form, because
  // the form is the thing under suspicion.
  const saved = await (await fetch(`${BOARD}/v1/providers`)).json()
  const mine = saved.providers.find(p => p.name === "github")
  expect(mine.root.toLowerCase()).toBe(ROOT.toLowerCase())
  expect(mine.worktrees).toBe(true)

  // Clear the folder and ask again, which is what the check button is for.
  fs.rmSync(path.normalize(`${WT}/dovholuknf`), { recursive: true, force: true })
  await page.getByRole("button", { name: "check", exact: true }).click()
  await expect(blockers).toContainText("empty")
  await beat(page, 2)

  await page.locator("#pv-worktrees").uncheck()
  await page.getByRole("button", { name: "save", exact: true }).click()
  await beat(page, 2)
})
