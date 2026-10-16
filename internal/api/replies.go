package api

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// cardReplies is a card's last few replies as text, for the phone's
// conversation page at /m (r-024, M2 in docs/backlog/ui/mobile-design.md).
// `?n=` asks for how many, 3 by default and at most 50. `?before=` is an RFC3339
// time and asks for the page strictly older than it. The daemon reads them
// from the transcript, or from the screen for a card with no readable one.
func (s *Server) cardReplies(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Get(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, NotOnRoom(id, s.Room))
			return
		}
		s.fail(w, err)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	var before time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		var perr error
		if before, perr = time.Parse(time.RFC3339Nano, b); perr != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("before must be an RFC3339 time: %w", perr))
			return
		}
	}
	var v any
	var err error
	if r.URL.Query().Get("all") == "1" && s.RepliesAll != nil {
		v, err = s.RepliesAll(id)
	} else {
		v, err = s.Replies(id, n, before)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, NotOnRoom(id, s.Room))
			return
		}
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSONGzip(w, r, http.StatusOK, v)
}

// writeJSONGzip is writeJSON, compressed when the caller accepts it. A page of replies is 60 KB of markdown that
// shrinks to a tenth, and the phone reads one on open and again at every turn end, over whatever link reaches it.
func writeJSONGzip(w http.ResponseWriter, r *http.Request, code int, v any) {
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		writeJSON(w, code, v)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	w.WriteHeader(code)
	zw := gzip.NewWriter(w)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		log.Printf("[atrium api] encode: %v", err)
	}
	_ = zw.Close()
}
