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

function claudeTool(tool) {
  return TOOL_NAMES[String(tool || "").toLowerCase()] || String(tool || "")
}

function claudeArgs(args) {
  if (!args || typeof args !== "object") return {}
  const out = {}
  for (const [k, v] of Object.entries(args)) out[ARG_NAMES[k] || k] = v
  // A patch carries its whole change as text. Shown as the content so the
  // board has something better than raw JSON to put in front of a human.
  if (typeof args.patchText === "string" && out.content === undefined) out.content = args.patchText
  return out
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

  return {
    event: async ({ event }) => {
      try {
        const p = event?.properties || {}
        switch (event?.type) {
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
      if (ans.permissionDecision === "deny") {
        // The one error this plugin raises on purpose: it is how a tool call
        // is refused, and the reason is what the model reads.
        throw new Error("atrium denied this tool call: " + (ans.permissionDecisionReason || "blocked via atrium"))
      }
      // An approval with an edited command. Only the fields atrium edits are
      // mapped back, under opencode's own names.
      try {
        const upd = ans.permissionDecision === "allow" ? ans.updatedInput : undefined
        if (upd && output?.args && typeof output.args === "object") {
          if (typeof upd.command === "string" && "command" in output.args) output.args.command = upd.command
          if (typeof upd.file_path === "string" && "filePath" in output.args) output.args.filePath = upd.file_path
          if (typeof upd.url === "string" && "url" in output.args) output.args.url = upd.url
          if (typeof upd.pattern === "string" && "pattern" in output.args) output.args.pattern = upd.pattern
        }
      } catch {}
    },

    "tool.execute.after": async (input) => {
      try {
        activity("tool-end", { tool_name: claudeTool(input?.tool), session_id: input?.sessionID || "", tool_use_id: input?.callID || "" })
      } catch {}
    },

    dispose: async () => {
      sessionEnd("exit")
    },
  }
}

export default AtriumPlugin
