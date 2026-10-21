// The service worker's notification click: openBoard focuses a board window, never a doc or phone page, and opens a
// board when none is open. sw.js runs in a vm with a fake `self`.
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "sw.js"), "utf8");
const ORIGIN = "http://127.0.0.1:7778";
let failed = 0;
const check = (name, ok, extra) => { if (!ok) { failed++; console.error("FAIL: " + name + (extra ? ": " + extra : "")); } };

function load(urls, focusedUrl) {
  const log = { focused: [], posted: [], opened: [] };
  const clients = urls.map(url => ({
    url, focused: url === focusedUrl,
    focus: async () => { log.focused.push(url); },
    postMessage: m => { log.posted.push({ url, m }); },
  }));
  const self = {
    addEventListener() {}, skipWaiting() {}, location: { origin: ORIGIN },
    clients: { matchAll: async () => clients, claim: async () => {}, openWindow: async u => { log.opened.push(u); } },
    registration: { showNotification: async () => {} },
  };
  const ctx = vm.createContext({ self, URL, URLSearchParams, console, Promise, caches: {}, fetch: async () => ({}) });
  vm.runInContext(src, ctx);
  return { openBoard: vm.runInContext("openBoard", ctx), log };
}

(async () => {
  const board = ORIGIN + "/";
  const doc = ORIGIN + "/read.html?f=x.md";
  let t = load([doc, board]);
  await t.openBoard(ORIGIN, "board", "", "");
  check("board focused over a doc window", t.log.focused.join() === board, JSON.stringify(t.log));
  check("board gets the goTo", t.log.posted.length === 1 && t.log.posted[0].url === board && t.log.posted[0].m.view === "board", JSON.stringify(t.log));
  check("nothing opened with a board present", t.log.opened.length === 0, JSON.stringify(t.log));

  t = load([doc, ORIGIN + "/m/"]);
  await t.openBoard(ORIGIN, "board", "t1", "");
  check("a doc and a phone page only: a new board opens", t.log.opened.length === 1 && t.log.opened[0].startsWith(ORIGIN + "/?"), JSON.stringify(t.log));
  check("neither doc nor phone page focused", t.log.focused.length === 0, JSON.stringify(t.log));

  const a = ORIGIN + "/index.html", b = ORIGIN + "/room/sg3";
  t = load([a, b], b);
  await t.openBoard(ORIGIN, "", "", "");
  check("the focused board wins among several", t.log.focused.join() === b, JSON.stringify(t.log));

  t = load([ORIGIN + "/#term=abc", ORIGIN + "/room/sg3/alias/x"]);
  await t.openBoard(ORIGIN, "", "", "");
  check("popped-out terminals are not boards", t.log.opened.length === 1, JSON.stringify(t.log));

  if (failed) process.exit(1);
  console.log("sw openBoard ok");
})();
