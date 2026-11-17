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
vm.runInContext(src + "\nthis.api = { caps, capsStates, capsCells, capsGroupGreen, capsGroupIsOpen };", ctx);
const { caps, capsStates, capsCells, capsGroupGreen, capsGroupIsOpen } = ctx.api;

const at = new Date().toISOString();
const res = (room, cap, state) => ({ room, cap, state, at });
const data = {
  caps: [
    { id: "go", kind: "toolchain" }, { id: "gcc", kind: "toolchain" },
    { id: "ssh:cdwsl", kind: "reach", optional: true },
    { id: "build:atrium", kind: "build", not_expected: ["m1mini"] },
    { id: "gh-auth", kind: "access" }
  ],
  rooms: [{ name: "sg3", online: true }, { name: "m1mini", online: true, running: ["gh-auth"] }],
  results: [
    res("sg3", "go", "pass"), res("sg3", "gcc", "fail"), res("sg3", "ssh:cdwsl", "fail"), res("sg3", "build:atrium", "pass"),
    res("sg3", "gh-auth", "pass"),
    res("m1mini", "go", "pass"), res("m1mini", "gcc", "pass"), res("m1mini", "ssh:cdwsl", "fail"), res("m1mini", "build:atrium", "fail"),
    res("m1mini", "gh-auth", "pass")
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

if (bad) process.exit(1);
console.log("caps groups: all green collapses, anything else opens, and a click wins.");
