//go:build integration

package gitsync

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// passRooms is a hub's view of its rooms with a transport that answers from a function, so a pass-through is
// tested without a link or a git.
type passRooms struct {
	rooms []RoomInfo
	rt    http.RoundTripper
}

func (f passRooms) Attached() []RoomInfo { return f.rooms }

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

// served is the pass-through on a real listener, because the deadlines are the connection's.
func servePass(t *testing.T, p *Pass) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.ServeHTTP(w, r.WithContext(WithReader(r.Context(), Reader{Reach: "loopback", Key: r.Header.Get("X-Reader")})))
	}))
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func slots(p *Pass) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running["sg3"]
}

// freed waits for the room's slot to be taken, which proves the request got there, and then to be given back.
func freed(t *testing.T, p *Pass, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for slots(p) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%s: the request never took the slot", what)
		}
		time.Sleep(time.Millisecond)
	}
	gone(t, p, what)
}

// gone waits for the slot to be given back, for a test whose own answer already proves the request was holding it.
func gone(t *testing.T, p *Pass, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for slots(p) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%s: the room's slot was never given back", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func rawConn(t *testing.T, srv *httptest.Server) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func quickPass(rt http.RoundTripper) *Pass {
	p := passFor([]RoomInfo{{Name: "sg3", Git: true}}, rt)
	p.PerRoom = 1
	p.FetchesPerMinute, p.RoundsPerMinute = 1000, 1000
	p.BodyIdle, p.RoomHeaderWait, p.RoomIdle, p.WriteIdle = 300*time.Millisecond, 300*time.Millisecond, 300*time.Millisecond, 300*time.Millisecond
	return p
}

func TestAReaderThatStopsReadingGivesTheSlotBack(t *testing.T) {
	var normal atomic.Bool
	p := quickPass(rtFunc(func(r *http.Request) (*http.Response, error) {
		if normal.Load() {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: endless{}}, nil
	}))
	srv := servePass(t, p)
	c := rawConn(t, srv)
	if _, err := c.Write([]byte("GET " + refsPath + " HTTP/1.1\r\nHost: x\r\nX-Reader: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	// It never reads. The slot is taken, and then it is not, though the connection is still open.
	freed(t, p, "a reader that stopped reading")
	// With the room's one slot free again, another reader is served.
	normal.Store(true)
	req, _ := http.NewRequest("GET", srv.URL+refsPath, nil)
	req.Header.Set("X-Reader", "b")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("the next fetch = %d", resp.StatusCode)
	}
}

func TestARoomThatNeverAnswersGivesTheSlotBackAndTheReaderA504(t *testing.T) {
	p := quickPass(rtFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	srv := servePass(t, p)
	start := time.Now()
	resp, err := http.Get(srv.URL + refsPath)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout || !strings.Contains(string(b), "sg3 did not answer in time") {
		t.Fatalf("%d %q", resp.StatusCode, b)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	gone(t, p, "a room that never answered")
}

func TestARoomThatGoesQuietMidAnswerGivesTheSlotBack(t *testing.T) {
	p := quickPass(rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: &ctxBody{ctx: r.Context()}}, nil
	}))
	srv := servePass(t, p)
	resp, err := http.Get(srv.URL + refsPath)
	if err != nil {
		t.Fatal(err)
	}
	// What came is cut short, and the reader's git sees a broken answer.
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	gone(t, p, "a room that went quiet")
}

func TestABodyThatStopsGivesTheSlotBack(t *testing.T) {
	p := quickPass(okRT("ok"))
	srv := servePass(t, p)
	c := rawConn(t, srv)
	// A hundred bytes promised, ten sent, and then nothing and the connection stays open.
	if _, err := c.Write([]byte("POST /git/room/sg3/github/o/r.git/git-upload-pack HTTP/1.1\r\nHost: x\r\n" +
		"Content-Type: application/x-git-upload-pack-request\r\nContent-Length: 100\r\n\r\n0032want aaa")); err != nil {
		t.Fatal(err)
	}
	freed(t, p, "a body that stopped")
	// And the room's next fetch runs.
	resp, err := http.Get(srv.URL + refsPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("the next fetch = %d", resp.StatusCode)
	}
}

// A fetch that keeps moving is not cut, though it takes far longer than any one deadline.
func TestAFetchThatKeepsMakingProgressIsNotCut(t *testing.T) {
	const chunks = 12
	p := quickPass(rtFunc(func(r *http.Request) (*http.Response, error) {
		pr, pw := io.Pipe()
		go func() {
			for i := 0; i < chunks; i++ {
				time.Sleep(100 * time.Millisecond)
				if _, err := pw.Write([]byte("0123456789")); err != nil {
					return
				}
			}
			pw.Close()
		}()
		// Like the real transport's body, it fails once the request's context is cancelled.
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: &ctxReader{ctx: r.Context(), rc: pr}}, nil
	}))
	srv := servePass(t, p)
	start := time.Now()
	resp, err := http.Get(srv.URL + refsPath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || len(b) != chunks*10 {
		t.Fatalf("%d, %d bytes, %v", resp.StatusCode, len(b), err)
	}
	if time.Since(start) < 1000*time.Millisecond {
		t.Fatalf("it was not slower than the deadlines (%v), so it proved nothing", time.Since(start))
	}

	// The same for a want list that arrives a few bytes at a time.
	body := string(pktLine("want "+strings.Repeat("a", 40)+"\n")) + "0000" + string(pktLine("done\n"))
	c := rawConn(t, srv)
	head := "POST /git/room/sg3/github/o/r.git/git-upload-pack HTTP/1.1\r\nHost: x\r\n" +
		"Content-Type: application/x-git-upload-pack-request\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n"
	if _, err := c.Write([]byte(head)); err != nil {
		t.Fatal(err)
	}
	var sent time.Duration
	for i := 0; i < len(body); i += 12 {
		end := min(i+12, len(body))
		if _, err := c.Write([]byte(body[i:end])); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		sent += 100 * time.Millisecond
	}
	if sent < 400*time.Millisecond {
		t.Fatalf("the body was not slower than the deadline (%v)", sent)
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || !strings.Contains(line, "200") {
		t.Fatalf("a slow but moving body was cut: %q %v", line, err)
	}
}

// The connection may carry the reader's next request, so a deadline that passed must not be left on it.
func TestTheDeadlinesAreClearedWhenTheRequestEnds(t *testing.T) {
	p := quickPass(okRT("ok"))
	srv := servePass(t, p)
	c := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 1}}
	defer c.CloseIdleConnections()
	body := string(pktLine("want "+strings.Repeat("a", 40)+"\n")) + "0000" + string(pktLine("done\n"))
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("POST", srv.URL+"/git/room/sg3/github/o/r.git/git-upload-pack", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("request %d on the same connection: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("request %d = %d", i, resp.StatusCode)
		}
		time.Sleep(450 * time.Millisecond) // longer than every deadline, so a stale one would have passed
	}
}
