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

  // No event file: the body comes from the run's own context. Every GITHUB_ variable is set, so a real run's own do not leak in.
  const ctx = { GITHUB_REPOSITORY: "o/r", GITHUB_RUN_ID: "42", GITHUB_RUN_ATTEMPT: "3", GITHUB_WORKFLOW: "ci",
    GITHUB_WORKFLOW_REF: "o/r/.github/workflows/ci.yml@refs/heads/x", GITHUB_HEAD_REF: "", GITHUB_REF_NAME: "feat/a\"b",
    GITHUB_SHA: "abc1234", GITHUB_EVENT_NAME: "push", GITHUB_EVENT_PATH: "", GITHUB_SERVER_URL: "https://github.com" };
  const own = path.join(tmp, "own.json");
  r = await sh(["--out", own], ctx);
  if (r.code !== 0 || r.stdout !== own) fail("a body from the run's context exited " + r.code + ": " + r.stderr);
  else {
    let b = null;
    try { b = JSON.parse(fs.readFileSync(own, "utf8")); } catch (e) { fail("the context body is not JSON: " + e.message); }
    const w = b && b.workflow_run;
    if (!w || w.id !== 42 || w.run_attempt !== 3 || w.name !== "ci" || w.path !== ".github/workflows/ci.yml" ||
        w.head_branch !== 'feat/a"b' || w.head_sha !== "abc1234" || w.event !== "push" || w.conclusion !== "failure" ||
        w.html_url !== "https://github.com/o/r/actions/runs/42" || b.repository.full_name !== "o/r")
      fail("the context body was " + JSON.stringify(b));
  }
  const prEvent = path.join(tmp, "pr-event.json");
  fs.writeFileSync(prEvent, '{"pull_request":{"head":{"sha":"deadbee"}}}');
  r = await sh(["--out", own, "--conclusion", "timed_out"],
    { ...ctx, GITHUB_EVENT_NAME: "pull_request", GITHUB_HEAD_REF: "feat/pr", GITHUB_EVENT_PATH: prEvent });
  if (r.code !== 0) fail("a pull request body exited " + r.code + ": " + r.stderr);
  else if (spawnSync("jq", ["--version"]).status === 0) {
    const w = JSON.parse(fs.readFileSync(own, "utf8")).workflow_run;
    if (w.head_branch !== "feat/pr" || w.head_sha !== "deadbee" || w.conclusion !== "timed_out" || w.event !== "pull_request")
      fail("the pull request body was " + JSON.stringify(w));
  }
  r = await sh(["--out", own, "--conclusion", "bogus"], ctx);
  if (r.code !== 2) fail("a bad conclusion exited " + r.code + ", wanted 2");
  r = await sh(["--out", own], { ...ctx, GITHUB_RUN_ID: "x1" });
  if (r.code !== 2) fail("a non-numeric run id exited " + r.code + ", wanted 2");

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
