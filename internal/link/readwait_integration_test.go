//go:build integration

package link

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testReadWait = 300 * time.Millisecond

func shortWait(p *Proxy) { p.readWaitOverride = testReadWait }

func TestASilentRoomsReadIsAnsweredNotHeld(t *testing.T) {
	release := make(chan struct{})
	front, _, done := pairWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}), shortWait)
	defer done()
	defer close(release)

	start := time.Now()
	res, err := http.Get(front.URL + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	took := time.Since(start)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a silent room's read got %d %q, want 503", res.StatusCode, body)
	}
	if took > 3*testReadWait {
		t.Fatalf("the read was answered after %s, want about %s", took, testReadWait)
	}
	if !strings.Contains(string(body), "did not answer in") {
		t.Fatalf("the 503 did not say the room was silent: %q", body)
	}
}

func TestAQuietEventStreamIsNotCut(t *testing.T) {
	release := make(chan struct{})
	front, _, done := pairWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Nothing at all, headers included, until something happens.
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: task\ndata: late\n\n")
	}), shortWait)
	defer done()

	// Not `/v1/events`, which the hub serves from its own merged feed. Any
	// proxied read that asks for a stream is held to the same rule.
	req, _ := http.NewRequest(http.MethodGet, front.URL+"/v1/tasks/x/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	got := make(chan *http.Response, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			got <- nil
			return
		}
		got <- res
	}()
	time.Sleep(4 * testReadWait)
	close(release)
	select {
	case res := <-got:
		if res == nil {
			return
		}
		defer res.Body.Close()
		buf := make([]byte, 256)
		n, _ := io.ReadAtLeast(res.Body, buf, len("event: task\ndata: late"))
		body := buf[:n]
		if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "late") {
			t.Fatalf("a quiet event stream was cut: %d %q", res.StatusCode, body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event stream never answered")
	}
}

func TestASlowBodyAfterPromptHeadersIsNotCut(t *testing.T) {
	front, _, done := pairWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		io.WriteString(w, "start ")
		fl.Flush()
		time.Sleep(3 * testReadWait)
		io.WriteString(w, "end")
	}), shortWait)
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks/x/files")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "start end" {
		t.Fatalf("a download that started promptly was cut: %d %q", res.StatusCode, body)
	}
}
