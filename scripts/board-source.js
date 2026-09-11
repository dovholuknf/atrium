// The board as a browser ends up with it, in one string.
//
// The board is served as `index.html` plus `board.css` plus about two dozen
// plain scripts that share one global scope. Every checker in this directory
// predates that split and greps ONE file for markup and script together, and
// several assert things that span the two: a selector in the stylesheet that a
// handler has to match, a `data-` attribute written in the HTML and read in the
// script. Rewriting each of them to take three inputs would mean touching every
// assertion, and an assertion weakened to make a checker pass is the failure
// the checkers exist to prevent.
//
// So this puts the pieces back together instead. The stylesheet goes where the
// link is and the whole script goes where the tags are, IN LOAD ORDER, which is
// the order the browser runs them in.

const fs = require("fs");
const path = require("path");

const web = path.join(__dirname, "..", "internal", "api", "web");

// loadOrder is the js files the page names, in the order it names them.
//
// Read off the page rather than off the directory, because the order is the
// part that matters: these are classic scripts sharing a global scope, so a
// file that loads before the one declaring what it calls at load time throws a
// ReferenceError and stops the rest of that file dead.
function loadOrder() {
  const page = fs.readFileSync(path.join(web, "index.html"), "utf8");
  const names = [];
  const re = /src="\/js\/([A-Za-z0-9_-]+\.js)"/g;
  let m;
  while ((m = re.exec(page))) names.push(m[1]);
  return names;
}

// boardScript is every script the page loads, concatenated in load order.
function boardScript() {
  return loadOrder().map(n => fs.readFileSync(path.join(web, "js", n), "utf8")).join("");
}

// wholeBoard is the page with the stylesheet and the script inlined.
function wholeBoard() {
  const page = fs.readFileSync(path.join(web, "index.html"), "utf8");
  const css = fs.readFileSync(path.join(web, "board.css"), "utf8");
  let inlined = false;
  return page.split("\n").map(line => {
    if (line.trim() === '<link rel="stylesheet" href="/board.css">') {
      return "<style>\n" + css.replace(/\n$/, "") + "\n</style>";
    }
    if (/^<script src="\/js\/[A-Za-z0-9_-]+\.js"><\/script>$/.test(line.trim())) {
      if (inlined) return null;
      inlined = true;
      return "<script>\n" + boardScript().replace(/\n$/, "") + "\n</script>";
    }
    return line;
  }).filter(l => l !== null).join("\n");
}

module.exports = { web, loadOrder, boardScript, wholeBoard };

// Called directly, it writes the whole page to stdout, which is how the shell
// checker hands it to the node checkers without reimplementing any of this.
if (require.main === module) {
  process.stdout.write(process.argv[2] === "--script" ? boardScript() : wholeBoard());
}
