// Run terminalLabel from index.html against the cards on a RUNNING board.
//
//   node scripts/check-terminal-titles.js internal/api/web/index.html
//
// Not part of `check-board.sh`, because it needs a daemon. Run it by hand when
// the naming changes.
//
// It exists because the rule cannot be reasoned about from the code: the cases
// that break it are all in real data. A worktree directory that disagrees with
// its branch (`.../desktop-edge-win/fix-app-version` on branch
// `promote-2.11.3.1-and-beta`), a repo whose name matches its org, and a card
// with no repo at all. Two of those produced a label naming the branch twice,
// and neither was visible by reading the function.
const fs = require("fs");
const http = require("http");

const html = fs.readFileSync(process.argv[2], "utf8");

// Pull the function out of the page rather than copying it, so this tests what
// actually ships.
const start = html.indexOf("function terminalLabel(task) {");
if (start < 0) { console.error("terminalLabel not found"); process.exit(1); }
let depth = 0, i = html.indexOf("{", start), end = -1;
for (; i < html.length; i++) {
  if (html[i] === "{") depth++;
  else if (html[i] === "}") { depth--; if (depth === 0) { end = i + 1; break; } }
}
const src = html.slice(start, end);
const terminalLabel = new Function(src + "; return terminalLabel;")();

http.get("http://localhost:7778/v1/tasks", res => {
  let body = "";
  res.on("data", c => body += c);
  res.on("end", () => {
    const tasks = JSON.parse(body).tasks || [];
    let bad = 0;
    for (const t of tasks) {
      const label = terminalLabel(t);
      // A drive letter says nothing, and a branch printed twice reads as two
      // different things.
      const dup = t.branch && label.split(":")[0].endsWith("/" + t.branch);
      const flag = (/^[A-Za-z]:/.test(label) || dup) ? "  <-- LOOK" : "";
      if (flag) bad++;
      console.log(label.padEnd(56) + flag);
      console.log("    from " + (t.worktree || "(no dir)") +
        "  repo=" + (t.repo || "-") + "  branch=" + (t.branch || "-"));
    }
    // Synthetic cases the live board does not happen to contain.
    console.log("\n--- edge cases ---");
    for (const t of [
      { worktree: "D:/worktrees/github/openziti/ziti/fix-arg-parsing", repo: "ziti", branch: "fix-arg-parsing" },
      { worktree: "D:/git/github/dovholuknf/atrium", repo: "atrium", branch: "main" },
      { worktree: "D:/scratch/notes", repo: "", branch: "", display_title: "notes" },
      { worktree: "", repo: "", branch: "", display_title: "nowhere" },
    ]) {
      console.log(terminalLabel(t).padEnd(56) + "  <- " + (t.worktree || "(no dir)"));
    }
    if (bad) { console.error("\n" + bad + " label(s) still show a drive or the worktree root."); process.exit(1); }
    console.log("\nno label leaks a drive letter or the worktree root.");
  });
}).on("error", e => { console.error("board not reachable: " + e.message); process.exit(1); });
