package api

import (
	"html"
	"net/http"
	"strconv"
	"strings"
)

// pageMarkdown draws a card's .md file as the board's own markdown view does, from the board's marked and purify. The
// source rides in an inert text block, and raw HTML in it is shown as text, as read.js does.
func pageMarkdown(w http.ResponseWriter, src []byte, prefix, name string) {
	b := prefix + pageBoardDir
	text := strings.ReplaceAll(string(src), "</", `<\/`)
	out := `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1"><title>` + html.EscapeString(name) + `</title>` +
		`<link rel="stylesheet" href="` + b + `css/tokens.css"><link rel="stylesheet" href="` + b + `css/themes.css">` +
		pageMarkdownStyle + `</head><body><header><code>` + html.EscapeString(name) +
		`</code><a class="raw" href="?raw=1">Raw</a></header><article id="md" class="md"></article>` +
		`<script id="src" type="text/plain">` + text + `</script>` +
		`<script src="` + b + `vendor/marked.js"></script><script src="` + b + `vendor/purify.js"></script>` +
		pageMarkdownScript + `</body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.Write([]byte(out))
}

const pageMarkdownStyle = `<style>
html, body { height: auto; overflow: visible; }
html { overflow-y: scroll; }
body { display: block; min-height: 100%; margin: 0; background: var(--bg-1); color: var(--body); font: 15px/1.5 var(--sans); }
header { display: flex; align-items: center; gap: 12px; padding: 12px 18px; border-bottom: 1px solid var(--stroke-dim);
  background: var(--shell-0); position: sticky; top: 0; z-index: 1; }
header code { font: var(--fs-sm)/1.4 var(--mono); color: var(--head); }
header .raw { margin-left: auto; color: var(--label); font-size: var(--fs-sm); }
.md { margin: 0; padding: 18px 22px 60px; color: var(--body); }
.md > * { max-width: 52rem; margin-left: auto; margin-right: auto; }
.md table { display: block; width: max-content; max-width: 100%; overflow-x: auto; border-collapse: collapse; }
.md h1, .md h2, .md h3, .md h4 { color: var(--head); line-height: 1.25; margin-top: 1.4em; margin-bottom: .5em; }
.md h1, .md h2 { border-bottom: 1px solid var(--stroke-dim); padding-bottom: .25em; }
.md a { color: var(--head); text-decoration: underline; }
.md code { font: .9em/1.4 var(--mono); background: var(--shell-0); padding: .1em .35em; border-radius: 4px; }
.md pre { background: var(--shell-0); padding: 12px 14px; border-radius: 6px; overflow: auto; white-space: pre; }
.md pre code { background: none; padding: 0; }
.md blockquote { margin-top: 1em; margin-bottom: 1em; padding: 0 1em; border-left: 3px solid var(--stroke-dim); color: var(--dim); }
.md th, .md td { border: 1px solid var(--stroke-dim); padding: 4px 10px; }
.md img { max-width: 100%; }
</style>`

const pageMarkdownScript = `<script>
(function () {
  var r = new marked.Renderer();
  r.html = function (h) {
    var s = typeof h === "string" ? h : (h && (h.text || h.raw)) || "";
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  };
  var dirty = marked.parse(document.getElementById("src").textContent, { gfm: true, breaks: false, renderer: r, async: false });
  document.getElementById("md").innerHTML = DOMPurify.sanitize(dirty, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ["style", "form", "input", "button", "textarea", "select", "iframe", "object", "embed", "svg", "math", "link", "meta", "base"],
    FORBID_ATTR: ["style", "srcset", "formaction"], ALLOW_DATA_ATTR: false
  });
})();
</script>`
