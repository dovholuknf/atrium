#!/usr/bin/env node
// subject: scripts/check-card-files.sh, scripts/test-touched.sh
//
// check-card-files.sh in a scratch repo: a root REPORT.md, BRIEF.md or QUESTIONS.md committed or staged is refused,
// an untracked one, one in a subfolder and a deleted one are not.
const fs = require("fs");
const os = require("os");
const path = require("path");
const { execFileSync, spawnSync } = require("child_process");

const script = path.join(__dirname, "check-card-files.sh");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "card-files-"));
const hooks = fs.mkdtempSync(path.join(os.tmpdir(), "card-files-hooks-"));
const git = (...a) => execFileSync("git", ["-c", "core.hooksPath=" + hooks, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", ...a], { cwd: dir, stdio: "pipe" });
const write = (f, s) => { fs.mkdirSync(path.dirname(path.join(dir, f)), { recursive: true }); fs.writeFileSync(path.join(dir, f), s); };
const run = () => spawnSync("bash", [script, "base"], { cwd: dir, encoding: "utf8" });

let bad = 0;
const expect = (name, code) => {
  const r = run();
  if (r.status !== code) { bad++; console.error("FAIL " + name + ": exit " + r.status + ", want " + code + "\n" + r.stdout + r.stderr); }
};

try {
  git("init", "-q", "-b", "base");
  write("a.txt", "a\n");
  write("REPORT.md", "old\n");
  git("add", "-A"); git("commit", "-qm", "base");
  git("checkout", "-qb", "work");

  write("BRIEF.md", "untracked\n");
  write("QUESTIONS.md", "untracked\n");
  expect("untracked root files pass", 0);

  git("rm", "-q", "REPORT.md"); git("commit", "-qm", "rm");
  expect("removing a root REPORT.md passes", 0);

  write("docs/REPORT.md", "kept\n");
  git("add", "docs/REPORT.md"); git("commit", "-qm", "sub");
  expect("a REPORT.md in a subfolder passes", 0);

  for (const f of ["REPORT.md", "BRIEF.md", "QUESTIONS.md"]) {
    write(f, "x\n");
    git("add", "-f", f);
    expect("a staged root " + f + " fails", 1);
    git("commit", "-qm", f);
    expect("a committed root " + f + " fails", 1);
    git("rm", "-q", "--cached", f); git("commit", "-qm", "un " + f);
    expect("a root " + f + " taken back out passes", 0);
  }
} finally {
  fs.rmSync(dir, { recursive: true, force: true });
  fs.rmSync(hooks, { recursive: true, force: true });
}
if (bad) { console.error(bad + " failed"); process.exit(1); }
console.log("card files ok");
