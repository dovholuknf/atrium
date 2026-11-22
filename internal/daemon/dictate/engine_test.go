package dictate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAskSendsPromptAndReadsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference" || r.FormValue("prompt") != "atrium, sha" || r.FormValue("temperature") != "0" {
			http.Error(w, `{"error":"bad request"}`, 400)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, `{"error":"no file"}`, 400)
			return
		}
		_ = f.Close()
		_, _ = w.Write([]byte(`{"text":" merge claude/main\n"}`))
	}))
	defer srv.Close()
	got, err := ask(context.Background(), srv.URL, []byte("RIFF....WAVE"), "atrium, sha")
	if err != nil || got != " merge claude/main\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestUnconfiguredEngineIsOff(t *testing.T) {
	e := &Engine{Conf: func() (string, string) { return "", "" }}
	if _, err := e.Transcribe(context.Background(), nil, ""); !errors.Is(err, ErrOff) {
		t.Fatalf("got %v, want ErrOff", err)
	}
}

func TestURLWinsAndStartsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"shared"}`))
	}))
	defer srv.Close()
	e := &Engine{
		Conf: func() (string, string) { return "no-such-binary", "no-such-model" },
		URL:  func() string { return srv.URL + "/inference/" },
	}
	got, err := e.Transcribe(context.Background(), []byte("RIFF....WAVE"), "")
	if err != nil || got != "shared" || e.cmd != nil {
		t.Fatalf("got %q, %v, cmd %v", got, err, e.cmd)
	}
}

func TestCheckURL(t *testing.T) {
	for _, c := range []struct {
		url     string
		confirm bool
		ok      bool
	}{
		{"https://gpu.corp:8080", false, true},
		{"http://127.0.0.1:8080", false, true},
		{"http://localhost:8080", false, true},
		{"http://gpu.corp:8080", false, false},
		{"http://gpu.corp:8080", true, true},
		{"ftp://gpu.corp", true, false},
		{"gpu.corp:8080", true, false},
	} {
		if err := CheckURL(c.url, c.confirm); (err == nil) != c.ok {
			t.Errorf("CheckURL(%q, %v) = %v", c.url, c.confirm, err)
		}
	}
}
