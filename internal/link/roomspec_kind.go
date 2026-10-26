package link

import (
	"bufio"
	"log"
	"net"
	"net/http"
	"time"
)

// serveRoomSpec answers HTTP on one connection a room dialled, for the room the certificate named.
func (h *Hub) serveRoomSpec(name string, conn net.Conn, br *bufio.Reader) {
	if h.RoomSpec == nil {
		_ = writeJSON(conn, welcome{OK: false, Error: "this hub keeps no room specs. update the hub"})
		return
	}
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	one := newOneConn(&buffered{Conn: conn, br: br})
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// SET, NOT ADDED TO: whatever the room sent in this header is gone.
			r.Header.Set(RoomSpecRoomHeader, name)
			h.RoomSpec.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: gitHeaderWait,
		ReadTimeout:       gitHeaderWait,
		IdleTimeout:       time.Second,
		ConnState: func(_ net.Conn, s http.ConnState) {
			if s == http.StateClosed || s == http.StateHijacked {
				one.finish()
			}
		},
		ErrorLog: log.New(discard{}, "", 0),
	}
	_ = srv.Serve(one)
}
