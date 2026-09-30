package gitsync

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// serve stands a Backend behind a forwarder over a plain transport, which is what the
// link's transport replaces in the daemon.
func forwarded(t *testing.T, s *served) *Forwarder {
	t.Helper()
	f, err := NewForwarder(http.DefaultTransport, strings.TrimPrefix(s.srv.URL, "http://"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

func TestTheForwarderCarriesAFetch(t *testing.T) {
	s := newServed(t)
	f := forwarded(t, s)
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	git(t, dst, "fetch", "-q", "--no-tags", f.URL+"/github/o/r.git", "+refs/heads/claude/main:refs/remotes/hub/claude/main")
	if got := git(t, dst, "rev-parse", "refs/remotes/hub/claude/main"); got != s.mainSHA {
		t.Fatalf("got %s", got)
	}
}

func TestTheForwarderRefusesARequestWithoutItsToken(t *testing.T) {
	s := newServed(t)
	f := forwarded(t, s)
	base := strings.TrimSuffix(f.URL, f.URL[strings.LastIndex(f.URL, "/"):])
	for _, u := range []string{
		base + "/github/o/r.git/info/refs?service=git-upload-pack",
		base + "/deadbeef/github/o/r.git/info/refs?service=git-upload-pack",
		f.URL + "x/github/o/r.git/info/refs?service=git-upload-pack",
		f.URL,
	} {
		code, _ := get(t, u)
		if code != 404 {
			t.Errorf("GET %s = %d, want 404", u, code)
		}
	}
	if code, _ := get(t, f.URL+"/github/o/r.git/info/refs?service=git-upload-pack"); code != 200 {
		t.Fatalf("with the token = %d", code)
	}
}

func TestTheForwarderClosesWhenTheCommandExits(t *testing.T) {
	s := newServed(t)
	f := forwarded(t, s)
	addr := strings.TrimPrefix(strings.SplitN(f.URL, "/", 4)[2], "")
	f.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatal("the forwarder is still listening after Close")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A fetch of many distinct tips, so the want list is far past git's post buffer floor and
// the client sends the POST chunked. It has to survive forwarder, transport and cgi.
func TestAChunkedPostGoesThrough(t *testing.T) {
	s := freshServed(t)
	var stream strings.Builder
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&stream, "commit refs/heads/b%d\ncommitter t <t@t> %d +0000\ndata 1\nx\n\n", i, 1000+i)
	}
	cmd := exec.Command("git", "-C", s.bare, "fast-import", "--quiet")
	cmd.Env = CleanEnv()
	cmd.Stdin = strings.NewReader(stream.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v %s", err, out)
	}
	f := forwarded(t, s)
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	git(t, dst, "-c", "http.postBuffer=1024", "fetch", "-q", "--no-tags",
		f.URL+"/github/o/r.git", "+refs/heads/*:refs/remotes/hub/*")
	if n := strings.Count(git(t, dst, "for-each-ref", "refs/remotes/hub"), "\n"); n < 1500 {
		t.Fatalf("only %d refs arrived", n)
	}
}

var _ = httptest.NewServer
