// The hub/room claim, recorded: kill the board, keep the agents.
//
// This is the demo, not a unit test. The Go suite already proves the transport
// in milliseconds. What this proves is the thing you cannot assert in Go: that
// a real browser, on a real board, watching a real agent, does not notice when
// the process serving the page is killed and brought back.
//
// It goes slowly on purpose, and it pauses where a person would stop to read.
//
// ── running it ──────────────────────────────────────────
//
//   go build -o build.claude/atrium2.exe ./cmd/atrium2
//   npm i -D @playwright/test
//   npx playwright install chromium
//   npx playwright test --config scripts/walkthrough/playwright.config.js --headed
//
// It starts its own hub and its own room on their own ports, against a
// throwaway database under D:/tmp, and stops both afterwards. It touches
// nothing belonging to the atrium you have running.

const { test, expect } = require("@playwright/test")
const { spawn, execSync } = require("child_process")
const fs = require("fs")
const path = require("path")

const SLOW = Number(process.env.ATRIUM_SLOW || 600)
const EXE = process.env.ATRIUM2_EXE ||
  path.resolve(__dirname, "../../build.claude/atrium2.exe")
const TREE = process.env.ATRIUM2_TREE || "D:/tmp/atrium2-demo"
const BOARD = "127.0.0.1:7900"
const LINK = "127.0.0.1:7901"
const ROOM_HTTP = "127.0.0.1:7910"
const ROOM_AGENT = "127.0.0.1:7911"

const beat = (page, n = 1) => page.waitForTimeout(SLOW * 2 * n)

let hub = null
let room = null

// start runs atrium2 and collects its output, because the join string is
// printed rather than returned and the test has to read it the way a person
// would.
function start(args, log) {
  const out = fs.openSync(log, "w")
  const p = spawn(EXE, args, { stdio: ["ignore", out, out] })
  p.on("error", e => { throw e })
  return p
}

async function until(what, ms = 20000) {
  const deadline = Date.now() + ms
  while (Date.now() < deadline) {
    try { if (await what()) return } catch (e) { /* not yet */ }
    await new Promise(r => setTimeout(r, 250))
  }
  throw new Error("timed out waiting")
}

const get = async (url) => {
  const res = await fetch(url)
  return { code: res.status, body: await res.text() }
}

test.beforeAll(async () => {
  if (!fs.existsSync(EXE)) {
    throw new Error(`no atrium2 at ${EXE}. build it first:\n` +
      `  go build -o build.claude/atrium2.exe ./cmd/atrium2`)
  }
  fs.rmSync(TREE, { recursive: true, force: true })
  fs.mkdirSync(TREE, { recursive: true })

  hub = start(["hub", "--addr", BOARD, "--link", LINK, "--dir", `${TREE}/hub`],
    `${TREE}/hub.log`)
  await until(async () => (await get(`http://${BOARD}/_hub/health`)).code === 200)

  // The join string, read out of what the hub printed. Exactly what a person
  // does: look at the terminal, select the line, paste it.
  const printed = fs.readFileSync(`${TREE}/hub.log`, "utf8")
  const token = (printed.match(/atrium2 join (\S+)/) || [])[1]
  if (!token) throw new Error("the hub printed no join string:\n" + printed)

  room = start(["join", token, "--name", "demo", "--dir", `${TREE}/room`,
    "--db", `${TREE}/room/atrium2.db`, "--http", ROOM_HTTP, "--agent", ROOM_AGENT],
    `${TREE}/room.log`)
  await until(async () => {
    const r = await get(`http://${BOARD}/_hub/rooms`)
    return r.code === 200 && JSON.parse(r.body).rooms.length === 1
  })

  // A runner to launch. The shell is the honest choice here: it is a real
  // supervised process in a real pseudo terminal, which is the thing whose
  // survival is the whole point, and it costs no tokens to run.
  const hs = JSON.parse((await get(`http://${BOARD}/v1/harnesses`)).body)
  const shell = hs.harnesses.find(h => h.id === "shell")
  shell.enabled = true
  await fetch(`http://${BOARD}/v1/harnesses/shell`, {
    method: "PUT", headers: { "Content-Type": "application/json" },
    body: JSON.stringify(shell),
  })
})

test.afterAll(() => {
  for (const p of [room, hub]) { try { p && p.kill() } catch (e) {} }
  try { execSync(`taskkill /F /IM atrium2.exe`, { stdio: "ignore" }) } catch (e) {}
  fs.rmSync(TREE, { recursive: true, force: true })
})

test("the board can be restarted without the agents noticing", async ({ page }) => {
  // ── the board, served by the hub ──
  await page.goto(`http://${BOARD}/`)
  await beat(page, 2)

  // It is the ordinary atrium board. Nothing about the page changed.
  await expect(page).toHaveTitle(/atrium/i)
  await page.screenshot({ path: "test-results/hubroom-1-board.png" })

  // ── launch an agent, through the hub, into the room ──
  const made = await fetch(`http://${BOARD}/v1/launch`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ harness: "shell", cwd: TREE, title: "watch-me-survive" }),
  })
  const task = await made.json()
  expect(task.pid).toBeGreaterThan(0)

  // WHAT THE BOARD DRAWS IS THE DISPLAY TITLE, which is the one that was asked
  // for. The `title` field on the launch response is the derived wire name,
  // `atrium2-demo-71500`, which appears nowhere on screen. Asserting on that
  // one fails while everything works, which is the worst kind of test.
  const shown = "watch-me-survive"

  // THE DEFAULT VIEW IS `stack`, FILTERED TO `ready`, and a running agent is
  // not ready. Looking there and finding nothing is a test failing for a
  // reason that has nothing to do with what it is testing.
  await page.getByRole("button", { name: /^all\s/ }).click()

  // The card appears without a reload, which means the event stream is flowing
  // through the proxy unbuffered.
  await expect(page.getByText(shown, { exact: false }).first())
    .toBeVisible({ timeout: 15000 })
  await beat(page, 2)
  await page.screenshot({ path: "test-results/hubroom-2-agent-running.png" })

  // ── kill the hub, with the browser watching ──
  hub.kill()
  await until(async () => {
    try { await get(`http://${BOARD}/_hub/health`); return false } catch (e) { return true }
  })

  // THE CLAIM, asserted while the board is dead: the agent's process is
  // untouched. Asked of the operating system rather than of atrium, because
  // atrium is what is being doubted.
  expect(alive(task.pid)).toBe(true)
  // And the room is still a working atrium at its own address, which is the
  // escape hatch.
  const escape = await get(`http://${ROOM_HTTP}/v1/tasks`)
  expect(escape.code).toBe(200)
  await beat(page, 3)

  // ── bring it back ──
  hub = start(["hub", "--addr", BOARD, "--link", LINK, "--dir", `${TREE}/hub`],
    `${TREE}/hub2.log`)
  await until(async () => {
    const r = await get(`http://${BOARD}/_hub/rooms`)
    return r.code === 200 && JSON.parse(r.body).rooms.length === 1
  }, 40000)

  await page.reload()
  await beat(page, 2)
  await page.getByRole("button", { name: /^all\s/ }).click()

  // Same card, same process id. The board went away and came back and the
  // agent never knew.
  await expect(page.getByText(shown, { exact: false }).first())
    .toBeVisible({ timeout: 20000 })
  const after = JSON.parse((await get(`http://${BOARD}/v1/tasks`)).body)
  const same = after.tasks.find(t => t.id === task.id)
  expect(same).toBeTruthy()
  expect(same.pid).toBe(task.pid)
  expect(alive(task.pid)).toBe(true)

  await page.screenshot({ path: "test-results/hubroom-3-survived.png" })
  await beat(page, 3)
})

// alive asks Windows, not atrium.
function alive(pid) {
  try {
    const out = execSync(`tasklist /FI "PID eq ${pid}" /NH`, { encoding: "utf8" })
    return out.includes(String(pid))
  } catch (e) {
    return false
  }
}
