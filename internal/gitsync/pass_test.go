package gitsync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// passRooms is a hub's view of its rooms with a transport that answers from a function, so a pass-through is
// tested without a link or a git.
type passRooms struct {
	rooms []RoomInfo
	rt    http.RoundTripper
}

func (f passRooms) Attached() []RoomInfo               { return f.rooms }
func (f passRooms) Transport(string) http.RoundTripper { return f.rt }

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func passFor(rooms []RoomInfo, rt http.RoundTripper) *Pass {
	h := &Hub{Dir: "", Rooms: passRooms{rooms, rt}}
	return h.PassHandler()
}

func ask(p *Pass, method, path, body, reader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://hub"+path, strings.NewReader(body))
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	}
	req = req.WithContext(WithReader(req.Context(), Reader{Reach: "loopback", Key: reader}))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

const refsPath = "/git/room/sg3/github/o/r.git/info/refs?service=git-upload-pack"

func okRT(body string) http.RoundTripper {
	return rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"x"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}

func TestAtMostTwoPassThroughsRunAtOncePerRoom(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 8)
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-gate
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	p := passFor([]RoomInfo{{Name: "sg3", Git: true}, {Name: "m1mini", Git: true}}, rt)
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	run := func(path, reader string) {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- ask(p, "GET", path, "", reader).Code }()
	}
	run(refsPath, "a")
	run(refsPath, "b")
	<-started
	<-started
	// A third, from a third reader, is told to wait, and never reaches the room.
	if rec := ask(p, "GET", refsPath, "", "c"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third = %d %s", rec.Code, rec.Body)
	}
	// Another room has its own two.
	run("/git/room/m1mini/github/o/r.git/info/refs?service=git-upload-pack", "d")
	<-started
	close(gate)
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Errorf("a running pass-through answered %d", c)
		}
	}
	// Both done: a slot is free again.
	if rec := ask(p, "GET", refsPath, "", "e"); rec.Code != 200 {
		t.Fatalf("after they finished = %d", rec.Code)
	}
}

func TestTheRateIsPerReaderAndPerMinute(t *testing.T) {
	p := passFor([]RoomInfo{{Name: "sg3", Git: true}}, okRT("ok"))
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	p.Now = func() time.Time { return now }
	for i := 0; i < 6; i++ {
		if rec := ask(p, "GET", refsPath, "", "a"); rec.Code != 200 {
			t.Fatalf("fetch %d = %d", i, rec.Code)
		}
	}
	if rec := ask(p, "GET", refsPath, "", "a"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("seventh = %d", rec.Code)
	}
	// Another reader is not held back by it.
	if rec := ask(p, "GET", refsPath, "", "b"); rec.Code != 200 {
		t.Fatalf("another reader = %d", rec.Code)
	}
	now = now.Add(61 * time.Second)
	if rec := ask(p, "GET", refsPath, "", "a"); rec.Code != 200 {
		t.Fatalf("a minute later = %d", rec.Code)
	}
}

func TestARefusedRequestIsNotPassedOn(t *testing.T) {
	var passed int
	var mu sync.Mutex
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		passed++
		mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	p := passFor([]RoomInfo{{Name: "sg3", Git: true}}, rt)
	want := string(pktLine("want "+strings.Repeat("a", 40)+"\n")) + "0000"
	for _, bad := range []string{"deepen 1\n", "shallow " + strings.Repeat("a", 40) + "\n", "filter blob:none\n"} {
		for i := 0; i < 5; i++ {
			rec := ask(p, "POST", "/git/room/sg3/github/o/r.git/git-upload-pack", want+string(pktLine(bad)), "a")
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ERR atrium") {
				t.Fatalf("%q = %d %q", bad, rec.Code, rec.Body)
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if passed != 0 {
		t.Fatalf("%d refused requests were passed on", passed)
	}
	// The capability form of a filter is refused too.
	capline := string(pktLine("want "+strings.Repeat("a", 40)+" multi_ack filter\n")) + "0000"
	if rec := ask(p, "POST", "/git/room/sg3/github/o/r.git/git-upload-pack", capline, "a"); !strings.Contains(rec.Body.String(), "ERR atrium") {
		t.Fatalf("filter capability: %q", rec.Body)
	}
}

func TestThePassThroughSendsNoProtocolHeaderCredentialOrCardAndOnlyTheFetchPaths(t *testing.T) {
	var got *http.Request
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	p := passFor([]RoomInfo{{Name: "SG3", Git: true}}, rt)
	req := httptest.NewRequest("GET", "http://hub"+refsPath, nil)
	for k, v := range map[string]string{"Git-Protocol": "version=2", "Authorization": "x", "Cookie": "c", HeaderCard: "s1", HeaderChain: "s1"} {
		req.Header.Set(k, v)
	}
	req = req.WithContext(WithReader(req.Context(), Reader{Reach: "loopback", Key: "a"}))
	p.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("nothing was passed on")
	}
	for _, k := range []string{"Git-Protocol", "Authorization", "Cookie", HeaderCard, HeaderChain} {
		if got.Header.Get(k) != "" {
			t.Errorf("%s reached the room", k)
		}
	}
	if got.URL.Path != "/v1/git/github/o/r.git/info/refs" || got.URL.RawQuery != "service=git-upload-pack" {
		t.Errorf("the room was asked for %s?%s", got.URL.Path, got.URL.RawQuery)
	}

	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/git/room/sg3/github/o/r.git/git-receive-pack", 403},
		{"GET", "/git/room/sg3/github/o/r.git/info/refs", 403},
		{"GET", "/git/room/sg3/github/o/r.git/HEAD", 404},
		{"GET", "/git/room/sg3/github/o/r.git/objects/info/packs", 404},
		{"GET", "/git/room/sg3/github/o/r.git/../x.git/info/refs?service=git-upload-pack", 404},
		{"GET", "/git/room/sg3/github/o/%2e%2e/r.git/info/refs?service=git-upload-pack", 404},
		{"GET", "/git/room/sg3/info/refs?service=git-upload-pack", 404},
		{"GET", "/git/room//github/o/r.git/info/refs?service=git-upload-pack", 404},
	} {
		if rec := ask(p, c.method, c.path, "", "a"); rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
	// No reader on the context is no identity.
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://hub"+refsPath, nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("no reader = %d", rec.Code)
	}
}

func TestARoomThatPredatesGitOrDoesNotAnswerIs503(t *testing.T) {
	old := passFor([]RoomInfo{{Name: "sg3", Git: false}}, okRT("ok"))
	if rec := ask(old, "GET", refsPath, "", "a"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a room that never said git = %d", rec.Code)
	}
	dead := passFor([]RoomInfo{{Name: "sg3", Git: true}}, rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	}))
	rec := ask(dead, "GET", refsPath, "", "a")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "sg3 is not connected") {
		t.Errorf("a room whose link failed = %d %q", rec.Code, rec.Body)
	}
	// And the slot it took is given back.
	for i := 0; i < 5; i++ {
		if rec := ask(dead, "GET", refsPath, "", "a"); rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "running already") {
			t.Fatalf("attempt %d: %d %q", i, rec.Code, rec.Body)
		}
	}
}
