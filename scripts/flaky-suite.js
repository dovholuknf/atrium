#!/usr/bin/env node
// Reads scripts/flaky-suite.json for ci.sh and test-touched.sh, and checks every entry still names a test.
//   node scripts/flaky-suite.js go-pkgs     the go packages, as ./path, space separated
//   node scripts/flaky-suite.js go-regex    the go test names, as ^(A|B)$
//   node scripts/flaky-suite.js board-units the board units, comma separated
//   node scripts/flaky-suite.js check       exit 1 when an entry names a test that does not exist
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const root = path.join(__dirname, "..");
const entries = JSON.parse(fs.readFileSync(path.join(__dirname, "flaky-suite.json"), "utf8")).entries;
const go = entries.filter(e => e.kind === "go");
const board = entries.filter(e => e.kind === "board");

function check() {
  const bad = [];
  const seen = new Set();
  for (const e of entries) {
    if (e.kind !== "go" && e.kind !== "board") { bad.push("entry " + JSON.stringify(e.name) + " has kind " + JSON.stringify(e.kind)); continue; }
    if (!e.name || !e.reason) bad.push("an entry of kind " + e.kind + " lacks a name or a reason");
    const key = e.kind + " " + (e.package || "") + " " + e.name;
    if (seen.has(key)) bad.push("listed twice: " + key);
    seen.add(key);
    if (e.kind === "go" && !/^Test\w+$/.test(e.name || "")) bad.push("go entry " + e.name + " is not a Test function name");
  }
  for (const e of go) {
    const dir = path.join(root, e.package || "");
    let files = [];
    try { files = fs.readdirSync(dir).filter(f => f.endsWith("_test.go")); } catch (x) { bad.push("go package " + e.package + " has no folder"); continue; }
    const re = new RegExp("^func " + e.name + "[(]", "m");
    if (!files.some(f => re.test(fs.readFileSync(path.join(dir, f), "utf8")))) bad.push("no func " + e.name + " in " + e.package);
  }
  if (board.length) {
    const r = spawnSync(process.execPath, [path.join(__dirname, "test-board-headless.js")], { env: Object.assign({}, process.env, { HEADLESS_LIST: "1" }), encoding: "utf8" });
    const json = (r.stdout || "").split("\n").reverse().find(l => l.startsWith("{"));
    if (!json) bad.push("the harness listed no units");
    else {
      const units = JSON.parse(json).units;
      for (const e of board) if (!units.includes(e.name)) bad.push("no board unit " + e.name);
    }
  }
  if (bad.length) { console.error("flaky-suite.json names tests that do not exist or is malformed:\n  " + bad.join("\n  ")); return 1; }
  console.log("ok: " + entries.length + " flaky-suite entries all name a test that exists");
  return 0;
}

const cmd = process.argv[2];
if (cmd === "go-pkgs") console.log([...new Set(go.map(e => "./" + e.package))].join(" "));
else if (cmd === "go-regex") console.log(go.length ? "^(" + go.map(e => e.name).join("|") + ")$" : "");
else if (cmd === "board-units") console.log(board.map(e => e.name).join(","));
else if (cmd === "check") process.exit(check());
else { console.error("usage: flaky-suite.js go-pkgs|go-regex|board-units|check"); process.exit(2); }
