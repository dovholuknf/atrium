// Package cardurl is which page a readable card address is: stage R3 of
// docs/rnd/card-urls-design.md.
//
// `/alias/<alias>`, `/room/<room>` and `/room/<room>/<name>` are the board page,
// and `/m/alias/<alias>` and `/m/room/<room>/<name>` the phone page. The server
// serves the same static page for any name and never resolves one: the page
// asks `GET /v1/tasks/<name>` itself, so there is one resolver and one place
// that says "no such card" well.
//
// ONE FUNCTION FOR BOTH SERVERS, the hub's `asset` and the room's `webHandler`,
// so the two cannot disagree on a shape.
package cardurl

import (
	"html"
	"net/http"
	"strings"
)

// Page reports whether a path is a card address. When it is, page is the board
// file to serve, or empty when the shape is wrong, which is a 404 naming the
// shapes that work.
func Page(urlPath string) (page string, isCard bool) {
	phone := false
	p := urlPath
	if strings.HasPrefix(p, "/m/") {
		phone, p = true, p[len("/m"):]
	}
	var parts []string
	switch {
	case strings.HasPrefix(p, "/alias/") || p == "/alias":
		parts = strings.Split(strings.TrimPrefix(p, "/alias"), "/")[1:]
		if len(parts) != 1 || parts[0] == "" {
			return "", true
		}
	case strings.HasPrefix(p, "/room/") || p == "/room":
		parts = strings.Split(strings.TrimPrefix(p, "/room"), "/")[1:]
		// The phone has no scoped board, so it needs the card.
		want := len(parts) == 2 || (len(parts) == 1 && !phone)
		if !want {
			return "", true
		}
		for _, s := range parts {
			if s == "" {
				return "", true
			}
		}
	default:
		return "", false
	}
	if phone {
		return "m/index.html", true
	}
	return "index.html", true
}

// NotFound is the page for a card address of the wrong shape.
func NotFound(w http.ResponseWriter, urlPath string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>not a card address</title>` +
		`<body style="font-family:system-ui;margin:2em"><p><code>` + html.EscapeString(urlPath) +
		`</code> is not a card address. These are:</p><ul>` +
		`<li><code>/alias/&lt;alias&gt;</code></li>` +
		`<li><code>/room/&lt;room&gt;/&lt;alias or handle&gt;</code></li>` +
		`<li><code>/room/&lt;room&gt;</code>, the board for that room</li>` +
		`<li><code>/m/alias/&lt;alias&gt;</code> and <code>/m/room/&lt;room&gt;/&lt;name&gt;</code> on the phone</li>` +
		`</ul><p><a href="/">the board</a></p></body>`))
}
