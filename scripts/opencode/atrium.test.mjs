// The opencode plugin against a fake atrium.
//
//   node --test --experimental-test-module-mocks scripts/opencode/atrium.test.mjs
//
// `node:child_process` is replaced, so every `atrium ...` the plugin starts is
// answered here and nothing is spawned. What matters most is the edited
// approval: what runs must be the edit or nothing, never the original.

import { test, mock } from "node:test"
import assert from "node:assert/strict"
import { EventEmitter } from "node:events"

// What the fake atrium prints for `hook --event permission`, set per test.
let answer = ""
// The payloads the plugin sent, by event.
const sent = []

function fakeChild(args) {
  const child = new EventEmitter()
  child.stdout = new EventEmitter()
  child.kill = () => {}
  child.stdin = {
    on() {},
    end(body) {
      const event = args[args.indexOf("--event") + 1]
      sent.push({ event, payload: JSON.parse(body) })
      setImmediate(() => {
        if (event === "permission" && answer) child.stdout.emit("data", Buffer.from(answer))
        child.emit("close", 0)
      })
    },
  }
  return child
}

mock.module("node:child_process", {
  namedExports: {
    spawn: (_exe, args) => fakeChild(args),
    spawnSync: () => ({ status: 0 }),
  },
})

process.env.ATRIUM_RUNNER = "opencode"
process.env.ATRIUM_HOOK_EXE = "fake-atrium"
const { AtriumPlugin } = await import("./atrium.js")
const hooks = await AtriumPlugin({ client: {}, directory: process.cwd() })
const before = hooks["tool.execute.before"]

function allow(updatedInput) {
  return JSON.stringify({ hookSpecificOutput: { permissionDecision: "allow", updatedInput } })
}

async function call(tool, args) {
  const output = { args }
  await before({ tool, sessionID: "ses_test", callID: "call_1" }, output)
  return output
}

test("an edited patch runs the edit, on the same object opencode holds", async () => {
  answer = allow({ patchText: "*** edited patch" })
  const args = { patchText: "*** original patch" }
  const out = await call("apply_patch", args)
  assert.equal(out.args, args)
  assert.deepEqual(args, { patchText: "*** edited patch" })
})

test("an edited MCP call is applied whole: changed, added and removed keys", async () => {
  answer = allow({ query: "edited", limit: 5 })
  const args = { query: "original", verbose: true }
  await call("mcp_search", args)
  assert.deepEqual(args, { query: "edited", limit: 5 })
})

test("an edited path comes back under opencode's own argument names", async () => {
  answer = allow({ file_path: "b.txt", old_string: "x", new_string: "y" })
  const args = { filePath: "a.txt", oldString: "x", newString: "y" }
  await call("edit", args)
  assert.deepEqual(args, { filePath: "b.txt", oldString: "x", newString: "y" })
})

test("an edit that cannot be applied refuses the call and leaves the original unrun", async () => {
  for (const bad of ["not an object", ["a", "list"], null]) {
    answer = allow(bad)
    const args = { patchText: "*** original patch" }
    await assert.rejects(call("apply_patch", args), /could not be applied/)
  }
})

test("a deny refuses the call with atrium's reason", async () => {
  answer = JSON.stringify({
    hookSpecificOutput: { permissionDecision: "deny", permissionDecisionReason: "not today" },
  })
  await assert.rejects(call("bash", { command: "rm -rf /" }), /not today/)
})

test("no answer from atrium lets the original through unchanged", async () => {
  answer = ""
  const args = { command: "echo hi" }
  await call("bash", args)
  assert.deepEqual(args, { command: "echo hi" })
})

test("a plain approval runs the original unchanged", async () => {
  answer = JSON.stringify({ hookSpecificOutput: { permissionDecision: "allow" } })
  const args = { command: "echo hi" }
  await call("bash", args)
  assert.deepEqual(args, { command: "echo hi" })
})

test("a patch is sent to the board once, under its own name", async () => {
  answer = ""
  sent.length = 0
  await call("apply_patch", { patchText: "*** the patch" })
  const perm = sent.find((s) => s.event === "permission")
  assert.ok(perm, "no permission request was sent")
  assert.deepEqual(perm.payload.tool_input, { patchText: "*** the patch" })
  assert.equal(perm.payload.tool_name, "Edit")
})
