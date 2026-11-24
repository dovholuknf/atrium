// atrium's hooks for opencode, as an opencode plugin.
//
// opencode has no hooks file. What it has is plugins: a module in
// ~/.config/opencode/plugin/ (or .opencode/plugin/ in a project) whose default
// export returns handlers for named points in its run loop. This one maps those
// points onto the same atrium subcommands Claude Code's hooks run, so the board
// cannot tell the difference:
//
//   startup, session.created   atrium session --event start
//   process exit, dispose      atrium session --event end
//   tool.execute.before        atrium hook --event tool-start   (fire and forget)
//   tool.execute.before        atrium hook --event permission   (awaited: the gate)
//   tool.execute.after         atrium hook --event tool-end     (fire and forget)
//   chat.message               atrium hook --event prompt       (fire and forget)
//   session.created (child)    atrium hook --event subagent-start
//   session.idle (child)       atrium hook --event subagent-end
//   session.idle               atrium turn --event end
//   permission.asked           atrium hook --event permission   (as a PermissionRequest, then replied)
//
// Measured against opencode 1.18.34 by reading the installed binary, not the
// docs: tool.execute.before runs before opencode's own permission ask, and an
// error thrown from it fails that one tool call with the message as its result.
// `output.args` is the live argument object, so an edited command is applied by
// assigning to it.
//
// A HOOK MUST NEVER FAIL A SESSION, the rule every atrium hook follows. Every
// call below swallows its own errors. Activity has one second and is not
// waited on. The gate is waited on, because a human is answering it, and fails
// OPEN: anything other than an explicit deny from atrium lets the tool run and
// leaves opencode's own permission rules in charge.
//
// opencode's own asks (external_directory, and any rule set to `ask`) come after
// this hook, as a permission.asked event. Measured against 1.19.4: there is no
// plugin hook that answers one, but the in-process client can reply to it. So
// the ask goes to atrium as a PermissionRequest, the same native shape codex
// uses, and atrium's answer is sent back as `once` or `reject`. "Always" is an
// atrium rule, never opencode's. Two kinds of ask are answered `once` with no
// human: one for a call atrium itself approved a moment ago, and one for paths
// inside the card's own home (ATRIUM_HOME). No answer from atrium leaves the ask
// in opencode's terminal, which is failing open.
//
// Only a session atrium launched is reported. atrium sets ATRIUM_RUNNER on
// every launch, and without it this plugin does nothing, so an opencode started
// by hand in some other terminal is left alone.
//
// Install: copy this file to ~/.config/opencode/plugin/atrium.js. The atrium
// binary is the one the running daemon records in its location file, the same
// rule `atrium hook install` uses, then ATRIUM_HOOK_EXE, then `atrium` on PATH.

import { spawn, spawnSync } from "node:child_process"
import { readFileSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"

const RUNNER = "opencode"
const ACTIVITY_MS = 1000
const SESSION_MS = 3000
const TURN_MS = 2000

// opencode's tool ids, as Claude Code names the same tool. atrium's gate skips
// pure reads by Claude's name and its standing rules are written against them,
// so a rule that allows `Bash(git status)` should allow it here too. Anything
// not listed (an MCP tool, a plugin tool) goes through under its own name.
const TOOL_NAMES = {
  bash: "Bash",
  edit: "Edit",
  multiedit: "MultiEdit",
  write: "Write",
  patch: "Edit",
  apply_patch: "Edit",
  read: "Read",
  list: "Read",
  glob: "Glob",
  grep: "Grep",
  webfetch: "WebFetch",
  websearch: "WebSearch",
  codesearch: "WebSearch",
  todowrite: "TodoWrite",
  todoread: "TodoWrite",
  task: "Task",
  skill: "Skill",
  question: "AskUserQuestion",
}

// opencode's argument names, as Claude Code spells them. atrium's gate reads
// file_path, old_string and new_string to say what an edit does.
const ARG_NAMES = {
  filePath: "file_path",
  oldString: "old_string",
  newString: "new_string",
  replaceAll: "replace_all",
}

// The same map backwards, for an edited approval coming home.
const OPENCODE_ARGS = Object.fromEntries(Object.entries(ARG_NAMES).map(([k, v]) => [v, k]))

function claudeTool(tool) {
  return TOOL_NAMES[String(tool || "").toLowerCase()] || String(tool || "")
}

// claudeArgs renames and adds nothing else. Every key here is sent to the board,
// so an alias would put the same text in front of a human twice.
function claudeArgs(args) {
  if (!args || typeof args !== "object") return {}
  const out = {}
  for (const [k, v] of Object.entries(args)) out[ARG_NAMES[k] || k] = v
  return out
}

function isObject(v) {
  return v !== null && typeof v === "object" && !Array.isArray(v)
}

// applyEdit makes `args` say what the human approved, IN PLACE: opencode runs
// the tool on the very object it handed this hook, so a new object would be
// ignored and the original would run.
//
// The whole edited input, not a list of fields. atrium shows a tool it has no
// summary for as raw JSON, and an edit to that comes back as the whole input.
// Returns false when it cannot be applied, and the caller refuses the call
// rather than running something the human did not approve.
function applyEdit(upd, args) {
  if (!isObject(upd) || !isObject(args)) return false
  try {
    const want = {}
    for (const [k, v] of Object.entries(upd)) want[OPENCODE_ARGS[k] || k] = v
    for (const k of Object.keys(args)) if (!(k in want)) delete args[k]
    Object.assign(args, want)
    for (const [k, v] of Object.entries(want)) if (args[k] !== v) return false
    return true
  } catch {
    return false
  }
}

// opencode's ask metadata, as atrium's gate reads a tool input.
const ASK_ARGS = { filepath: "file_path", parentDir: "parent_dir" }

function askInput(ask) {
  const out = {}
  if (isObject(ask?.metadata)) for (const [k, v] of Object.entries(ask.metadata)) out[ASK_ARGS[k] || k] = v
  if (!out.command && !out.file_path && Array.isArray(ask?.patterns)) out.patterns = ask.patterns
  return out
}

function slashPath(p) {
  let s = String(p || "").replace(/\\/g, "/").replace(/\/?\*+$/, "").replace(/\/+$/, "")
  if (process.platform === "win32") s = s.toLowerCase()
  return s
}

// inHome is true only for an external_directory ask whose every path is the
// card's home or under it.
function inHome(ask, home) {
  const h = slashPath(home)
  if (!h || ask?.permission !== "external_directory") return false
  const meta = isObject(ask.metadata) ? ask.metadata : {}
  const paths = [...(Array.isArray(ask.patterns) ? ask.patterns : []),
    ...(Array.isArray(meta.directories) ? meta.directories : [])]
  if (meta.filepath) paths.push(meta.filepath)
  if (!paths.length) return false
  return paths.every((p) => {
    const s = slashPath(p)
    return s === h || s.startsWith(h + "/")
  })
}

function locationFile() {
  const env = process.env
  if (process.platform === "win32") {
    return join(env.LOCALAPPDATA || join(homedir(), "AppData", "Local"), "atrium", "daemon.json")
  }
  if (process.platform === "darwin") return join(homedir(), "Library", "Caches", "atrium", "daemon.json")
  if (env.XDG_RUNTIME_DIR) return join(env.XDG_RUNTIME_DIR, "atrium", "daemon.json")
  if (env.XDG_STATE_HOME) return join(env.XDG_STATE_HOME, "atrium", "daemon.json")
  return join(homedir(), ".local", "state", "atrium", "daemon.json")
}

// atriumExe is resolved per call, not once: the daemon can be redeployed to a
// new path while this session is open, and the location file follows it.
function atriumExe() {
  const override = (process.env.ATRIUM_HOOK_EXE || "").trim()
  if (override) return override
  try {
    const exe = String(JSON.parse(readFileSync(locationFile(), "utf8")).exe || "").trim()
    if (exe) return exe
  } catch {}
  return "atrium"
}

// run starts one atrium subcommand with a payload on stdin and resolves with
// whatever it printed, or "" on any failure. Never rejects.
function run(args, payload, timeoutMs, cwd) {
  return new Promise((resolve) => {
    let out = ""
    let done = false
    const finish = () => {
      if (done) return
      done = true
      clearTimeout(timer)
      resolve(out)
    }
    let child
    try {
      child = spawn(atriumExe(), args, { cwd, windowsHide: true, stdio: ["pipe", "pipe", "ignore"] })
    } catch {
      return resolve("")
    }
    const timer = timeoutMs > 0 ? setTimeout(() => {
      try { child.kill() } catch {}
      finish()
    }, timeoutMs) : undefined
    child.on("error", finish)
    child.on("close", finish)
    child.stdout?.on("data", (b) => { out += b.toString() })
    try {
      child.stdin.on("error", () => {})
      child.stdin.end(JSON.stringify(payload || {}))
    } catch {}
  })
}

export const AtriumPlugin = async ({ client, directory }) => {
  if (!(process.env.ATRIUM_RUNNER || "").trim()) return {}

  const cwd = directory || process.cwd()
  const base = (extra) => ({ cwd, ...extra })

  // The session this card is: the first top-level one. A task tool call makes
  // a child session that runs under the same card, and reporting its id as the
  // card's would make a resume pick up the subagent instead of the conversation.
  let mainSession = ""
  const children = new Set()
  const announced = new Set()

  const activity = (event, payload) => {
    run(["hook", "--event", event, "--runner", RUNNER], base(payload), ACTIVITY_MS, cwd)
  }

  const sessionStart = (sessionID, source) => {
    if (sessionID) {
      if (announced.has(sessionID)) return
      announced.add(sessionID)
    }
    run(["session", "--event", "start", "--runner", RUNNER],
      base({ session_id: sessionID || "", source: source || "startup" }), SESSION_MS, cwd)
  }

  // Claim the first top-level session seen, from whichever event names it
  // first. A resumed session (`--session`) is never created, so the first
  // message or tool call in it is what says which one it is.
  const noteSession = (sessionID, source) => {
    if (!sessionID || children.has(sessionID) || mainSession) return
    mainSession = sessionID
    sessionStart(sessionID, source)
  }

  // Once, whichever of exit and dispose comes first. Synchronous because an
  // exiting process does not wait for a promise.
  let ended = false
  const sessionEnd = (reason) => {
    if (ended) return
    ended = true
    try {
      spawnSync(atriumExe(), ["session", "--event", "end", "--runner", RUNNER], {
        cwd, windowsHide: true, timeout: SESSION_MS, stdio: ["pipe", "ignore", "ignore"],
        input: JSON.stringify(base({ session_id: mainSession, reason: reason || "exit" })),
      })
    } catch {}
  }
  process.once("exit", () => sessionEnd("exit"))

  // The card goes up when opencode opens, before anything has been typed,
  // which is what Claude Code's SessionStart does. No id yet: opencode makes a
  // session on the first message.
  sessionStart("", "startup")

  // A queued message for an idle card comes back from `atrium turn` as a Stop
  // block, and the reason is the message. opencode has no Stop hook to return
  // it to, so it is sent as the next prompt instead.
  const deliver = async (sessionID, text) => {
    const parts = [{ type: "text", text }]
    try {
      await client.session.promptAsync({ path: { id: sessionID }, body: { parts } })
      return
    } catch {}
    try {
      await client.session.promptAsync({ sessionID, parts })
    } catch {}
  }

  // Calls atrium approved in tool.execute.before, until they finish.
  const approved = new Set()

  // The plugin's client is opencode's own: v2 has permission.reply, v1 the session route.
  const reply = async (ask, answer, message) => {
    try {
      if (client?.permission?.reply) {
        await client.permission.reply({ requestID: ask.id, reply: answer, message })
        return
      }
      await client?.postSessionIdPermissionsPermissionId?.({
        path: { id: ask.sessionID, permissionID: ask.id }, body: { response: answer },
      })
    } catch {}
  }

  const onAsk = async (ask) => {
    if (!ask?.id) return
    if (approved.has(ask.tool?.callID) || inHome(ask, process.env.ATRIUM_HOME)) return reply(ask, "once")
    const payload = base({
      hook_event_name: "PermissionRequest", tool_name: claudeTool(ask.permission), tool_input: askInput(ask),
      session_id: ask.sessionID || "", tool_use_id: ask.id,
    })
    const out = await run(["hook", "--event", "permission", "--runner", RUNNER], payload, 0, cwd)
    let decision
    try { decision = JSON.parse(out || "null")?.hookSpecificOutput?.decision } catch {}
    if (decision?.behavior === "allow") return reply(ask, "once")
    if (decision?.behavior === "deny") return reply(ask, "reject", decision.message || "blocked via atrium")
  }

  return {
    event: async ({ event }) => {
      try {
        const p = event?.properties || {}
        switch (event?.type) {
          case "permission.asked":
            // Not awaited: a human may take minutes, and the event loop serves the whole session.
            onAsk(p).catch(() => {})
            break
          case "session.created": {
            const info = p.info || {}
            if (info.parentID) {
              children.add(info.id)
              activity("subagent-start", { agent_id: info.id, agent_type: info.title || "task" })
            } else {
              noteSession(info.id, "startup")
            }
            break
          }
          case "session.idle": {
            const id = p.sessionID
            if (children.has(id)) {
              children.delete(id)
              activity("subagent-end", { agent_id: id })
              break
            }
            if (id && id !== mainSession) break
            const out = await run(["turn", "--event", "end", "--runner", RUNNER],
              base({ session_id: id || "", hook_event_name: "Stop" }), TURN_MS, cwd)
            let ans
            try { ans = JSON.parse(out) } catch {}
            if (ans?.decision === "block" && typeof ans.reason === "string" && ans.reason.trim() && id) {
              await deliver(id, ans.reason)
            }
            break
          }
        }
      } catch {}
    },

    "chat.message": async (input) => {
      try {
        noteSession(input?.sessionID, "resume")
        if (input?.sessionID && !children.has(input.sessionID)) {
          activity("prompt", { session_id: input.sessionID })
        }
      } catch {}
    },

    "tool.execute.before": async (input, output) => {
      let ans
      try {
        noteSession(input?.sessionID, "resume")
        const tool = claudeTool(input?.tool)
        const toolInput = claudeArgs(output?.args)
        const payload = base({ tool_name: tool, tool_input: toolInput, session_id: input?.sessionID || "", tool_use_id: input?.callID || "" })
        activity("tool-start", payload)
        // No deadline: a human may take minutes. atrium bounds its own probe
        // and prints nothing when it is down, which lets the tool through.
        const out = await run(["hook", "--event", "permission", "--runner", RUNNER], payload, 0, cwd)
        ans = JSON.parse(out || "null")?.hookSpecificOutput
      } catch {
        return
      }
      if (!ans) return
      if (ans.permissionDecision === "allow" && input?.callID) approved.add(input.callID)
      if (ans.permissionDecision === "deny") {
        // The one error this plugin raises on purpose: it is how a tool call
        // is refused, and the reason is what the model reads.
        throw new Error("atrium denied this tool call: " + (ans.permissionDecisionReason || "blocked via atrium"))
      }
      // An approval of an edited call. What runs is the edit or nothing: the
      // original is exactly what the human changed their mind about.
      if (ans.permissionDecision === "allow" && ans.updatedInput !== undefined) {
        if (!applyEdit(ans.updatedInput, output?.args)) {
          throw new Error("atrium approved an edited version of this tool call that could not be " +
            "applied, so nothing was run")
        }
      }
    },

    "tool.execute.after": async (input) => {
      try {
        approved.delete(input?.callID)
        activity("tool-end", { tool_name: claudeTool(input?.tool), session_id: input?.sessionID || "", tool_use_id: input?.callID || "" })
      } catch {}
    },

    dispose: async () => {
      sessionEnd("exit")
    },
  }
}

export default AtriumPlugin
