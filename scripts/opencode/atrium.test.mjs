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
process.env.ATRIUM_HOME = "/cards/c1"
const { AtriumPlugin } = await import("./atrium.js")
const client = {}
const hooks = await AtriumPlugin({ client, directory: process.cwd() })
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

const replies = []
client.permission = { reply: async (r) => { replies.push(r) } }

function native(behavior, message) {
  return JSON.stringify({ hookSpecificOutput: { hookEventName: "PermissionRequest", decision: { behavior, message } } })
}

async function ask(props) {
  replies.length = 0
  sent.length = 0
  await hooks.event({ event: { type: "permission.asked", properties: { sessionID: "ses_test", ...props } } })
  for (let i = 0; i < 20; i++) await new Promise((r) => setImmediate(r))
}

const outside = {
  permission: "external_directory", patterns: ["/other/x/*"], always: ["/other/x/*"],
  metadata: { filepath: "/other/x/a.txt", parentDir: "/other/x" },
}

test("opencode's own ask goes to atrium as a PermissionRequest and an allow replies once", async () => {
  answer = native("allow")
  await ask({ id: "per_1", ...outside })
  const perm = sent.find((s) => s.event === "permission")
  assert.equal(perm.payload.hook_event_name, "PermissionRequest")
  assert.equal(perm.payload.tool_name, "external_directory")
  assert.equal(perm.payload.tool_input.file_path, "/other/x/a.txt")
  assert.deepEqual(replies, [{ requestID: "per_1", reply: "once", message: undefined }])
})

test("a deny from atrium rejects opencode's ask with atrium's reason", async () => {
  answer = native("deny", "not there")
  await ask({ id: "per_2", ...outside })
  assert.deepEqual(replies, [{ requestID: "per_2", reply: "reject", message: "not there" }])
})

test("no answer from atrium leaves opencode's ask to its own terminal", async () => {
  answer = ""
  await ask({ id: "per_3", ...outside })
  assert.ok(sent.find((s) => s.event === "permission"))
  assert.deepEqual(replies, [])
})

test("an ask inside the card's home is answered once without asking atrium", async () => {
  answer = native("deny", "must not be asked")
  await ask({
    id: "per_4", permission: "external_directory", patterns: ["/cards/c1/*"],
    metadata: { filepath: "/cards/c1/BRIEF.md", parentDir: "/cards/c1" },
  })
  assert.equal(sent.filter((s) => s.event === "permission").length, 0)
  assert.deepEqual(replies, [{ requestID: "per_4", reply: "once", message: undefined }])
})

test("a home-like sibling folder is not the card's home", async () => {
  answer = ""
  await ask({ id: "per_5", permission: "external_directory", patterns: ["/cards/c10/*"], metadata: {} })
  assert.ok(sent.find((s) => s.event === "permission"))
  assert.deepEqual(replies, [])
})

test("an ask for a call atrium just approved is answered once without asking again", async () => {
  answer = JSON.stringify({ hookSpecificOutput: { permissionDecision: "allow" } })
  await before({ tool: "edit", sessionID: "ses_test", callID: "call_ok" }, { args: { filePath: "/other/x/a.txt" } })
  answer = native("deny", "must not be asked")
  await ask({ id: "per_6", ...outside, tool: { messageID: "m", callID: "call_ok" } })
  assert.equal(sent.filter((s) => s.event === "permission").length, 0)
  assert.deepEqual(replies, [{ requestID: "per_6", reply: "once", message: undefined }])
  await hooks["tool.execute.after"]({ tool: "edit", sessionID: "ses_test", callID: "call_ok" })
  answer = ""
  await ask({ id: "per_7", ...outside, tool: { messageID: "m", callID: "call_ok" } })
  assert.deepEqual(replies, [])
})

test("a v1 client is answered on the session route", async () => {
  const v2 = client.permission
  delete client.permission
  const posted = []
  client.postSessionIdPermissionsPermissionId = async (o) => { posted.push(o) }
  try {
    answer = native("allow")
    await ask({ id: "per_8", ...outside })
    assert.deepEqual(posted, [{ path: { id: "ses_test", permissionID: "per_8" }, body: { response: "once" } }])
  } finally {
    client.permission = v2
    delete client.postSessionIdPermissionsPermissionId
  }
})
