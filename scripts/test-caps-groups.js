// The capabilities matrix's group rule, RUN against stubs: a group of only pass and n/a starts collapsed, any other starts
// open, and a click overrides. Pure functions in internal/api/web/js/caps.js, so no browser.
const fs = require("fs");
const path = require("path");
const vm = require("vm");

let bad = 0;
function fail(msg) { console.error("FAIL: " + msg); bad++; }

const src = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "js", "caps.js"), "utf8");
const ctx = { document: { getElementById() { return null; }, querySelector() { return null; } }, console, setTimeout, clearTimeout };
vm.createContext(ctx);
vm.runInContext(src + "\nthis.api = { caps, capsStates, capsCells, capsGroupGreen, capsGroupIsOpen, capsFix, capsRerunFailed };", ctx);
const { caps, capsStates, capsCells, capsGroupGreen, capsGroupIsOpen, capsFix, capsRerunFailed } = ctx.api;

const at = new Date().toISOString();
const res = (room, cap, state) => ({ room, cap, state, at });
const data = {
  caps: [
    { id: "go", kind: "toolchain" }, { id: "gcc", kind: "toolchain" },
    { id: "ssh:cdwsl", kind: "reach", optional: true },
    { id: "build:atrium", kind: "build", not_expected: ["room-a"] },
    { id: "gh-auth", kind: "access" }
  ],
  rooms: [{ name: "room-d", online: true }, { name: "room-a", online: true, running: ["gh-auth"] }],
  results: [
    res("room-d", "go", "pass"), res("room-d", "gcc", "fail"), res("room-d", "ssh:cdwsl", "fail"), res("room-d", "build:atrium", "pass"),
    res("room-d", "gh-auth", "pass"),
    res("room-a", "go", "pass"), res("room-a", "gcc", "pass"), res("room-a", "ssh:cdwsl", "fail"), res("room-a", "build:atrium", "fail"),
    res("room-a", "gh-auth", "pass")
  ]
};
const rows = capsCells(data);
const grp = kind => rows.filter(x => x.cap.kind === kind);

if (capsStates({ optional: true }, "a", { state: "fail" }, false) !== "na") fail("an optional failure is not n/a");
if (capsStates({ not_expected: ["a"] }, "a", { state: "fail" }, false) !== "na") fail("a not-expected failure is not n/a");
if (capsStates({ not_expected: ["b"] }, "a", { state: "fail" }, false) !== "fail") fail("not expected on another room hid a failure");
if (capsStates({ optional: true }, "a", { state: "pass" }, false) !== "pass") fail("an optional pass is not pass");
if (capsStates({}, "a", null, false) !== "unknown") fail("no result is not unknown");

if (capsGroupIsOpen("reach", grp("reach"))) fail("reach is only n/a and should start collapsed");
if (capsGroupIsOpen("build", grp("build"))) fail("build is pass and a not-expected miss and should start collapsed");
if (!capsGroupIsOpen("toolchain", grp("toolchain"))) fail("toolchain has a fail and should start open");
if (!capsGroupIsOpen("access", grp("access"))) fail("access has a running probe and should start open");
if (capsGroupGreen(grp("access"))) fail("a running cell counted as green");

caps.open.toolchain = false;
if (capsGroupIsOpen("toolchain", grp("toolchain"))) fail("a click did not collapse an open group");
caps.open.reach = true;
if (!capsGroupIsOpen("reach", grp("reach"))) fail("a click did not open a collapsed group");

const busy = capsRerunFailed(409, { unreached: { m1try: "{\"error\":\"a capability run is already going\"}" } });
if (!/m1try is already probing/.test(busy)) fail("a busy room was not named as already probing: " + busy);
const down = capsRerunFailed(502, { unreached: { m1try: "{\"error\":\"boom\"}" } });
if (down !== "Not re-run. m1try: boom") fail("a failed re-run did not show the room's reason: " + down);
// A red cell says what to do: the capability's own fix, else one by its kind, naming the room.
if (!/^Run gh auth login.*Re-run.$/.test(capsFix({ id: "gh-auth", kind: "access", fix: "Run gh auth login on the room's machine" }, "room-a"))) fail("a fix from the list is not used");
if (!/Install gcc on room-d/.test(capsFix({ id: "gcc", kind: "toolchain" }, "room-d"))) fail("a toolchain miss does not say to install it on its room");
if (!/room-d can reach cdwsl/.test(capsFix({ id: "ssh:cdwsl", kind: "reach" }, "room-d"))) fail("a reach miss does not name the host");

if (bad) process.exit(1);
console.log("caps groups: all green collapses, anything else opens, and a click wins.");
