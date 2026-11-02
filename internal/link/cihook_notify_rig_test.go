//go:build integration

package link

import (
	forgepkg "github.com/dovholuknf/atrium/internal/link/forge"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

func TestANoticeBecomesAGrowlerOnTheBoard(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, r := range []string{"sg4", "m1mini"} {
		if _, err := st.Add(r, hubstore.TransportDirect); err != nil {
			t.Fatal(err)
		}
	}
	x.proxy.SetGrowler(NewGrowler(testGrowlStore{st}))
	secret, _ := st.MintCIHookSecret(false)
	hook := NewCIHook(st, x.proxy.Deliverer(), nil)
	x.proxy.SetCIHook(hook)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(ciBody("o/r", 1, 1, "CI", "main", "failure")))
	req.Header.Set(forgepkg.CISecretHeader, secret)
	hook.Receive(httptest.NewRecorder(), req)
	rows, err := st.GrowlLive()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RoomName != "m1mini" || !strings.HasPrefix(rows[0].ID, "ci|") || rows[0].Reason != ReasonCI ||
		!strings.Contains(rows[0].Title, "CI failure: o/r CI on main") || !strings.Contains(rows[0].Body, "no route") {
		t.Fatalf("growlers = %+v", rows)
	}
}
