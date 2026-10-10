//go:build integration

package link

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// pinRoom records the pin-order bodies it was sent. hang holds the reply past
// any bound; fail answers 500.
type pinRoom struct {
	mu   sync.Mutex
	got  []string
	hang chan struct{}
	fail bool
}

func (p *pinRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/tasks/pin-order" {
		io.WriteString(w, `{}`)
		return
	}
	b, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.got = append(p.got, string(b))
	p.mu.Unlock()
	if p.hang != nil {
		select {
		case <-p.hang:
		case <-r.Context().Done():
		}
		return
	}
	if p.fail {
		http.Error(w, "nope", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"ok":true}`)
}

func (p *pinRoom) bodies() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.got...)
}

func postPin(t *testing.T, front, body, room string) (int, map[string]any, time.Duration) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, front+"/v1/tasks/pin-order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set("X-Atrium-Room", room)
	}
	start := time.Now()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	took := time.Since(start)
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out, took
}

// FF4. THE FULL UNTAGGED LIST REACHES EVERY ROOM, header or not.
func TestThePinOrderReachesEveryRoomWhole(t *testing.T) {
	a, b := &pinRoom{}, &pinRoom{}
	front, _, done := two(t, a, b)
	defer done()

	for _, room := range []string{"", "alpha"} {
		code, out, _ := postPin(t, front.URL, `{"ids":["alpha~c1","beta~c2","c3"]}`, room)
		if code != http.StatusOK {
			t.Fatalf("room header %q: answered %d", room, code)
		}
		if un, _ := out["unreached"].([]any); len(un) != 0 {
			t.Errorf("unreached %v, wanted none", un)
		}
	}
	for name, r := range map[string]*pinRoom{"alpha": a, "beta": b} {
		got := r.bodies()
		if len(got) != 2 {
			t.Fatalf("room %s was sent %d lists, wanted 2", name, len(got))
		}
		for _, g := range got {
			if g != `{"ids":["c1","c2","c3"]}` {
				t.Errorf("room %s was sent %s", name, g)
			}
		}
	}
}

// FF5. A ROOM THAT NEVER ANSWERS neither fails nor stalls the reply.
func TestAHungRoomDoesNotStallThePinOrder(t *testing.T) {
	ok, hung := &pinRoom{}, &pinRoom{hang: make(chan struct{})}
	defer close(hung.hang)
	front, _, done := two(t, ok, hung)
	defer done()

	code, out, took := postPin(t, front.URL, `{"ids":["c1","c2"]}`, "")
	if code != http.StatusOK {
		t.Fatalf("answered %d with one room taking the list", code)
	}
	if took > pinOrderBound+2*time.Second {
		t.Errorf("reply took %v, bound is %v", took, pinOrderBound)
	}
	un, _ := out["unreached"].([]any)
	if len(un) != 1 || un[0].(map[string]any)["room"] != "beta" {
		t.Errorf("unreached = %v, wanted beta", out["unreached"])
	}
	if len(ok.bodies()) != 1 {
		t.Errorf("the answering room was sent %d lists", len(ok.bodies()))
	}
}

// FF5, the other half: with no room taking it, the answer is 502.
func TestPinOrderIs502WhenNoRoomTakesIt(t *testing.T) {
	a, b := &pinRoom{fail: true}, &pinRoom{fail: true}
	front, _, done := two(t, a, b)
	defer done()

	code, _, _ := postPin(t, front.URL, `{"ids":["c1"]}`, "")
	if code != http.StatusBadGateway {
		t.Fatalf("answered %d, wanted 502", code)
	}
}
