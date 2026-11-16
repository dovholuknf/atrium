package link

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// pageOf reads `/room/<room>/<card>/files/<rest>`.
func pageOf(p string) (room, card, rest string, ok bool) {
	parts := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 5)
	if len(parts) < 4 || parts[0] != "room" || parts[1] == "" || parts[2] == "" || parts[3] != "files" {
		return "", "", "", false
	}
	if len(parts) == 5 {
		rest = parts[4]
	}
	return parts[1], parts[2], rest, true
}

// servePage hands a card's HTML file and its relative assets to the room as a sandboxed page, on whatever host the
// human reached the hub on. The room confines the path to the card and sets the sandbox.
func (p *Proxy) servePage(w http.ResponseWriter, r *http.Request) bool {
	room, card, rest, ok := pageOf(r.URL.EscapedPath())
	if !ok || p.hub == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	if !p.hub.Has(room) {
		writeErrBody(w, http.StatusServiceUnavailable, "room "+room+" is not attached to this hub")
		return true
	}
	rel, err := url.PathUnescape(rest)
	if err != nil {
		writeErrBody(w, http.StatusBadRequest, "that path is not valid")
		return true
	}
	out := r.Clone(context.WithValue(r.Context(), roomKey{}, room))
	out.URL.Path = "/v1/tasks/" + card + "/page/" + rel
	out.URL.RawPath = ""
	out.Header.Del("Cookie")
	out.Header.Del("Authorization")
	// The room names it too: internal/api/page.go PagePrefixHeader.
	out.Header.Set("X-Atrium-Page-Prefix", "/room/"+room+"/"+card+"/files/")
	p.proxy.ServeHTTP(w, out)
	return true
}
