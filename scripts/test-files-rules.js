// The file drawer's rules, RUN against stubs: when "open in your editor" is offered, how a folder is sorted and
// filtered, and the breadcrumb's steps. Pure functions in internal/api/web/js/files-rules.js, so no browser.
const fs = require("fs");
const path = require("path");
const vm = require("vm");

let bad = 0;
function fail(msg) { console.error("FAIL: " + msg); bad++; }

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "files-rules.js"), "utf8");
const ctx = { console };
vm.createContext(ctx);
vm.runInContext(src + "\nthis.api = { fileOpensHere, filesShownBy, crumbSteps, cardRelPath };", ctx);
const { fileOpensHere, filesShownBy, crumbSteps, cardRelPath } = ctx.api;

// OPEN IN YOUR EDITOR only where the window opens in front of the person at the browser.
const rooms = [{ name: "sg4", transport: "local" }, { name: "sgg", transport: "ziti" }, { name: "m1mini", transport: "mtls" }];
const cases = [
  ["a card on the board's own daemon, browser on loopback", "01abc", "127.0.0.1", true],
  ["the same over localhost", "01abc", "localhost", true],
  ["the same over ipv6 loopback", "01abc", "[::1]", true],
  ["a card on the hub's own room", "sg4~01abc", "127.0.0.1", true],
  ["a card on a remote room (Windows)", "sgg~01abc", "127.0.0.1", false],
  ["a card on a remote room (macOS)", "m1mini~01abc", "127.0.0.1", false],
  ["a room the hub does not list", "pi5~01abc", "127.0.0.1", false],
  ["the board's own card, browser elsewhere", "01abc", "sg4.lan", false],
  ["the hub's own room, browser over a share", "sg4~01abc", "board.example.zrok.io", false],
  ["no card", "", "127.0.0.1", true],
];
for (const [what, id, host, want] of cases) {
  const got = fileOpensHere(id, host, rooms);
  if (got !== want) fail(`open here, ${what}: ${got}, wanted ${want}`);
}
if (fileOpensHere("sg4~01abc", "127.0.0.1", undefined)) fail("open here with no room list said yes for a room card");

// SORTED, FOLDERS FIRST whatever the key, and filtered by name without case.
const rows = [
  { name: "b.txt", size: 10, mtime: "2026-11-12T08:00:00Z" },
  { name: "Zdir", dir: true, mtime: "2026-11-01T00:00:00Z" },
  { name: "a10.log", size: 500, mtime: "2026-11-10T00:00:00Z" },
  { name: "a9.log", size: 5, mtime: "2026-11-12T09:00:00Z" },
  { name: "adir", dir: true, mtime: "2026-11-12T10:00:00Z" },
];
const names = r => Array.from(r, e => e.name).join(",");
let got = names(filesShownBy(rows, "", { key: "name" }));
if (got !== "adir,Zdir,a9.log,a10.log,b.txt") fail("by name: " + got);
got = names(filesShownBy(rows, "", { key: "name", desc: true }));
if (got !== "Zdir,adir,b.txt,a10.log,a9.log") fail("by name, descending: " + got);
got = names(filesShownBy(rows, "", { key: "size", desc: true }));
if (!got.startsWith("adir,Zdir,") || !got.endsWith("a10.log,b.txt,a9.log")) fail("by size, largest first: " + got);
got = names(filesShownBy(rows, "", { key: "mtime", desc: true }));
if (got !== "adir,Zdir,a9.log,b.txt,a10.log") fail("by modified, newest first: " + got);
got = names(filesShownBy(rows, "A1", { key: "name" }));
if (got !== "a10.log") fail("filter A1: " + got);
got = names(filesShownBy(rows, "dir", { key: "name" }));
if (got !== "adir,Zdir") fail("filter dir: " + got);
if (filesShownBy(rows, "zzz", { key: "name" }).length !== 0) fail("a filter that matches nothing kept rows");
if (rows[0].name !== "b.txt") fail("sorting changed the rows it was given");

// CRUMBS. A Windows path asked in another case still walks from the card, and a folder outside the card is named whole.
const steps = (root, at) => Array.from(crumbSteps(root, at), p => p.label + (p.whole ? "*" : "")).join(" / ");
const want = [
  ["V:/cards/x/work", "V:/cards/x/work/img/a", "work / img / a"],
  ["V:/cards/x/work", "v:/cards/x/work/img", "work / img"],
  ["V:/cards/x/work", "v:/CARDS/x/WORK", "work"],
  ["/Users/claude/cards/x/work", "/Users/claude/cards/x/work/docs", "work / docs"],
  ["/Users/claude/cards/x/work", "/users/claude/cards/x/work/docs", "work / /users/claude/cards/x/work/docs*"],
  ["/home/c/cards/x/work", "/home/c/cards/x/workshop", "work / /home/c/cards/x/workshop*"],
  ["V:/cards/x/work", "V:/localai/open/notes", "work / V:/localai/open/notes*"],
  ["V:/cards/x/work/", "V:/cards/x/work/", "work"],
];
for (const [root, at, w] of want) {
  const g = steps(root, at);
  if (g !== w) fail(`crumbs for ${at} under ${root}: "${g}", wanted "${w}"`);
}
const deep = crumbSteps("v:/cards/x/work", "V:/cards/x/work/a/b");
if (deep[2].path !== "v:/cards/x/work/a/b") fail("a crumb's path is not under the root it walks from: " + deep[2].path);

// THE CARD'S OWN SPELLING of a folder, shown beside the absolute one on hovering the breadcrumb.
for (const [root, at, w] of [
  ["V:/cards/x/work", "V:/cards/x/work/img", "/work/img"],
  ["V:/cards/x/work/", "v:/cards/x/work", "/work"],
  ["/home/c/cards/x/work", "/home/c/cards/x/work/a/b", "/work/a/b"],
  ["/home/c/cards/x/work", "/home/c/cards/x/workshop", ""],
  ["V:/cards/x/work", "V:/localai/open/notes", ""],
  ["", "/a", ""],
]) {
  const g = cardRelPath(root, at);
  if (g !== w) fail(`card path for ${at} under ${root}: "${g}", wanted "${w}"`);
}

if (!bad) console.log("test-files-rules ok");
process.exit(bad ? 1 : 0);
