package link

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

// uploadTimer times one board upload through the hub, one line per paste. The id is the browser's, and the room logs
// the same one, so the two lines line up. Steps are offsets from the hub first seeing the request.
type uploadTimer struct {
	t0                             time.Time
	mu                             sync.Mutex
	route, conn, first, eof, wrote time.Duration
	reply                          time.Duration
	bytes                          int64
}

func isUpload(r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, "/v1/tasks/")
	if !ok || r.Method != http.MethodPost {
		return false
	}
	_, tail, _ := strings.Cut(rest, "/")
	return tail == "files"
}

// timeUpload wraps one forwarded upload. The returned function logs the line once the reply is done.
func timeUpload(w http.ResponseWriter, r *http.Request, t0 time.Time) (http.ResponseWriter, *http.Request, func(room string)) {
	u := &uploadTimer{t0: t0, route: time.Since(t0)}
	at := func(d *time.Duration) {
		u.mu.Lock()
		if *d == 0 {
			*d = time.Since(t0)
		}
		u.mu.Unlock()
	}
	ctx := httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{
		GotConn:              func(httptrace.GotConnInfo) { at(&u.conn) },
		WroteRequest:         func(httptrace.WroteRequestInfo) { at(&u.wrote) },
		GotFirstResponseByte: func() { at(&u.reply) },
	})
	r = r.WithContext(ctx)
	r.Body = &countedBody{ReadCloser: r.Body, u: u, first: at}
	id := r.Header.Get("X-Atrium-Upload")
	return &timedWriter{ResponseWriter: w, u: u}, r, func(room string) {
		u.mu.Lock()
		defer u.mu.Unlock()
		ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
		log.Printf("[hub upload] id=%s room=%s bytes=%d route=%.0fms conn=%.0fms body-first=%.0fms body-done=%.0fms "+
			"wrote=%.0fms room-reply=%.0fms total=%.0fms", logLine(id), logLine(room), u.bytes, ms(u.route), ms(u.conn),
			ms(u.first), ms(u.eof), ms(u.wrote), ms(u.reply), ms(time.Since(t0)))
	}
}

type countedBody struct {
	io.ReadCloser
	u     *uploadTimer
	first func(*time.Duration)
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.u.mu.Lock()
	b.u.bytes += int64(n)
	b.u.mu.Unlock()
	if n > 0 {
		b.first(&b.u.first)
	}
	if err == io.EOF {
		b.first(&b.u.eof)
	}
	return n, err
}

// timedWriter adds the hub's steps to the reply as Server-Timing, which the board reads for the same upload.
type timedWriter struct {
	http.ResponseWriter
	u *uploadTimer
}

func (t *timedWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

func (t *timedWriter) WriteHeader(code int) {
	t.u.mu.Lock()
	route, conn := t.u.route, t.u.conn
	body := t.u.eof - t.u.first
	sent := max(t.u.wrote-t.u.eof, 0)
	wait := max(time.Since(t.u.t0)-t.u.wrote, 0)
	t.u.mu.Unlock()
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	t.Header().Add("Server-Timing", fmt.Sprintf("hub-route;dur=%.1f, hub-conn;dur=%.1f, hub-body;dur=%.1f, hub-sent;dur=%.1f, hub-room;dur=%.1f",
		ms(route), ms(conn-route), ms(body), ms(sent), ms(wait)))
	t.ResponseWriter.WriteHeader(code)
}
