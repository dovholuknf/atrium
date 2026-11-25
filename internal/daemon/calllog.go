package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/dovholuknf/atrium/internal/base/calllog"
)

// logged writes a start line before the agent listener's handler runs and an end line after, for every hook and
// agent call in one place.
func logged(l *calllog.Log, room func() string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller := r.URL.Query().Get("agent")
		if caller == "" {
			caller = r.Header.Get("X-Atrium-Agent")
		}
		if caller == "" && r.Method == http.MethodPost && r.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
			var a struct {
				Agent string `json:"agent"`
			}
			if json.Unmarshal(raw, &a) == nil {
				caller = a.Agent
			}
		}
		end := l.Begin("hook", r.Method+" "+r.URL.Path, caller, room())
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				end(calllog.Error, fmt.Errorf("panic: %v", p))
				panic(p)
			}
			switch {
			case sw.status >= 500:
				end(calllog.Error, fmt.Errorf("status %d", sw.status))
			case r.Context().Err() != nil:
				end(calllog.Outcome(r.Context(), r.Context().Err()), nil)
			default:
				end(calllog.OK, nil)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

// statusWriter remembers the status and still lets an upgrade or a flush through.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	return h.Hijack()
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
