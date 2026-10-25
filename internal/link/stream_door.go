package link

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
)

// THE HUB MACHINE'S OPERATOR is the one door for the caller `hub`: POST /_hub/ssh/<room> with `Connection: Upgrade` and
// `Upgrade: atrium-stream`, on the hub's loopback board listener. See docs/rnd/ssh-via-hub.md decisions 6 and 7.
//
// THE BOARD CAN NEVER OPEN A STREAM, for three separate reasons that each hold alone. A browser cannot set `Connection`
// or `Upgrade` on fetch or XHR, and a WebSocket says `websocket`. `Origin` and `Sec-Fetch-Site` are refused, so a page
// open on the hub machine cannot reach it either. And a share or overlay listener never passes `edge.LocalOperator`,
// so it answers 404 whatever the share's login said.

// serveSSH answers /_hub/ssh/<room>.
func (p *Proxy) serveSSH(w http.ResponseWriter, r *http.Request, room string) {
	if !edge.LocalOperator(r) {
		// 404, not 403: off the hub machine this route does not exist.
		http.NotFound(w, r)
		return
	}
	refuse := func(status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":%q}`, msg)
	}
	if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		refuse(http.StatusForbidden, "a page cannot open an ssh stream")
		return
	}
	if r.Method != http.MethodPost || !headerHas(r.Header, "Connection", "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), StreamUpgrade) {
		refuse(http.StatusBadRequest, "that is not a stream request")
		return
	}
	name, err := url.PathUnescape(room)
	if err != nil || strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
		refuse(http.StatusBadRequest, "name the room to reach")
		return
	}
	st, err := p.hub.OpenStream(r.Context(), name, "hub", CleanClaim(r.Header.Get("X-Atrium-Claim")))
	if err != nil {
		status := http.StatusBadGateway
		if se, ok := err.(*StreamError); ok {
			status = se.Status
		}
		refuse(status, err.Error())
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		st.Abort()
		refuse(http.StatusInternalServerError, "this connection cannot be taken over")
		return
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		st.Abort()
		return
	}
	_ = c.SetDeadline(time.Time{})
	if _, err := fmt.Fprint(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+StreamUpgrade+"\r\n\r\n"); err != nil {
		c.Close()
		st.Abort()
		return
	}
	// Bytes the server already read are the first of ssh's, and go onward first.
	st.Run(&buffered{Conn: c, br: rw.Reader}, nil)
}
