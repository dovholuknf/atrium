// scripts/ci-notify.sh, RUN against fixture events and a loopback server standing in for the hub.
// subject: scripts/ci-notify.sh, scripts/ci-notify.ps1
const fs = require("fs");
const os = require("os");
const path = require("path");
const http = require("http");
const { spawn, spawnSync } = require("child_process");

const root = path.join(__dirname, "..");
const script = path.join(__dirname, "ci-notify.sh");
const runEvent = path.join(__dirname, "fixtures", "ci-workflow-run.json");
const pushEvent = path.join(__dirname, "fixtures", "ci-push-event.json");
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "ci-notify-"));

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

function sh(args, env) {
  return new Promise(resolve => {
    const p = spawn("bash", [script, ...args], { cwd: tmp, env: { ...process.env, ATRIUM_CI_HOOK_SECRET: "", ...env } });
    let stdout = "", stderr = "";
    p.stdout.on("data", d => (stdout += d));
    p.stderr.on("data", d => (stderr += d));
    p.on("close", code => resolve({ code, stdout: stdout.trim(), stderr }));
  });
}

(async () => {
  if (spawnSync("bash", ["-c", "true"]).status !== 0) {
    console.log("skip: no bash on PATH");
    return;
  }

  const out = path.join(tmp, "body.json");
  let r = await sh([runEvent, "--out", out]);
  if (r.code !== 0) fail("a workflow_run event exited " + r.code + ": " + r.stderr);
  if (r.stdout !== out) fail("printed " + r.stdout + ", wanted the body path");
  if (!fs.readFileSync(out).equals(fs.readFileSync(runEvent))) fail("the body is not the event as is");

  const none = path.join(tmp, "none.json");
  r = await sh([pushEvent, "--out", none]);
  if (r.code !== 3) fail("a push event exited " + r.code + ", wanted 3");
  if (fs.existsSync(none)) fail("a push event wrote a body");

  r = await sh([path.join(tmp, "missing.json")]);
  if (r.code !== 2) fail("a missing file exited " + r.code + ", wanted 2");

  r = await sh([runEvent, "--out", out, "--url", "http://127.0.0.1:1/"]);
  if (r.code !== 2) fail("a url with no secret exited " + r.code + ", wanted 2");

  let seen = null, answer = 202;
  const srv = http.createServer((req, res) => {
    const chunks = [];
    req.on("data", c => chunks.push(c));
    req.on("end", () => {
      seen = { method: req.method, url: req.url, secret: req.headers["x-ziti-hook-secret"], type: req.headers["content-type"],
        body: Buffer.concat(chunks) };
      res.writeHead(answer, { "Content-Type": "application/json" });
      res.end('{"run":"r1","targets":0}');
    });
  });
  await new Promise(ok => srv.listen(0, "127.0.0.1", ok));
  const url = "http://127.0.0.1:" + srv.address().port + "/_hub/ci-hook";

  r = await sh([runEvent, "--out", out, "--url", url, "--secret", "s3cret"]);
  if (r.code !== 0) fail("a post exited " + r.code + ": " + r.stderr);
  if (!seen || seen.method !== "POST" || seen.url !== "/_hub/ci-hook") fail("the hub saw " + JSON.stringify(seen && seen.url));
  else {
    if (seen.secret !== "s3cret") fail("the secret header was " + seen.secret);
    if (!/application\/json/.test(seen.type)) fail("the content type was " + seen.type);
    if (!seen.body.equals(fs.readFileSync(runEvent))) fail("the posted body is not the event");
  }
  if (r.stdout.includes("s3cret") || r.stderr.includes("s3cret")) fail("the secret was printed");

  seen = null;
  r = await sh([runEvent, "--out", out, "--url", url], { ATRIUM_CI_HOOK_SECRET: "fromenv" });
  if (r.code !== 0 || !seen || seen.secret !== "fromenv") fail("the secret did not come from the environment");

  answer = 401;
  r = await sh([runEvent, "--out", out, "--url", url, "--secret", "wrong"]);
  if (r.code !== 1) fail("a refused post exited " + r.code + ", wanted 1");

  srv.close();
  fs.rmSync(tmp, { recursive: true, force: true });
  if (bad) {
    console.error(bad + " failed");
    process.exit(1);
  }
  console.log("ci-notify: ok");
})();
