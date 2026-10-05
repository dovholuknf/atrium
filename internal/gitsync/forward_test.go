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

// toServer sends every request to srv, whatever host it names, as the link's transport does for "hub".
type toServer struct{ srv *httptest.Server }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Host = strings.TrimPrefix(s.srv.URL, "http://")
	return http.DefaultTransport.RoundTrip(r)
}

func TestTheHubLoopbackServesAFetchOfOneRepositoryAndNothingElse(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	url, stop, err := HubLoopback(toServer{srv}, "github/o/r")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	base := strings.TrimSuffix(url, "/git/hub/github/o/r.git")
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/git/hub/github/o/r.git/info/refs?service=git-upload-pack", 200},
		{"POST", "/git/hub/github/o/r.git/git-upload-pack", 200},
		{"GET", "/git/hub/github/o/r.git/info/refs?service=git-receive-pack", 403},
		{"POST", "/git/hub/github/o/r.git/git-receive-pack", 403},
		{"GET", "/git/hub/github/o/r.git/git-upload-pack", 403},
		{"POST", "/git/hub/github/o/r.git/info/refs?service=git-upload-pack", 403},
		{"GET", "/git/hub/github/o/other.git/info/refs?service=git-upload-pack", 403},
		{"GET", "/git/hub/github/o/r.git/HEAD", 403},
		{"GET", "/_forge/pr", 403},
	} {
		req, _ := http.NewRequest(c.method, base+c.path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, res.StatusCode, c.want)
		}
	}
	want := []string{"GET /git/hub/github/o/r.git/info/refs", "POST /git/hub/github/o/r.git/git-upload-pack"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("the hub saw %q, want %q", seen, want)
	}
	if _, _, err := HubLoopback(toServer{srv}, "../x"); err == nil {
		t.Fatal("a bad store name was taken")
	}
}
