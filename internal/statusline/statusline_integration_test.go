//go:build integration

package statusline

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These run a real listener and a real post, which a unit test may not do.

func TestRenderPostsToTheRoomOncePerTwelveSeconds(t *testing.T) {
	got := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r.Method + " " + r.URL.Path + " " + r.Header.Get("Content-Type") + " " + string(b)
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("ATRIUM_HUB_URL", srv.URL)
	t.Setenv("ATRIUM_TASK_ID", "card-1")
	spawn := func(url string, body []byte) { Post(url, string(body)) }
	now := time.Now()

	t.Setenv("ATRIUM_PERM_GATE", "off")
	Render(payloadFor("sess-off"), home, now, spawn)
	if _, err := os.Stat(filepath.Join(home, ".claude", "usage-log.state", "sess-off.post")); err == nil {
		t.Fatal("ATRIUM_PERM_GATE=off still posted")
	}

	t.Setenv("ATRIUM_PERM_GATE", "on")
	line := Render(payloadFor("sess-1"), home, now, spawn)
	if !strings.Contains(line, " wk ") || !strings.Contains(line, "/x") {
		t.Fatalf("the line lost its segments: %q", line)
	}
	select {
	case g := <-got:
		if want := "POST /telemetry application/json " + wantBody; g != want {
			t.Fatalf("post\n got  %s\n want %s", g, want)
		}
	default:
		t.Fatal("no telemetry post reached the room")
	}

	Render(payloadFor("sess-1"), home, now.Add(5*time.Second), spawn)
	select {
	case g := <-got:
		t.Fatalf("posted again inside 12 seconds: %s", g)
	default:
	}
}

func TestADeadRoomDoesNotBlockTheDraw(t *testing.T) {
	// A hub that accepts and never answers, so the post hangs until its own one second cap.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	accepted := make(chan struct{}, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
			select {
			case accepted <- struct{}{}:
			default:
			}
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	})
	t.Setenv("ATRIUM_HUB_URL", "http://"+ln.Addr().String())
	t.Setenv("ATRIUM_PERM_GATE", "on")

	// spawn stands in for the detached copy: the real post, off Render's path.
	postDone := make(chan struct{})
	spawn := func(url string, body []byte) {
		go func() {
			Post(url, string(body))
			close(postDone)
		}()
	}
	Render(payloadFor("sess-dead"), t.TempDir(), time.Now(), spawn)
	// Render returned while the hung post was still running: the draw does not wait for the hub.
	select {
	case <-postDone:
		t.Fatal("the post finished before Render returned, so the hub did not hang it")
	default:
	}
	select {
	case <-accepted:
	case <-time.After(10 * time.Second): // wait: the post dials a local listener, the only wait here
		t.Fatal("the post never reached the hung hub")
	}
	select {
	case <-postDone:
	case <-time.After(10 * time.Second): // wait: the post's own one second cap must end it
		t.Fatal("the post did not end at its cap")
	}
}
