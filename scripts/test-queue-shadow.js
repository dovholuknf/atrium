// A Queue row's shadow stays small: a wide blur is rasterized again for every tile that scrolls in, and made the list scroll slowly.
const fs = require("fs");
const path = require("path");

const css = fs.readFileSync(path.join(__dirname, "..", "internal", "api", "web", "css", "yourqueue.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const rules = [...css.matchAll(/([^{}]*\.yq-row[^{}]*)\{([^}]*)\}/g)].filter(m => /box-shadow/.test(m[2]));
let bad = 0;
if (!rules.length) { console.error("no .yq-row rule sets a box-shadow"); bad++; }
for (const m of rules) {
  for (const s of m[2].match(/box-shadow:[^;]*/)[0].split(/,(?![^(]*\))/)) {
    const nums = (s.match(/(?<![\w.#-])-?\d+(?:\.\d+)?px/g) || []).map(parseFloat);
    if (nums.length >= 3 && nums[2] > 10) { console.error("blur " + nums[2] + "px on " + m[1].trim()); bad++; }
  }
}
if (bad) process.exit(1);
console.log("queue shadow ok");
