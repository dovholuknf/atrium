// What the reconciler actually DOES, run rather than read.
//
// `check-morph.js` beside this one checks the shape of the code. This runs it.
// The claim being tested is not "the board still draws": it is that a row
// nobody changed comes out of a repaint as THE SAME OBJECT, never written to,
// because that object is what is holding the scroll position, the text
// selection and the focus. A reconciler that rebuilds a row it could have kept
// looks identical on screen and has the original bug.
//
// There is no browser here and this repo has no npm dependencies, so the tiny
// piece of DOM the reconciler touches is implemented below and the real
// functions are lifted out of `index.html` and run against it. That is a
// narrower test than a browser, and it is the part a browser would not tell
// you anyway: whether a node survived, or was replaced by an identical one.
//
// The fixture markup is deliberately simple. The parser here understands
// tags, double-quoted attributes and text, which is all these cases need.
const fs = require("fs");

let bad = 0;
function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}
function is(got, want, what) {
  if (got !== want) fail(`${what}: expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
}

// ── the smallest DOM the reconciler needs ───────────────

class Node {
  constructor() {
    this.parentNode = null;
    this.nextSibling = null;
    this.previousSibling = null;
    this.firstChild = null;
    this.lastChild = null;
    // Every write the reconciler makes to this node, so a test can assert
    // that an unchanged row was LEFT ALONE and not merely left looking right.
    this.writes = [];
  }
  get childNodes() {
    const out = [];
    for (let n = this.firstChild; n; n = n.nextSibling) out.push(n);
    return out;
  }
  insertBefore(node, ref) {
    if (node.parentNode) node.parentNode.removeChild(node);
    node.parentNode = this;
    const prev = ref ? ref.previousSibling : this.lastChild;
    node.previousSibling = prev;
    node.nextSibling = ref || null;
    if (prev) prev.nextSibling = node; else this.firstChild = node;
    if (ref) ref.previousSibling = node; else this.lastChild = node;
    return node;
  }
  appendChild(node) { return this.insertBefore(node, null); }
  removeChild(node) {
    if (node.parentNode !== this) throw new Error("not a child");
    if (node.previousSibling) node.previousSibling.nextSibling = node.nextSibling;
    else this.firstChild = node.nextSibling;
    if (node.nextSibling) node.nextSibling.previousSibling = node.previousSibling;
    else this.lastChild = node.previousSibling;
    node.parentNode = node.nextSibling = node.previousSibling = null;
    return node;
  }
  replaceChild(fresh, stale) {
    this.insertBefore(fresh, stale);
    this.removeChild(stale);
    return stale;
  }
}

class Text extends Node {
  constructor(value) { super(); this.nodeType = 3; this.nodeName = "#text"; this._v = value; }
  get nodeValue() { return this._v; }
  set nodeValue(v) { this.writes.push("text=" + v); this._v = v; }
}

class Element extends Node {
  constructor(tag) {
    super();
    this.nodeType = 1;
    this.tagName = tag.toUpperCase();
    this.nodeName = this.tagName;
    this._attrs = new Map();
  }
  get attributes() {
    const list = [...this._attrs].map(([name, value]) => ({ name, value }));
    list.length = list.length;
    return list;
  }
  getAttribute(n) { return this._attrs.has(n) ? this._attrs.get(n) : null; }
  hasAttribute(n) { return this._attrs.has(n); }
  setAttribute(n, v) { this.writes.push("set:" + n); this._attrs.set(n, v); }
  removeAttribute(n) { this.writes.push("del:" + n); this._attrs.delete(n); }
  // Only what `morphKey` reads: data-* in camel case.
  get dataset() {
    const d = {};
    for (const [n, v] of this._attrs) {
      if (!n.startsWith("data-")) continue;
      d[n.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = v;
    }
    return d;
  }
  set innerHTML(html) {
    while (this.firstChild) this.removeChild(this.firstChild);
    parseInto(this, html);
  }
  get outerHTML() {
    const attrs = [...this._attrs].map(([n, v]) => ` ${n}="${v}"`).join("");
    let kids = "";
    for (let n = this.firstChild; n; n = n.nextSibling) {
      kids += n.nodeType === 1 ? n.outerHTML : n.nodeValue;
    }
    return `<${this.tagName.toLowerCase()}${attrs}>${kids}</${this.tagName.toLowerCase()}>`;
  }
  get innerHTML() {
    let kids = "";
    for (let n = this.firstChild; n; n = n.nextSibling) {
      kids += n.nodeType === 1 ? n.outerHTML : n.nodeValue;
    }
    return kids;
  }
}

// Tags, double-quoted attributes and text. Nothing self-closing, no voids:
// the fixtures below do not need them and a parser that guesses is worse than
// one that refuses.
function parseInto(root, html) {
  const stack = [root];
  const token = /<\/([a-zA-Z0-9]+)\s*>|<([a-zA-Z0-9]+)((?:\s+[a-zA-Z0-9-]+="[^"]*")*)\s*>/g;
  let at = 0, m;
  const text = (s) => { if (s) stack[stack.length - 1].appendChild(new Text(s)); };
  while ((m = token.exec(html))) {
    text(html.slice(at, m.index));
    at = token.lastIndex;
    if (m[1]) {
      if (stack.length < 2) throw new Error("unbalanced close </" + m[1] + ">");
      stack.pop();
    } else {
      const el = new Element(m[2]);
      const attr = /([a-zA-Z0-9-]+)="([^"]*)"/g;
      let a;
      while ((a = attr.exec(m[3]))) el.setAttribute(a[1], a[2]);
      el.writes.length = 0;
      stack[stack.length - 1].appendChild(el);
      stack.push(el);
    }
  }
  text(html.slice(at));
  if (stack.length !== 1) throw new Error("unclosed tag in fixture");
}

const document = { createElement: (tag) => new Element(tag) };

// ── the real functions, lifted out of the board ─────────

// The board's scripts, concatenated in the order the page loads them. The
// reconciler is in one of them and this does not care which: what it needs is
// the text between the two markers, wherever it now lives.
const page = require("./board-source.js").boardScript();
const from = page.indexOf("function setHTML(el, html) {");
const to = page.indexOf("// `scrollParent` lived here.");
if (from < 0 || to < 0 || to < from) {
  console.error("FAIL: could not find the reconciler in the board's script. " +
    "If it moved, move these markers with it.");
  process.exit(1);
}
const src = page.slice(from, to);
const { setHTML } = new Function("document", src +
  "\nreturn { setHTML, morphChildren, morphKey, morphNode, morphAttrs };")(document);

// ── the cases ───────────────────────────────────────────

function board(html) {
  const el = new Element("div");
  el.innerHTML = html;
  return el;
}
const card = (id, age, title) =>
  `<div class="card" data-id="${id}"><div class="title">${title}</div>` +
  `<span class="chip">running ${age}</span></div>`;

// 1. Nothing changed. Nothing is written, and the second call does not even
//    parse: this is the poll that lands every five seconds with no news.
{
  const el = board(card("a", "3s", "one") + card("b", "9s", "two"));
  const kept = el.firstChild;
  const html = card("a", "3s", "one") + card("b", "9s", "two");
  is(setHTML(el, html), false, "an identical repaint reports that nothing moved");
  is(el.firstChild, kept, "an identical repaint keeps the row object");
  is(kept.writes.length, 0, "an identical repaint writes nothing to the row");
  is(setHTML(el, html), false, "a repaint from the same markup is skipped outright");
}

// 2. THE CASE THE WHOLE THING IS FOR. One age ticked. The card survives, its
//    title is untouched, and only the chip's text is written.
{
  const el = board(card("a", "3s", "one") + card("b", "9s", "two"));
  const first = el.firstChild;
  const title = first.firstChild;
  const titleText = title.firstChild;
  const chipText = first.lastChild.firstChild;
  setHTML(el, card("a", "4s", "one") + card("b", "9s", "two"));
  is(el.firstChild, first, "the ticking card is the same element");
  is(title.firstChild, titleText, "its title is the same text node");
  is(titleText.writes.length, 0, "its title was not written to");
  is(chipText.nodeValue, "running 4s", "the age was updated");
  is(chipText.writes.length, 1, "the age was written exactly once");
  is(el.lastChild.writes.length, 0, "the card that did not tick was left alone");
}

// 3. A card arrives. Every card already there keeps its node, including the
//    ones the new one is inserted above.
{
  const el = board(card("a", "3s", "one") + card("b", "9s", "two"));
  const a = el.firstChild, b = el.lastChild;
  setHTML(el, card("c", "1s", "new") + card("a", "3s", "one") + card("b", "9s", "two"));
  is(el.childNodes.length, 3, "the new card is on the board");
  is(el.childNodes[1], a, "the card it was inserted above is the same element");
  is(el.childNodes[2], b, "and so is the one after that");
  is(a.writes.length, 0, "neither of them was written to");
}

// 4. A card leaves. It goes, and its neighbours do not.
{
  const el = board(card("a", "3s", "one") + card("b", "9s", "two") + card("c", "1s", "three"));
  const a = el.firstChild, c = el.lastChild;
  setHTML(el, card("a", "3s", "one") + card("c", "1s", "three"));
  is(el.childNodes.length, 2, "the removed card is gone");
  is(el.firstChild, a, "the card above it survived");
  is(el.lastChild, c, "the card below it survived");
  is(c.writes.length, 0, "and neither was rewritten");
}

// 5. Reordered, which is what a sort by activity does on every poll. Both
//    cards move and neither is rebuilt.
{
  const el = board(card("a", "3s", "one") + card("b", "9s", "two"));
  const a = el.firstChild, b = el.lastChild;
  setHTML(el, card("b", "9s", "two") + card("a", "3s", "one"));
  is(el.firstChild, b, "the reordered card moved rather than being rebuilt");
  is(el.lastChild, a, "and so did the other one");
  is(a.writes.length + b.writes.length, 0, "neither was written to");
}

// 6. A card moves between columns. Keys are matched WITHIN A PARENT, so this
//    one card is rebuilt under its new column, which is correct: it is the
//    change. What matters is that the two COLUMNS are keyed as well, so
//    neither of them is rebuilt around it and the scroll in the one you are
//    reading does not move because something landed in the other.
{
  const el = board(
    `<section data-column="ready">${card("a", "3s", "one")}</section>` +
    `<section data-column="running"></section>`);
  const ready = el.firstChild, running = el.lastChild;
  setHTML(el,
    `<section data-column="ready"></section>` +
    `<section data-column="running">${card("a", "4s", "one")}</section>`);
  is(el.firstChild, ready, "the column it left is the same element");
  is(el.lastChild, running, "so is the column it arrived in");
  is(running.firstChild.getAttribute("data-id"), "a", "the card arrived");
  is(ready.firstChild, null, "and left");
}

// 7. Structure changed: a `why` line appears under a card. The card is still
//    the same element, because the container is what holds the scroll.
{
  const el = board(card("a", "3s", "one"));
  const a = el.firstChild;
  setHTML(el, `<div class="card" data-id="a"><div class="title">one</div>` +
    `<span class="chip">running 3s</span><div class="why">blocked</div></div>`);
  is(el.firstChild, a, "the card kept its element when its structure changed");
  is(a.lastChild.innerHTML, "blocked", "and gained the new line");
}

// 8. The status class moves. It is an attribute write and nothing else: the
//    text under it is not touched, so a selection over the title lives.
{
  const el = board(card("a", "3s", "one"));
  const a = el.firstChild;
  const titleText = a.firstChild.firstChild;
  setHTML(el, `<div class="card waiting" data-id="a"><div class="title">one</div>` +
    `<span class="chip">running 3s</span></div>`);
  is(a.getAttribute("class"), "card waiting", "the status class was applied");
  is(a.writes.join(","), "set:class", "only the class was written");
  is(titleText.writes.length, 0, "the text under it was not");
}

// 9. An attribute that went is removed rather than left behind. A card that
//    stops being pinned and keeps `data-pinned` is a card that lies.
{
  const el = board(`<div class="card" data-id="a" data-pinned="1"><b>one</b></div>`);
  setHTML(el, `<div class="card" data-id="a"><b>one</b></div>`);
  is(el.firstChild.hasAttribute("data-pinned"), false, "the dropped attribute is gone");
}

// 10. Unkeyed markup still reconciles, by position and shape. Most of the
//     board's smaller lists have no id to key on and they get tier two anyway.
{
  const el = board(`<div class="row">one</div><div class="row">two</div>`);
  const first = el.firstChild, second = el.lastChild;
  setHTML(el, `<div class="row">one</div><div class="row">three</div>`);
  is(el.firstChild, first, "the unchanged unkeyed row kept its element");
  is(first.writes.length + first.firstChild.writes.length, 0, "and was not written to");
  is(second.firstChild.nodeValue, "three", "the changed one was updated in place");
}

// 11. An unkeyed node is never matched against a keyed one. A separator drawn
//     between the pinned rows and the rest moves as the pinned count changes,
//     and matching it to a card would rewrite a card into a separator.
{
  const el = board(card("a", "3s", "one") + `<div class="pinbreak">the rest</div>` +
    card("b", "9s", "two"));
  const a = el.firstChild, b = el.lastChild;
  setHTML(el, card("a", "3s", "one") + card("b", "9s", "two") +
    `<div class="pinbreak">the rest</div>`);
  is(el.firstChild, a, "the first card is still the first card");
  is(el.childNodes[1], b, "the second one moved up rather than being overwritten");
  is(el.lastChild.getAttribute("class"), "pinbreak", "the separator moved to the end");
}

// 12. `data-morph-keep` means a subtree the board handed to something else.
//     Its attributes sync, its children are not the board's to reconcile.
{
  const el = board(`<div data-id="t" data-morph-keep="" class="on"><b>live</b></div>`);
  const inner = el.firstChild.firstChild;
  setHTML(el, `<div data-id="t" data-morph-keep="" class="off"><i>replaced</i></div>`);
  is(el.firstChild.getAttribute("class"), "off", "its attributes still sync");
  is(el.firstChild.firstChild, inner, "its children were left alone");
}

// 13. Emptying and refilling. The cache is keyed on the markup, so a list that
//     goes empty and comes back is not skipped by the fast path.
{
  const el = board(card("a", "3s", "one"));
  is(setHTML(el, ""), true, "clearing reports a change");
  is(el.childNodes.length, 0, "and clears");
  is(setHTML(el, card("a", "3s", "one")), true, "refilling reports a change");
  is(el.childNodes.length, 1, "and refills");
}

// 14. The fast path does not fire on an element somebody else cleared behind
//     it. The markup would match the cache and the list would stay empty.
{
  const el = board(card("a", "3s", "one"));
  const html = card("a", "3s", "one");
  setHTML(el, html);
  el.innerHTML = "";
  is(setHTML(el, html), true, "a cleared element is repainted rather than skipped");
  is(el.childNodes.length, 1, "and comes back");
}

// 15. A key comes back on a DIFFERENT KIND of node, which is what a group
//     drawn as a flat list one poll and as a collapsible the next looks like.
//     The one that goes must not take the reconciler's place in the list with
//     it, or everything after it lands in the wrong order and the nodes it was
//     supposed to replace are never removed.
{
  const el = board(`<div data-id="g">one</div>` + card("a", "3s", "one") +
    card("b", "9s", "two"));
  const b = el.lastChild;
  setHTML(el, `<section data-id="g">one</section>` + card("a", "3s", "one") +
    card("b", "9s", "two"));
  is(el.childNodes.length, 3, "nothing was left behind or duplicated");
  is(el.firstChild.nodeName, "SECTION", "the replaced node is the new kind");
  is(el.childNodes[1].getAttribute("data-id"), "a", "the order held");
  is(el.lastChild, b, "and the rows after it were not rebuilt");
}

// 16. The same, one position in, so the replacement is not the node the
//     reconciler happens to be standing on.
{
  const el = board(card("a", "3s", "one") + `<div data-id="g">one</div>` +
    card("b", "9s", "two"));
  const a = el.firstChild, b = el.lastChild;
  setHTML(el, card("a", "3s", "one") + `<section data-id="g">one</section>` +
    card("b", "9s", "two"));
  is(el.childNodes.length, 3, "nothing was left behind or duplicated");
  is(el.firstChild, a, "the row above kept its element");
  is(el.childNodes[1].nodeName, "SECTION", "the replaced node is the new kind");
  is(el.lastChild, b, "and the row below kept its element");
}

// 17. And the same again with a row LEAVING after it, which is the shape that
//     actually catches a stranded cursor: everything past the replacement is
//     appended to the end, so the row that should have gone is never reached
//     and stays on the board looking current.
{
  const el = board(`<div data-id="g">one</div>` + card("a", "3s", "one") +
    card("x", "1s", "gone"));
  setHTML(el, `<section data-id="g">one</section>` + card("a", "3s", "one"));
  is(el.childNodes.length, 2, "the removed row is gone rather than stranded");
  is(el.firstChild.nodeName, "SECTION", "the replaced node is the new kind");
  is(el.lastChild.getAttribute("data-id"), "a", "and the survivor is in place");
}

if (bad) {
  console.error(`${bad} reconciler behaviour check(s) failed.`);
  process.exit(1);
}
console.log("the reconciler keeps what did not change.");
