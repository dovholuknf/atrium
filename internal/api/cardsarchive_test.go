package api

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormCwd(t *testing.T) {
	for in, want := range map[string]string{
		`C:\wt\a\`:    "C:/wt/a",
		"C:/wt//a/":   "C:/wt/a",
		"/home/x/wt/": "/home/x/wt",
		"/":           "/",
		"  /a/b  ":    "/a/b",
		"":            "",
		`\\?\C:\wt\a`: "C:/wt/a",
	} {
		if got := normCwd(in); got != want {
			t.Errorf("normCwd(%q) = %q, want %q", in, got, want)
		}
	}
	if !samePathFold("/a/b/", "/a/b") || samePathFold("/a/b", "/a/c") || samePathFold("", "") {
		t.Error("samePathFold")
	}
}

func TestRepliesAnswerGzippedOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	v := map[string]any{"text": strings.Repeat("a reply ", 500)}

	plain := httptest.NewRecorder()
	writeJSONGzip(plain, httptest.NewRequest("GET", "/", nil), http.StatusOK, v)
	if plain.Header().Get("Content-Encoding") != "" {
		t.Fatal("compressed for a caller that did not ask")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	zipped := httptest.NewRecorder()
	writeJSONGzip(zipped, req, http.StatusOK, v)
	if zipped.Header().Get("Content-Encoding") != "gzip" || zipped.Body.Len() >= plain.Body.Len()/4 {
		t.Fatalf("encoding %q, %d bytes against %d", zipped.Header().Get("Content-Encoding"), zipped.Body.Len(), plain.Body.Len())
	}
	zr, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.NewDecoder(zr).Decode(&got); err != nil || got["text"] != v["text"] {
		t.Fatalf("round trip: %v %v", err, got)
	}
}
